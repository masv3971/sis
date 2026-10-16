package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

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

// Create inserts an entity, mapping a unique-key violation to domain.ErrConflict.
func (r *DocRepo[T, PT]) Create(ctx context.Context, e PT) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO entities (type, id, sourced_id, data, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		r.typeName, e.GetID(), e.GetSourcedID(), data, e.GetCreatedAt(), e.GetUpdatedAt())
	return mapUniqueViolation(err)
}

// Update replaces an entity, returning domain.ErrNotFound if absent and
// domain.ErrConflict if the new sourcedId belongs to another entity.
func (r *DocRepo[T, PT]) Update(ctx context.Context, e PT) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE entities SET sourced_id=$3, data=$4, updated_at=$5
		 WHERE type=$1 AND id=$2`,
		r.typeName, e.GetID(), e.GetSourcedID(), data, e.GetUpdatedAt())
	if err != nil {
		return mapUniqueViolation(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Get returns an entity by id.
func (r *DocRepo[T, PT]) Get(ctx context.Context, id string) (PT, error) {
	return r.scanOne(ctx, `SELECT data FROM entities WHERE type=$1 AND id=$2`, id)
}

// GetByIDs returns the entities whose id is in ids in one query.
func (r *DocRepo[T, PT]) GetByIDs(ctx context.Context, ids []string) ([]PT, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT data FROM entities WHERE type=$1 AND id = ANY($2)`, r.typeName, ids)
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

// GetBySourcedID returns an entity by business key.
func (r *DocRepo[T, PT]) GetBySourcedID(ctx context.Context, sourcedID string) (PT, error) {
	return r.scanOne(ctx, `SELECT data FROM entities WHERE type=$1 AND sourced_id=$2`, sourcedID)
}

// List returns a page of entities ordered by sourcedId.
func (r *DocRepo[T, PT]) List(ctx context.Context, params port.ListParams) (port.Page[PT], error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM entities WHERE type=$1`, r.typeName).Scan(&total); err != nil {
		return port.Page[PT]{}, err
	}

	query := `SELECT data FROM entities WHERE type=$1 ORDER BY sourced_id OFFSET $2`
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

// Delete removes an entity by id.
func (r *DocRepo[T, PT]) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM entities WHERE type=$1 AND id=$2`, r.typeName, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
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
