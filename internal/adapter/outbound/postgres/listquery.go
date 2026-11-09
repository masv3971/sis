package postgres

import (
	"fmt"
	"strings"

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

// personColumn maps a stored person key to the SQL expression for its column,
// with prefix before the column name. Only scalar columns can be sorted or
// filtered on. An empty optional column is NULL, as the key is absent.
func personColumn(prefix string) func(key string) (string, error) {
	return func(key string) (string, error) {
		switch key {
		case "sourcedId":
			return prefix + "sourced_id", nil
		case "status":
			return prefix + "status", nil
		case "givenName":
			return prefix + "given_name", nil
		case "familyName":
			return prefix + "family_name", nil
		case "middleName":
			return "NULLIF(" + prefix + "middle_name, '')", nil
		case "dateOfBirth":
			return "NULLIF(" + prefix + "date_of_birth, '')", nil
		}
		return "", fmt.Errorf("postgres: unknown person key %q", key)
	}
}
