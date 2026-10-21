// Package postgres is the outbound persistence adapter: a pgx-backed
// implementation of the core repository ports, exposed as a lifecycle service.
package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Service owns the connection pool and provides the repositories. It is a
// "Service" rather than a "Client" because it holds a runtime resource (the
// pgx pool) that must be released on shutdown via Close.
type Service struct {
	pool *pgxpool.Pool
	log  *logger.Log
}

// New connects to sis.adapter.outbound.postgres, verifies connectivity, and
// runs migrations. ctx bounds only this startup work: the pool outlives it.
func New(ctx context.Context, cfg *model.Cfg, log *logger.Log) (*Service, error) {
	poolCfg, err := poolConfig(cfg.SIS.Adapter.Outbound.Postgres)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
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

// poolConfig parses the connection string and applies the configured
// timeouts on top of it. A zero timeout keeps what the connection string
// (or the server) says.
func poolConfig(cfg model.Postgres) (*pgxpool.Config, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.ConnectionString)
	if err != nil {
		// pgx redacts the password in its parse errors.
		return nil, fmt.Errorf("parse postgres connection_string: %w", err)
	}
	if cfg.ConnectTimeout > 0 {
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}
	if cfg.StatementTimeout > 0 {
		// A startup parameter, so it is the session default that
		// RESET statement_timeout returns to. Rounded up to whole
		// milliseconds: truncating 0 < t < 1ms to 0 would disable it.
		millis := (cfg.StatementTimeout + time.Millisecond - 1) / time.Millisecond
		poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(int64(millis), 10)
	}
	return poolCfg, nil
}

// Persons returns the Person repository backed by this service.
func (s *Service) Persons() *PersonRepository {
	return &PersonRepository{pool: s.pool}
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
