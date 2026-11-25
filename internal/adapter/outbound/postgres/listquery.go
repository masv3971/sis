package postgres

import (
	"fmt"
	"strings"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// filterSQL turns f into a parenthesised condition, appending each value to
// args as a bound parameter. column maps a stored key to the SQL expression
// holding its text, which is NULL for a record without the key: a NULL
// comparison is never true, so such a record matches no term. It returns ""
// when f keeps every record.
func filterSQL(f *port.Filter, column func(key string) (string, error), args []any) (string, []any, error) {
	if f == nil || len(f.Terms) == 0 {
		return "", args, nil
	}
	conds := make([]string, 0, len(f.Terms))
	for _, term := range f.Terms {
		expr, err := column(term.Field)
		if err != nil {
			return "", nil, err
		}
		args = append(args, term.Value)
		n := len(args)
		switch term.Op {
		case port.FilterEquals:
			conds = append(conds, fmt.Sprintf("lower(%s) = lower($%d)", expr, n))
		case port.FilterNotEquals:
			conds = append(conds, fmt.Sprintf("lower(%s) <> lower($%d)", expr, n))
		case port.FilterContains:
			conds = append(conds, fmt.Sprintf("strpos(lower(%s), lower($%d)) > 0", expr, n))
		default:
			return "", nil, fmt.Errorf("postgres: unknown filter operator %d", term.Op)
		}
	}
	join := " AND "
	if f.Any {
		join = " OR "
	}
	return "(" + strings.Join(conds, join) + ")", args, nil
}

// searchSQL is the condition that every term occurs in one of the searched
// properties of spec, with each term's pattern appended to args. Each term is
// one ILIKE pattern, so its LIKE wildcards are escaped and match literally.
// It scans the live records of the type; add a pg_trgm index if that
// outgrows it.
func searchSQL(spec domain.QuerySpec, terms []string, args []any) (string, []any, error) {
	conds := make([]string, 0, len(terms))
	for _, term := range terms {
		args = append(args, "%"+escapeLike(term)+"%")
		n := len(args)
		matches := make([]string, 0, len(spec.SearchFields)+len(spec.SearchValueArrays))
		for _, key := range spec.SearchFields {
			expr, err := docKey(key)
			if err != nil {
				return "", nil, err
			}
			matches = append(matches, fmt.Sprintf("%s ILIKE $%d", expr, n))
		}
		// An array property may be absent or JSON null, which
		// jsonb_array_elements rejects, hence the CASE.
		for _, key := range spec.SearchValueArrays {
			if !port.ValidKey(key) {
				return "", nil, fmt.Errorf("postgres: invalid key %q", key)
			}
			matches = append(matches, fmt.Sprintf(`EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(data->'%[1]s') = 'array' THEN data->'%[1]s' ELSE '[]' END) x
				WHERE x->>'value' ILIKE $%[2]d)`, key, n))
		}
		conds = append(conds, "("+strings.Join(matches, " OR ")+")")
	}
	return strings.Join(conds, " AND "), args, nil
}

// escapeLike makes s match literally inside a LIKE pattern (backslash is
// the default escape character).
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// anyOfSQL is the condition that a's array property holds one of its values,
// or "" when a is nil. The key is spliced into the SQL, so it is checked.
func anyOfSQL(a *port.AnyOf, args []any) (string, []any, error) {
	if a == nil {
		return "", args, nil
	}
	if !port.ValidKey(a.Field) {
		return "", nil, fmt.Errorf("postgres: invalid key %q", a.Field)
	}
	args = append(args, a.Values)
	return fmt.Sprintf(`(jsonb_typeof(data->'%[1]s') = 'array' AND data->'%[1]s' ?| $%[2]d::text[])`, a.Field, len(args)), args, nil
}

// orderSQL is an ORDER BY list: sortExpr first when it is not empty, then
// tail. A record without the sort key goes last in both directions, and the
// key compares byte by byte, as the in-memory repositories do.
func orderSQL(sortExpr string, desc bool, tail string) string {
	if sortExpr == "" {
		return tail
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return fmt.Sprintf(`%s COLLATE "C" %s NULLS LAST, %s`, sortExpr, dir, tail)
}

// docKey is the SQL expression for a string property of an entity's JSONB. A
// property that is not a string is NULL, as it is for the in-memory doc store.
// key is spliced into the SQL, so it must be a plain key.
func docKey(key string) (string, error) {
	if !port.ValidKey(key) {
		return "", fmt.Errorf("postgres: invalid key %q", key)
	}
	return fmt.Sprintf(`(CASE WHEN jsonb_typeof(data->'%[1]s') = 'string' THEN data->>'%[1]s' END)`, key), nil
}
