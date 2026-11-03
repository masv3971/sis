package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

const uniqueViolation = "23505"

// maxPrealloc bounds the result-slice capacity reserved up front for a List,
// so a large caller-supplied limit cannot force a huge allocation (or a
// negative-cap panic) before any row is scanned. Results beyond this simply
// grow the slice as rows arrive.
const maxPrealloc = 256

// preallocCap returns a safe initial capacity for a page of up to limit rows.
func preallocCap(limit int) int {
	return min(max(limit, 0), maxPrealloc)
}

// personColumns is the shared projection for person reads.
const personColumns = `id, sourced_id, status, given_name, family_name,
	middle_name, date_of_birth, roles, identifiers, emails, phones,
	created_at, updated_at, expires_at`

// PersonRepository is the pgx-backed port.PersonRepository.
type PersonRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a person, mapping a unique-key violation with a live person
// to domain.ErrConflict. An expired person holding the same id or sourcedId
// is deleted first, in the same transaction.
func (r *PersonRepository) Create(ctx context.Context, p domain.Person) error {
	roles, ids, emails, phones, err := marshalArrays(p)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM persons WHERE (id=$1 OR sourced_id=$2) AND expires_at <= now()`,
			p.ID, p.SourcedID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO persons (`+personColumns+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			p.ID, p.SourcedID, p.Status, p.GivenName, p.FamilyName,
			p.MiddleName, p.DateOfBirth, roles, ids, emails, phones,
			p.CreatedAt, p.UpdatedAt, p.ExpiresAt)
		return err
	})
	return mapUniqueViolation(err)
}

// mapUniqueViolation translates a Postgres unique-key violation into
// domain.ErrConflict (HTTP 409) so Create and Update report a business-key
// collision identically; any other error passes through unchanged.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrConflict
	}
	return err
}

// Update replaces a live person whose stored expiry still equals
// prevExpiresAt, returning domain.ErrNotFound if it does not exist,
// domain.ErrStale if the expiry changed since the caller read it, and
// domain.ErrConflict if the new sourcedId belongs to another live person. An
// expired holder of that sourcedId is deleted first. See DocRepo.Update.
func (r *PersonRepository) Update(ctx context.Context, p domain.Person, prevExpiresAt *time.Time) error {
	roles, ids, emails, phones, err := marshalArrays(p)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM persons WHERE sourced_id=$1 AND id<>$2 AND expires_at <= now()`,
			p.SourcedID, p.ID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE persons SET
			sourced_id=$2, status=$3, given_name=$4, family_name=$5,
			middle_name=$6, date_of_birth=$7, roles=$8, identifiers=$9,
			emails=$10, phones=$11, updated_at=$12, expires_at=$13
			WHERE id=$1 AND expires_at IS NOT DISTINCT FROM $14 AND `+liveFilter,
			p.ID, p.SourcedID, p.Status, p.GivenName, p.FamilyName,
			p.MiddleName, p.DateOfBirth, roles, ids, emails, phones,
			p.UpdatedAt, p.ExpiresAt, prevExpiresAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return missOrStale(ctx, tx,
				`SELECT EXISTS (SELECT 1 FROM persons WHERE id=$1 AND `+liveFilter+`)`, p.ID)
		}
		return nil
	})
	return mapUniqueViolation(err)
}

// Get returns a live person by id.
func (r *PersonRepository) Get(ctx context.Context, id string) (domain.Person, error) {
	return r.scanOne(ctx, `SELECT `+personColumns+` FROM persons WHERE id=$1 AND `+liveFilter, id)
}

// GetByIDs returns the persons whose id is in ids in one query.
func (r *PersonRepository) GetByIDs(ctx context.Context, ids []string) ([]domain.Person, error) {
	return r.getAny(ctx, "id", ids)
}

// GetBySourcedIDs returns the persons whose sourcedId is in sids in one
// query.
func (r *PersonRepository) GetBySourcedIDs(ctx context.Context, sids []string) ([]domain.Person, error) {
	return r.getAny(ctx, "sourced_id", sids)
}

