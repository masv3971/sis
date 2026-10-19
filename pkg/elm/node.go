// Package elm reads European Learning Model (ELM v3) documents, such as a
// European Digital Credential (EDC), in their JSON-LD form.
//
// ELM JSON-LD is loose in ways a typed struct decoder handles badly. A
// property may hold one value or an array. A node may be inline or just its
// id (a reference to a node defined elsewhere in the document). A text may be
// a plain string or a language map ({"en": ["..."]}). So the document is
// decoded generically and read through Node, which smooths these over:
// Nodes and Node resolve id references through a document-wide index, and
// Text picks one language. Unknown properties (@context, displayParameter,
// proofs, ...) are simply never read.
package elm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Document is a decoded ELM JSON-LD document with an index of every node
// that has an id, so references can be resolved.
type Document struct {
	Root  *Node
	index map[string]map[string]any
	// Languages is the preference order Text uses to pick a language; the
	// first present wins, then the alphabetically first available.
	Languages []string
}

// Node is one JSON-LD node (a JSON object) of a Document, with the path it
// was reached by for error messages.
type Node struct {
	doc  *Document
	obj  map[string]any
	Path string
}

// Parse decodes data, which must hold a single JSON object, into a Document.
// Numbers are kept as json.Number so identifiers are never rounded.
func Parse(data []byte, path string) (*Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a JSON object", path)
	}
	d := &Document{index: map[string]map[string]any{}, Languages: []string{"en"}}
	d.indexNodes(obj)
	d.Root = &Node{doc: d, obj: obj, Path: path}
	return d, nil
}

// indexNodes records every object with an id. Where the same id is defined
// more than once, the definition with the most properties wins, so a bare
// {"id": ...} stub never shadows the full node.
func (d *Document) indexNodes(v any) {
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t["id"].(string); ok && id != "" {
			if prev, seen := d.index[id]; !seen || len(t) > len(prev) {
				d.index[id] = t
			}
		}
		for _, child := range t {
			d.indexNodes(child)
		}
	case []any:
		for _, child := range t {
			d.indexNodes(child)
		}
	}
}

// complete returns obj with the properties of the indexed definition of its
// id that obj lacks. obj's own properties win, so an inline override is
// kept. obj is returned as is when it has no id, no definition, or nothing
// to add (e.g. it is the indexed definition); otherwise a copy is returned
// and the document is never modified.
func (d *Document) complete(obj map[string]any) map[string]any {
	id, _ := obj["id"].(string)
	if id == "" {
		return obj
	}
	full, ok := d.index[id]
	if !ok {
		return obj
	}
	var merged map[string]any
	for k, v := range full {
		if _, has := obj[k]; has {
			continue
		}
		if merged == nil {
			merged = maps.Clone(obj)
		}
		merged[k] = v
	}
	if merged == nil {
		return obj
	}
	return merged
}

// ID returns the node's id, or "".
func (n *Node) ID() string {
	s, _ := n.obj["id"].(string)
	return s
}

// Types returns the node's type values (from "type" or "@type").
func (n *Node) Types() []string {
	v, ok := n.obj["type"]
	if !ok {
		v = n.obj["@type"]
	}
	var out []string
	for _, x := range many(v) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Is reports whether the node has type t.
func (n *Node) Is(t string) bool { return slices.Contains(n.Types(), t) }

// Has reports whether the property is present and not null.
func (n *Node) Has(prop string) bool { return n.obj[prop] != nil }

// Nodes returns the nodes held by prop, one or many. Inline objects with an
// id are completed from the document's definition of that id: a typed
// reference such as {"id": "urn:org:1", "type": "Organisation"} gains the
// indexed legalName etc., while its own inline properties win. String
// values are resolved as id references. References that resolve to nothing
// in the document become nodes holding only their id.
func (n *Node) Nodes(prop string) []*Node {
	var out []*Node
	vals := many(n.obj[prop])
	for i, v := range vals {
		p := n.Path + "." + prop
		if len(vals) > 1 || isArray(n.obj[prop]) {
			p += "[" + strconv.Itoa(i) + "]"
		}
		switch t := v.(type) {
		case map[string]any:
			out = append(out, &Node{doc: n.doc, obj: n.doc.complete(t), Path: p})
		case string:
			obj, ok := n.doc.index[t]
			if !ok {
				obj = map[string]any{"id": t}
			}
			out = append(out, &Node{doc: n.doc, obj: obj, Path: p})
		}
	}
	return out
}

// Node returns the first node held by prop, or nil.
func (n *Node) Node(prop string) *Node {
	if ns := n.Nodes(prop); len(ns) > 0 {
		return ns[0]
	}
	return nil
}

// String returns prop as plain text: a string, a number, or (for a language
// map) the preferred language's text. Empty if absent.
func (n *Node) String(prop string) string {
	return n.doc.text(n.obj[prop])
}

// Literal returns prop only when it is a scalar (string, number, bool), so a
// property that may be either a literal or a node can be told apart.
func (n *Node) Literal(prop string) string {
	switch n.obj[prop].(type) {
	case string, json.Number, bool:
		return n.doc.text(n.obj[prop])
	}
	return ""
}

// Text is String for language-tagged properties (title, legalName, ...).
func (n *Node) Text(prop string) string { return n.String(prop) }

// text renders v as one string, picking the preferred language from a
// language map ({"en": "x"} or {"en": ["x"]}), a JSON-LD value object
// ({"@value": "x"}), or the first element of an array.
func (d *Document) text(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case []any:
		for _, x := range t {
			if s := d.text(x); s != "" {
				return s
			}
		}
	case map[string]any:
		if val, ok := t["@value"]; ok {
			return d.text(val)
		}
		for _, lang := range d.Languages {
			if s := d.text(t[lang]); s != "" {
				return s
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if strings.HasPrefix(k, "@") || k == "id" || k == "type" {
				continue
			}
			if s := d.text(t[k]); s != "" {
				return s
			}
		}
	}
	return ""
}

func many(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	default:
		return []any{t}
	}
}

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}
