package adapter

import (
	"context"
	"fmt"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// selectStore picks the outbound persistence backend from configuration and
// returns the port.Store that downstream consumers use. The raw
// *postgres.Service is returned alongside so adapter.Service can own its
// lifecycle (Close releases the pgx pool); it is nil when the in-memory
// store is in use and is never passed outside this package.
//
// The generic doc-repo helper (docRepo) also takes the same *postgres.Service
// handle — nil means "memory". Keeping that handle inside internal/adapter is
// how we stop *postgres.Service leaking through the API surface while still
// letting the generic entity services pick the matching backend.
func selectStore(ctx context.Context, cfg *model.Cfg, log *logger.Log) (*port.Store, *postgres.Service, error) {
	if cfg.SIS.DB.ConnectionString == "" {
		log.Info("db.connection_string unset; using in-memory store")
		return &port.Store{
			Persons:  memory.NewPersonRepository(),
			Accounts: memory.NewAccountRepository(),
			Clients:  memory.NewClientRepository(),
		}, nil, nil
	}
	pg, err := postgres.New(ctx, cfg.SIS.DB.ConnectionString, log.New("postgres"))
	if err != nil {
		return nil, nil, fmt.Errorf("postgres init: %w", err)
	}
	return &port.Store{
		Persons:  pg.Persons(),
		Accounts: pg.Accounts(),
		Clients:  pg.Clients(),
	}, pg, nil
}