// getAny returns the live persons whose column (a constant, never input) is
// in keys.
func (r *PersonRepository) getAny(ctx context.Context, column string, keys []string) ([]domain.Person, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+personColumns+` FROM persons WHERE `+column+` = ANY($1) AND `+liveFilter, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.Person, 0, preallocCap(len(keys)))
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// GetBySourcedID returns a person by business key.
func (r *PersonRepository) GetBySourcedID(ctx context.Context, sourcedID string) (domain.Person, error) {
	return r.scanOne(ctx, `SELECT `+personColumns+` FROM persons WHERE sourced_id=$1 AND `+liveFilter, sourcedID)
}

// List returns a page of persons ordered by family name, given name, then id.
// The unique id tie-breaker makes the order total so rows cannot move between
// pages for people who share a name. The total comes from the same statement
// as the page, so the two always agree; a page past the end is one row of
// NULL person columns that carries only the total.
func (r *PersonRepository) List(ctx context.Context, params port.ListParams) (port.Page[domain.Person], error) {
	var limit any // nil: LIMIT NULL, no limit
	if params.Limit > 0 {
		limit = params.Limit
	}
	rows, err := r.pool.Query(ctx, `SELECT p.*, t.n
		FROM (SELECT count(*) AS n FROM persons WHERE `+liveFilter+`) t
		LEFT JOIN LATERAL (SELECT `+personColumns+` FROM persons WHERE `+liveFilter+`
			ORDER BY family_name, given_name, id OFFSET $1 LIMIT $2) p ON true
		ORDER BY p.family_name, p.given_name, p.id`, params.Offset, limit)
	if err != nil {
		return port.Page[domain.Person]{}, err
	}
	defer rows.Close()

	var total int
	items := make([]domain.Person, 0, preallocCap(params.Limit))
	for rows.Next() {
		if rows.RawValues()[0] == nil { // NULL id: only the total
			dest := make([]any, len(rows.FieldDescriptions())) // nil skips a column
			dest[len(dest)-1] = &total
			if err := rows.Scan(dest...); err != nil {
				return port.Page[domain.Person]{}, err
			}
			continue
		}
		p, err := scanPerson(rows, &total)
		if err != nil {
			return port.Page[domain.Person]{}, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return port.Page[domain.Person]{}, err
	}
	return port.Page[domain.Person]{Items: items, Total: total}, nil
}

// Delete removes a live person by id.
func (r *PersonRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM persons WHERE id=$1 AND `+liveFilter, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteExpired removes every person expired at now and returns their ids.
func (r *PersonRepository) DeleteExpired(ctx context.Context, now time.Time) ([]string, error) {
	return collectIDs(r.pool.Query(ctx, `DELETE FROM persons WHERE expires_at <= $1 RETURNING id`, now))
}

func (r *PersonRepository) scanOne(ctx context.Context, query, arg string) (domain.Person, error) {
	p, err := scanPerson(r.pool.QueryRow(ctx, query, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Person{}, domain.ErrNotFound
	}
	return p, err
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface {
	Scan(dest ...any) error
}

// scanPerson scans one personColumns row, then any extra trailing columns.
func scanPerson(src row, extra ...any) (domain.Person, error) {
	var (
		p                          domain.Person
		roles, ids, emails, phones []byte
	)
	dest := append([]any{&p.ID, &p.SourcedID, &p.Status, &p.GivenName, &p.FamilyName,
		&p.MiddleName, &p.DateOfBirth, &roles, &ids, &emails, &phones,
		&p.CreatedAt, &p.UpdatedAt, &p.ExpiresAt}, extra...)
	if err := src.Scan(dest...); err != nil {
		return domain.Person{}, err
	}
	if err := unmarshalInto(roles, &p.Roles); err != nil {
		return domain.Person{}, err
	}
	if err := unmarshalInto(ids, &p.Identifiers); err != nil {
		return domain.Person{}, err
	}
	if err := unmarshalInto(emails, &p.Emails); err != nil {
		return domain.Person{}, err
	}
	if err := unmarshalInto(phones, &p.Phones); err != nil {
		return domain.Person{}, err
	}
	return p, nil
}

func marshalArrays(p domain.Person) (roles, ids, emails, phones []byte, err error) {
	if roles, err = json.Marshal(p.Roles); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal roles: %w", err)
	}
	if ids, err = json.Marshal(p.Identifiers); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal identifiers: %w", err)
	}
	if emails, err = json.Marshal(p.Emails); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal emails: %w", err)
	}
	if phones, err = json.Marshal(p.Phones); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal phones: %w", err)
	}
	return roles, ids, emails, phones, nil
}

func unmarshalInto(data []byte, dst any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}
