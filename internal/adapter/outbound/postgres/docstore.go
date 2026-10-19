package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// liveFilter restricts a query to records that have not expired (see
// port.Repository). Kept as one constant so every read applies the same rule
// as domain.Expired (expired at exactly expires_at).
const liveFilter = `(expires_at IS NULL OR expires_at > now())`

// DocRepo is a pgx-backed generic port.Repository storing each entity as JSONB
// in the shared entities table, discriminated by its type tag.
type DocRepo[T any, PT domain.EntityPtr[T]] struct {
	pool     *pgxpool.Pool
	typeName string
}

// NewDocRepo returns a doc repository for entity type T backed by the service.
func NewDocRepo[T any, PT domain.EntityPtr[T]](s *Service) *DocRepo[T, PT] {
	return &DocRepo[T, PT]{pool: s.pool, typeName: PT(new(T)).EntityType()}
}

// Create inserts an entity, mapping a unique-key violation with a live
// record to domain.ErrConflict. An expired record holding the same id or
// sourcedId is deleted first, in the same transaction.
func (r *DocRepo[T, PT]) Create(ctx context.Context, e PT) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM entities WHERE type=$1 AND (id=$2 OR sourced_id=$3) AND expires_at <= now()`,
			r.typeName, e.GetID(), e.GetSourcedID()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO entities (type, id, sourced_id, data, created_at, updated_at, expires_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			r.typeName, e.GetID(), e.GetSourcedID(), data, e.GetCreatedAt(), e.GetUpdatedAt(), e.GetExpiresAt())
		return err
	})
	return mapUniqueViolation(err)
}

// Update replaces a live entity whose stored expiry still equals
// prevExpiresAt. It returns domain.ErrNotFound if absent, domain.ErrStale if
// the expiry changed since the caller read it, and domain.ErrConflict if the
// new sourcedId belongs to another live entity. An expired holder of that
// sourcedId is deleted first, in the same transaction (rolled back with any
// failure).
//
// The precondition sits in the UPDATE's WHERE clause, so Postgres checks it
// against the latest committed row version: a concurrent write cannot slip
// in between the check and the write.
func (r *DocRepo[T, PT]) Update(ctx context.Context, e PT, prevExpiresAt *time.Time) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM entities WHERE type=$1 AND sourced_id=$2 AND id<>$3 AND expires_at <= now()`,
			r.typeName, e.GetSourcedID(), e.GetID()); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE entities SET sourced_id=$3, data=$4, updated_at=$5, expires_at=$6
			 WHERE type=$1 AND id=$2 AND expires_at IS NOT DISTINCT FROM $7 AND `+liveFilter,
			r.typeName, e.GetID(), e.GetSourcedID(), data, e.GetUpdatedAt(), e.GetExpiresAt(), prevExpiresAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return missOrStale(ctx, tx,
				`SELECT EXISTS (SELECT 1 FROM entities WHERE type=$1 AND id=$2 AND `+liveFilter+`)`,
				r.typeName, e.GetID())
		}
		return nil
	})
	return mapUniqueViolation(err)
}

// missOrStale explains a conditional UPDATE that matched no row: the record
// is live (so the precondition failed: domain.ErrStale) or it is not
// (domain.ErrNotFound). existsQuery must select one boolean.
func missOrStale(ctx context.Context, tx pgx.Tx, existsQuery string, args ...any) error {
	var live bool
	if err := tx.QueryRow(ctx, existsQuery, args...).Scan(&live); err != nil {
		return err
	}
	if live {
		return domain.ErrStale
	}
	return domain.ErrNotFound
}

// Get returns a live entity by id.
func (r *DocRepo[T, PT]) Get(ctx context.Context, id string) (PT, error) {
	return r.scanOne(ctx, `SELECT data FROM entities WHERE type=$1 AND id=$2 AND `+liveFilter, id)
}

// GetByIDs returns the live entities whose id is in ids in one query.
func (r *DocRepo[T, PT]) GetByIDs(ctx context.Context, ids []string) ([]PT, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT data FROM entities WHERE type=$1 AND id = ANY($2) AND `+liveFilter, r.typeName, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PT, 0, preallocCap(len(ids)))
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		e, err := unmarshalDoc[T, PT](data)
		if err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// GetBySourcedID returns a live entity by business key.
func (r *DocRepo[T, PT]) GetBySourcedID(ctx context.Context, sourcedID string) (PT, error) {
	return r.scanOne(ctx, `SELECT data FROM entities WHERE type=$1 AND sourced_id=$2 AND `+liveFilter, sourcedID)
}

// List returns a page of live entities ordered by sourcedId.
func (r *DocRepo[T, PT]) List(ctx context.Context, params port.ListParams) (port.Page[PT], error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM entities WHERE type=$1 AND `+liveFilter, r.typeName).Scan(&total); err != nil {
		return port.Page[PT]{}, err
	}

	query := `SELECT data FROM entities WHERE type=$1 AND ` + liveFilter + ` ORDER BY sourced_id OFFSET $2`
	args := []any{r.typeName, params.Offset}
	if params.Limit > 0 {
		query += ` LIMIT $3`
		args = append(args, params.Limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return port.Page[PT]{}, err
	}
	defer rows.Close()

	items := make([]PT, 0, preallocCap(params.Limit))
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return port.Page[PT]{}, err
		}
		e, err := unmarshalDoc[T, PT](data)
		if err != nil {
			return port.Page[PT]{}, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return port.Page[PT]{}, err
	}
	return port.Page[PT]{Items: items, Total: total}, nil
}

// Delete removes a live entity by id.
func (r *DocRepo[T, PT]) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM entities WHERE type=$1 AND id=$2 AND `+liveFilter, r.typeName, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteExpired removes every entity of this type expired at now and returns
// their ids.
func (r *DocRepo[T, PT]) DeleteExpired(ctx context.Context, now time.Time) ([]string, error) {
	return collectIDs(r.pool.Query(ctx,
		`DELETE FROM entities WHERE type=$1 AND expires_at <= $2 RETURNING id`, r.typeName, now))
}

// collectIDs drains a single-text-column result (e.g. DELETE ... RETURNING id).
func collectIDs(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (r *DocRepo[T, PT]) scanOne(ctx context.Context, query, arg string) (PT, error) {
	var data []byte
	err := r.pool.QueryRow(ctx, query, r.typeName, arg).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return unmarshalDoc[T, PT](data)
}

func unmarshalDoc[T any, PT domain.EntityPtr[T]](data []byte) (PT, error) {
	e := PT(new(T))
	if err := json.Unmarshal(data, e); err != nil {
		return nil, err
	}
	return e, nil
}
