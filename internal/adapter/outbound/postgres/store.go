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

// Close releases the connection pool. pgxpool.Close blocks until every
// borrowed connection is returned and ignores contexts, so it runs in a
// goroutine and Close returns when ctx is done even if a connection is still
// held. The inbound side cancels in-flight requests before this runs (see
// adapter.Service.Close), so a stuck borrower is the exception; when it
// happens the pool finishes closing in the background and the process exit
// reaps whatever is left.
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("closing postgres pool")
	done := make(chan struct{})
	go func() {
		s.pool.Close()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.log.Warn("postgres pool close did not finish before shutdown deadline", "err", ctx.Err())
		return fmt.Errorf("close postgres pool: %w", ctx.Err())
	}
}
