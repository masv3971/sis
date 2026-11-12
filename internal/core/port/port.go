// Package port declares the interfaces (ports) the core depends on. Outbound
// adapters implement them; the service layer calls them. No adapter imports
// another — everything meets here.
package port

import (
	"context"
	"regexp"
	"strings"

	"github.com/SUNET/sis/internal/core/domain"
)

// ListParams carries the options common to list queries. Each inbound adapter
// maps its own conventions (OEAPI pageNumber/pageSize, Edu-API limit/offset,
// sort and filter) onto this shared shape. Adapters that do not implement
// sort or filter reject them rather than ignore them. Add fields here only
// together with an implementation in every repository.
//
// Repositories treat Limit <= 0 as "unbounded", so inbound adapters must
// reject non-positive limits and clamp larger ones to their configured
// maximum (sis.adapter.inbound.<api>) before calling a repository.
type ListParams struct {
	Limit  int
	Offset int
	// Sort is the stored key (its JSON name) to order by; empty keeps the
	// default order. Ties, and records without the key, which sort last
	// whatever Desc says, fall back to the default order. Keys compare as
	// text, byte by byte.
	Sort string
	Desc bool
	// Filter keeps only the records it matches; nil keeps all of them.
	Filter *Filter
	// Search keeps only the records matching every term: a term matches
	// when it occurs, ignoring case, in one of the searched properties of
	// the type (domain.QuerySpec). A type without a QuerySpec rejects it.
	Search []string
	// AnyOf keeps only the records matching it; nil keeps all of them.
	AnyOf *AnyOf
}

// AnyOf matches the records whose array property Field holds at least one of
// Values. Field is a stored key chosen by the inbound adapter, never request
// input; only arrays of strings can be matched.
type AnyOf struct {
	Field  string
	Values []string
}

// FilterOp is how a FilterTerm compares a stored value with its Value.
type FilterOp int

const (
	FilterEquals FilterOp = iota
	FilterNotEquals
	FilterContains
)

// FilterTerm compares one stored key with Value. Comparison is on text and
// ignores case. A record that lacks the key matches no term, not even
// FilterNotEquals.
type FilterTerm struct {
	Field string
	Op    FilterOp
	Value string
}

// Filter combines terms with AND, or with OR when Any is set. No terms keep
// every record.
//
// Field names (and ListParams.Sort) are stored keys that the inbound adapter
// picks from a fixed allowlist, never request input. Repositories still
// reject a name that is not a known key. Only string-valued keys can be
// sorted or filtered on.
type Filter struct {
	Terms []FilterTerm
	Any   bool
}

var keyPattern = regexp.MustCompile(`^[A-Za-z]+$`)

// ValidKey reports whether name has the shape of a stored JSON key. The
// document repositories splice the key into SQL, so they check it even though
// callers pass constants.
func ValidKey(name string) bool { return keyPattern.MatchString(name) }

// Match reports whether a record satisfies f. get returns the record's value
// for a key, and false when the record lacks it. The in-memory repositories
// use it so they agree with the SQL the Postgres ones build; case folding
// there is the database's lower().
func (f *Filter) Match(get func(field string) (string, bool)) bool {
	if f == nil || len(f.Terms) == 0 {
		return true
	}
	for _, term := range f.Terms {
		matched := term.match(get)
		if f.Any && matched {
			return true
		}
		if !f.Any && !matched {
			return false
		}
	}
	return !f.Any
}

func (t FilterTerm) match(get func(field string) (string, bool)) bool {
	value, ok := get(t.Field)
	if !ok {
		return false
	}
	value, want := strings.ToLower(value), strings.ToLower(t.Value)
	switch t.Op {
	case FilterEquals:
		return value == want
	case FilterNotEquals:
		return value != want
	case FilterContains:
		return strings.Contains(value, want)
	}
	return false
}

// Page is a paginated query result.
type Page[T any] struct {
	Items []T
	Total int
}

// EventPublisher is the port for emitting domain events to the outside world.
//
// Publish is called after the write it describes has been committed, so the
// service cannot undo the write when Publish fails; it logs the lost event
// instead. A publisher that can fail (e.g. a message bus) is responsible for
// durable delivery, for example by writing an outbox in the same transaction
// and relaying from it.
type EventPublisher interface {
	Publish(ctx context.Context, e domain.Event) error
}
