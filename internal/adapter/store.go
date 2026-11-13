package adapter

import (
	"context"
	"fmt"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// selectStore picks the outbound persistence backend from configuration. It
// returns the raw *postgres.Service so adapter.Service can own its lifecycle
// (Close releases the pgx pool); it is nil when the in-memory store is in
// use and is never passed outside this package.
//
// The generic doc-repo helper (docRepo) takes the same *postgres.Service
// handle — nil means "memory". Keeping that handle inside internal/adapter is
// how we stop *postgres.Service leaking through the API surface while still
// letting the entity services pick the matching backend.
func selectStore(ctx context.Context, cfg *model.Cfg, log *logger.Log) (*postgres.Service, error) {
	if cfg.SIS.Adapter.Outbound.Postgres.ConnectionString == "" {
		log.Info("sis.adapter.outbound.postgres.connection_string unset; using in-memory store")
		return nil, nil
	}
	pg, err := postgres.New(ctx, cfg, log.New("postgres"))
	if err != nil {
		return nil, fmt.Errorf("postgres init: %w", err)
	}
	return pg, nil
}
