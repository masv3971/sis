package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

const uniqueViolation = "23505"

// personColumns is the shared projection for person reads.
const personColumns = `id, sourced_id, status, given_name, family_name,
	middle_name, date_of_birth, roles, identifiers, emails, phones,
	created_at, updated_at`

// PersonRepository is the pgx-backed port.PersonRepository.
type PersonRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a person, mapping a unique-key violation to domain.ErrConflict.
func (r *PersonRepository) Create(ctx context.Context, p domain.Person) error {
	roles, ids, emails, phones, err := marshalArrays(p)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO persons (`+personColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		p.ID, p.SourcedID, p.Status, p.GivenName, p.FamilyName,
		p.MiddleName, p.DateOfBirth, roles, ids, emails, phones,
		p.CreatedAt, p.UpdatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrConflict
	}
	return err
}

// Update replaces a person, returning domain.ErrNotFound if it does not exist.
func (r *PersonRepository) Update(ctx context.Context, p domain.Person) error {
	roles, ids, emails, phones, err := marshalArrays(p)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE persons SET
		sourced_id=$2, status=$3, given_name=$4, family_name=$5,
		middle_name=$6, date_of_birth=$7, roles=$8, identifiers=$9,
		emails=$10, phones=$11, updated_at=$12
		WHERE id=$1`,
		p.ID, p.SourcedID, p.Status, p.GivenName, p.FamilyName,
		p.MiddleName, p.DateOfBirth, roles, ids, emails, phones,
		p.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Get returns a person by id.
func (r *PersonRepository) Get(ctx context.Context, id string) (domain.Person, error) {
	return r.scanOne(ctx, `SELECT `+personColumns+` FROM persons WHERE id=$1`, id)
}

// GetBySourcedID returns a person by business key.
func (r *PersonRepository) GetBySourcedID(ctx context.Context, sourcedID string) (domain.Person, error) {
	return r.scanOne(ctx, `SELECT `+personColumns+` FROM persons WHERE sourced_id=$1`, sourcedID)
}

// List returns a page of persons ordered by family then given name.
func (r *PersonRepository) List(ctx context.Context, params port.ListParams) (port.Page[domain.Person], error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM persons`).Scan(&total); err != nil {
		return port.Page[domain.Person]{}, err
	}

	query := `SELECT ` + personColumns + ` FROM persons ORDER BY family_name, given_name OFFSET $1`
	args := []any{params.Offset}
	if params.Limit > 0 {
		query += ` LIMIT $2`
		args = append(args, params.Limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return port.Page[domain.Person]{}, err
	}
	defer rows.Close()

	items := make([]domain.Person, 0, params.Limit)
	for rows.Next() {
		p, err := scanPerson(rows)
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

// Delete removes a person by id.
func (r *PersonRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM persons WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
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

func scanPerson(r row) (domain.Person, error) {
	var (
		p                          domain.Person
		roles, ids, emails, phones []byte
	)
	if err := r.Scan(&p.ID, &p.SourcedID, &p.Status, &p.GivenName, &p.FamilyName,
		&p.MiddleName, &p.DateOfBirth, &roles, &ids, &emails, &phones,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
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
