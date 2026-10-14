// Package postgres is the outbound persistence adapter: a pgx-backed
// implementation of the core repository ports, exposed as a lifecycle service.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/pkg/logger"
)

// Service owns the connection pool and provides the repositories. It is a
// "Service" rather than a "Client" because it holds a runtime resource (the
// pgx pool) that must be released on shutdown via Close.
type Service struct {
	pool *pgxpool.Pool
	log  *logger.Log
}

// New connects to Postgres, verifies connectivity, and runs migrations.
func New(ctx context.Context, databaseURL string, log *logger.Log) (*Service, error) {
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
	return &Service{pool: pool, log: log}, nil
}

// Persons returns the Person repository backed by this service.
func (s *Service) Persons() *PersonRepository {
	return &PersonRepository{pool: s.pool}
}

// Accounts returns the Account repository backed by this service.
func (s *Service) Accounts() *AccountRepository {
	return &AccountRepository{pool: s.pool}
}

// Clients returns the OAuth2 client repository backed by this service.
func (s *Service) Clients() *ClientRepository {
	return &ClientRepository{pool: s.pool}
}

// Close releases the connection pool.
func (s *Service) Close(_ context.Context) error {
	s.log.Info("closing postgres pool")
	s.pool.Close()
	return nil
}
