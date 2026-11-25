package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/SUNET/sis/internal/core/domain"
)

// goMigrations are migrations whose work SQL cannot do. The runner executes
// the version's .up.sql first, then this step, in the same transaction.
var goMigrations = map[string]func(ctx context.Context, tx pgx.Tx) error{
	"0012_sourced_id_lowercase": foldSourcedIDs,
}

// maxListed bounds the rows a migration error names.
const maxListed = 20

// foldSourcedIDs stores every sourcedId in the form domain.FoldSourcedID
// returns, in the sourced_id column and in the JSONB document. The folding is
// Go's, never the database's lower(), which follows the database locale.
// It stops, naming the rows, on a non-ASCII id or on two ids of one type that
// fold to the same value, and changes nothing then.
func foldSourcedIDs(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT type, id, sourced_id FROM entities`)
	if err != nil {
		return fmt.Errorf("read sourcedIds: %w", err)
	}
	type change struct{ typ, id, sourcedID string }
	var (
		changes  []change
		nonASCII []string
		folded   = map[[2]string][]string{}
	)
	for rows.Next() {
		var typ, id, sid string
		if err := rows.Scan(&typ, &id, &sid); err != nil {
			rows.Close()
			return fmt.Errorf("read sourcedIds: %w", err)
		}
		if !domain.ValidSourcedID(sid) {
			nonASCII = append(nonASCII, typ+":"+sid)
			continue
		}
		fold := domain.FoldSourcedID(sid)
		key := [2]string{typ, fold}
		folded[key] = append(folded[key], sid)
		if fold != sid {
			changes = append(changes, change{typ, id, fold})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read sourcedIds: %w", err)
	}

	if len(nonASCII) > 0 {
		return fmt.Errorf("sourcedId must be printable ASCII; fix these (type:sourcedId) first: %s", listed(nonASCII))
	}
	var collisions []string
	for key, sids := range folded {
		if len(sids) > 1 {
			sort.Strings(sids)
			collisions = append(collisions, key[0]+":"+strings.Join(sids, " | "))
		}
	}
	if len(collisions) > 0 {
		return fmt.Errorf("sourcedIds collide ignoring case; merge or rename these (type:ids) first: %s", listed(collisions))
	}

	batch := &pgx.Batch{}
	for _, c := range changes {
		batch.Queue(`UPDATE entities SET sourced_id=$3, data=jsonb_set(data, '{sourcedId}', to_jsonb($3::text)) WHERE type=$1 AND id=$2`,
			c.typ, c.id, c.sourcedID)
	}
	results := tx.SendBatch(ctx, batch)
	for range changes {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("fold sourcedId: %w", err)
		}
	}
	return results.Close()
}

// listed joins items, sorted, naming at most maxListed of them.
func listed(items []string) string {
	sort.Strings(items)
	if len(items) <= maxListed {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(items[:maxListed], ", "), len(items)-maxListed)
}
