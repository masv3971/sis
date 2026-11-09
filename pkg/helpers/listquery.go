package helpers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/port"
)

// ListFields maps the field names an API accepts in sort and filter onto the
// stored keys (see port.ListParams). Only a field listed here can be used, so
// a client never names a stored key directly.
type ListFields map[string]string

// maxFilterValue bounds one filter value, so a request cannot make the
// database compare against an arbitrarily large string.
const maxFilterValue = 256

// ParseListQuery is ParseLimitOffset for an API that also implements the
// Edu-API sort, orderBy and filter query parameters over fields:
//
//	sort=<field>&orderBy=asc|desc
//	filter=<field>='<value>' [AND|OR <field>='<value>']
//
// Predicates are = (equals), != (not equals) and ~ (contains), compared on
// text and ignoring case. A filter holds one or two terms joined by a single
// AND or OR. A quote inside a value is written twice.
//
// A field outside fields is a 400: for sort the generic validation error, for
// filter ErrInvalidFilterField, which Edu-API reports as the spec's
// invalid_filter_field. An operator that is not implemented (>, >=, <, <=) is
// a 400 too. Nil fields rejects sort and filter, as ParseLimitOffset does.
func ParseListQuery(gc *gin.Context, defaultLimit, maxLimit int, fields ListFields) (port.ListParams, error) {
	defaultLimit, maxLimit, err := checkBounds(defaultLimit, maxLimit)
	if err != nil {
		return port.ListParams{}, err
	}
	params := port.ListParams{Limit: defaultLimit}
	if fields == nil {
		for _, name := range unsupportedListParams {
			if gc.Query(name) != "" {
				return port.ListParams{}, pagingError(name, "not supported")
			}
		}
	} else if err := parseSortFilter(gc, fields, &params); err != nil {
		return port.ListParams{}, err
	}
	if err := parseLimitOffset(gc, maxLimit, &params); err != nil {
		return port.ListParams{}, err
	}
	return params, nil
}

func parseSortFilter(gc *gin.Context, fields ListFields, params *port.ListParams) error {
	if raw := gc.Query("sort"); raw != "" {
		key, ok := fields[raw]
		if !ok {
			return pagingError("sort", "unknown field")
		}
		params.Sort = key
	}
	if raw, ok := gc.GetQuery("orderBy"); ok {
		switch strings.ToLower(raw) {
		case "asc":
		case "desc":
			params.Desc = true
		default:
			return pagingError("orderBy", "must be asc or desc")
		}
		if params.Sort == "" {
			return pagingError("orderBy", "requires sort")
		}
	}
	if raw := gc.Query("filter"); raw != "" {
		filter, err := parseFilter(raw, fields)
		if err != nil {
			return err
		}
		params.Filter = filter
	}
	return nil
}

// parseFilter parses one term, optionally followed by AND or OR and a second.
func parseFilter(raw string, fields ListFields) (*port.Filter, error) {
	var filter port.Filter
	rest := raw
	for {
		term, remainder, err := parseTerm(rest, fields)
		if err != nil {
			return nil, err
		}
		filter.Terms = append(filter.Terms, term)
		rest = remainder
		if rest == "" {
			return &filter, nil
		}
		if len(filter.Terms) == 2 {
			return nil, pagingError("filter", "only one AND or OR is supported")
		}
		any, after, ok := cutLogical(rest)
		if !ok {
			return nil, pagingError("filter", "expected AND or OR between terms")
		}
		filter.Any, rest = any, after
	}
}

// cutLogical strips a leading " AND " or " OR " (the spec requires the
// spaces) and reports which it was.
func cutLogical(s string) (isOr bool, rest string, ok bool) {
	for _, word := range []string{"AND", "OR"} {
		n := len(word) + 2
		if len(s) >= n && s[0] == ' ' && strings.EqualFold(s[1:1+len(word)], word) && s[1+len(word)] == ' ' {
			return word == "OR", s[n:], true
		}
	}
	return false, "", false
}

// parseTerm parses field<op>'value' from the start of s and returns the rest.
func parseTerm(s string, fields ListFields) (port.FilterTerm, string, error) {
	end := 0
	for end < len(s) && isFieldChar(s[end]) {
		end++
	}
	name, s := s[:end], s[end:]
	if name == "" {
		return port.FilterTerm{}, "", pagingError("filter", "expected a field name")
	}

	var op port.FilterOp
	switch {
	case strings.HasPrefix(s, "!="):
		op, s = port.FilterNotEquals, s[2:]
	case strings.HasPrefix(s, "="):
		op, s = port.FilterEquals, s[1:]
	case strings.HasPrefix(s, "~"):
		op, s = port.FilterContains, s[1:]
	case strings.HasPrefix(s, ">="), strings.HasPrefix(s, "<="):
		return port.FilterTerm{}, "", pagingError("filter", "operator not supported: "+s[:2])
	case strings.HasPrefix(s, ">"), strings.HasPrefix(s, "<"):
		return port.FilterTerm{}, "", pagingError("filter", "operator not supported: "+s[:1])
	default:
		return port.FilterTerm{}, "", pagingError("filter", "expected = != or ~ after "+name)
	}

	key, ok := fields[name]
	if !ok {
		return port.FilterTerm{}, "", NewErrorDetailsWithStatus(ErrInvalidFilterField,
			map[string]any{"field": name, "error": "unknown field or not filterable"}, http.StatusBadRequest)
	}
	value, rest, err := parseQuoted(s)
	if err != nil {
		return port.FilterTerm{}, "", err
	}
	return port.FilterTerm{Field: key, Op: op, Value: value}, rest, nil
}

func isFieldChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_'
}

// parseQuoted reads a single-quoted value, where ” stands for one quote, and
// returns it with the text after the closing quote.
func parseQuoted(s string) (value, rest string, err error) {
	if s == "" || s[0] != '\'' {
		return "", "", pagingError("filter", "a value must be in single quotes")
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		if s[i] != '\'' {
			b.WriteByte(s[i])
			if b.Len() > maxFilterValue {
				return "", "", pagingError("filter", "value too long")
			}
			continue
		}
		if i+1 < len(s) && s[i+1] == '\'' {
			b.WriteByte('\'')
			i++
			continue
		}
		return b.String(), s[i+1:], nil
	}
	return "", "", pagingError("filter", "unterminated quoted value")
}
