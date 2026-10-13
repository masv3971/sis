package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/internal/core/domain"
)

// AccountRepository is the pgx-backed port.AccountRepository.
type AccountRepository struct {
	pool *pgxpool.Pool
}

// GetByUsername returns an account by username.
func (r *AccountRepository) GetByUsername(ctx context.Context, username string) (domain.Account, error) {
	var (
		a     domain.Account
		roles []byte
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, roles, status, created_at, updated_at
		 FROM accounts WHERE username=$1`, username,
	).Scan(&a.ID, &a.Username, &a.PasswordHash, &roles, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Account{}, err
	}
	if err := unmarshalInto(roles, &a.Roles); err != nil {
		return domain.Account{}, err
	}
	return a, nil
}

// Upsert inserts or updates by username, preserving id and created_at on update.
func (r *AccountRepository) Upsert(ctx context.Context, a domain.Account) error {
	roles, err := json.Marshal(a.Roles)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO accounts (id, username, password_hash, roles, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (username) DO UPDATE SET
		   password_hash=EXCLUDED.password_hash, roles=EXCLUDED.roles,
		   status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`,
		a.ID, a.Username, a.PasswordHash, roles, a.Status, a.CreatedAt, a.UpdatedAt)
	return err
}

// ClientRepository is the pgx-backed port.ClientRepository.
type ClientRepository struct {
	pool *pgxpool.Pool
}

// GetByClientID returns a client by client_id.
func (r *ClientRepository) GetByClientID(ctx context.Context, clientID string) (domain.Client, error) {
	var (
		c      domain.Client
		scopes []byte
	)
	err := r.pool.QueryRow(ctx,
		`SELECT client_id, secret_hash, name, scopes, created_at, updated_at
		 FROM oauth_clients WHERE client_id=$1`, clientID,
	).Scan(&c.ClientID, &c.SecretHash, &c.Name, &scopes, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Client{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Client{}, err
	}
	if err := unmarshalInto(scopes, &c.Scopes); err != nil {
		return domain.Client{}, err
	}
	return c, nil
}

// Upsert inserts or updates by client_id, preserving created_at on update.
func (r *ClientRepository) Upsert(ctx context.Context, c domain.Client) error {
	scopes, err := json.Marshal(c.Scopes)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO oauth_clients (client_id, secret_hash, name, scopes, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (client_id) DO UPDATE SET
		   secret_hash=EXCLUDED.secret_hash, name=EXCLUDED.name,
		   scopes=EXCLUDED.scopes, updated_at=EXCLUDED.updated_at`,
		c.ClientID, c.SecretHash, c.Name, scopes, c.CreatedAt, c.UpdatedAt)
	return err
}
