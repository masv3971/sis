// Package postgres is the outbound persistence adapter: a pgx-backed
// implementation of the core repository ports, exposed as a lifecycle service.
package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store owns the connection pool and provides the repositories.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New connects to Postgres, verifies connectivity, and runs migrations.
func New(ctx context.Context, databaseURL string, log *slog.Logger) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	log.Info("postgres connected and migrated")
	return &Store{pool: pool, log: log}, nil
}

// Persons returns the Person repository backed by this store.
func (s *Store) Persons() *PersonRepository {
	return &PersonRepository{pool: s.pool}
}

// Accounts returns the Account repository backed by this store.
func (s *Store) Accounts() *AccountRepository {
	return &AccountRepository{pool: s.pool}
}

// Clients returns the OAuth2 client repository backed by this store.
func (s *Store) Clients() *ClientRepository {
	return &ClientRepository{pool: s.pool}
}

// Close releases the connection pool.
func (s *Store) Close(_ context.Context) error {
	s.log.Info("closing postgres pool")
	s.pool.Close()
	return nil
}
