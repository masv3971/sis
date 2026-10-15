// Package adapter is the composition root for every adapter in the system.
// It owns the full wiring graph — outbound persistence (postgres or memory),
// the event publisher, the support Clients that glue outbound to inbound
// (readmodel, auth, oidcrp), and the inbound HTTP transport — and exposes it
// as a single lifecycle Service so main's only job is to build it and close
// it on signal.
//
// Service vs Client: this is a Service rather than a Client because it owns
// runtime resources transitively (the postgres pool and the HTTP listener,
// via its sub-Services) and therefore has a Close that must run on shutdown.
// Close drains the inbound side first (so no new work enters the graph) and
// then releases the outbound pool.
package adapter

import (
	"context"
	"errors"

	"github.com/SUNET/sis/internal/adapter/outbound/events"
	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/httpserver"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Outbound bundles the driven adapters: everything the application calls out
// to. pg is the lifecycle handle for the pgx pool (nil when the in-memory
// store is in use); store is the port.Store built from the chosen backend;
// events is the domain-event publisher (log-only today, NATS later).
type Outbound struct {
	pg     *postgres.Service
	store  *port.Store
	events *events.Client
}

// Inbound bundles the driving adapters and the support Clients only inbound
// consumers see. http is the HTTP transport Service; readmodel is the shared
// catalog of per-entity services consumed by every HTTP handler; auth is the
// composition root for authentication (holds the optional OIDC RP). These
// three are grouped here because today only the HTTP transport reads them;
// when a second inbound (e.g. gRPC) arrives it will share this bundle.
type Inbound struct {
	readmodel *readmodel.Client
	auth      *auth.Client
	http      *httpserver.Service
}

// Service owns every adapter plus the support Clients that glue them
// together, grouped into Outbound (driven) and Inbound (driving + support).
// It is a "Service" rather than a "Client" because it transitively owns
// runtime resources (the postgres pool and the HTTP listener) that must be
// released on shutdown via Close.
type Service struct {
	outbound Outbound
	inbound  Inbound
	log      *logger.Log
}

// New builds the full adapter graph. It picks the persistence backend
// (postgres when configured, else in-memory), constructs the event publisher
// and the support Clients, then starts the HTTP server. The returned Service
// is the single lifecycle handle the caller must Close on shutdown.
//
// On init failure New returns the error immediately without closing partially
// acquired resources: today the only such resource is the postgres pool and
// main always exits on error, so the OS reaps it. If a future sub-adapter
// acquires a resource that needs graceful release before process exit (e.g.
// a NATS connection that must drain), add that cleanup here then.
func New(ctx context.Context, cfg *model.Cfg, log *logger.Log) (*Service, error) {
	s := &Service{log: log.New("adapter")}

	// Outbound persistence. selectStore builds the port.Store that
	// downstream consumers use; pg is the raw lifecycle handle owned by
	// this Service and nil when the in-memory store is in use. docRepo
	// routes through the same handle to pick postgres vs memory per
	// entity type.
	var err error
	if s.outbound.store, s.outbound.pg, err = selectStore(ctx, cfg, log); err != nil {
		return nil, err
	}

	// Event-publisher Client (log-only today; NATS will replace it later).
	if s.outbound.events, err = events.New(ctx, cfg, log); err != nil {
		return nil, err
	}

	// Read-model Client: person service + the five typed entity services,
	// used by both /api/v1 and the standards (OEAPI/Edu-API) providers.
	// Built inline here because the generic doc-repo helper must stay in
	// this package (see docrepo.go); readmodel is now a pure bundle type.
	store, pg, events := s.outbound.store, s.outbound.pg, s.outbound.events
	s.inbound.readmodel = &readmodel.Client{
		Persons:          service.New(store.Persons, events, nil, log),
		Organizations:    service.NewResource(docRepo[domain.Organization](pg), events, nil, log),
		AcademicSessions: service.NewResource(docRepo[domain.AcademicSession](pg), events, nil, log),
		Courses:          service.NewResource(docRepo[domain.Course](pg), events, nil, log),
		Offerings:        service.NewResource(docRepo[domain.Offering](pg), events, nil, log),
		Enrollments:      service.NewResource(docRepo[domain.Enrollment](pg), events, nil, log),
	}
	s.log.Info("read-model catalog built")

	// Auth Client: the composition root for authentication. Builds the
	// token issuer, wires the OIDC Relying Party when configured, and
	// seeds any bootstrap credentials. httpserver asks it OIDCEnabled() to
	// decide whether to mount the OIDC routes.
	if s.inbound.auth, err = auth.New(ctx, cfg, store.Accounts, store.Clients, log); err != nil {
		return nil, err
	}

	// Inbound HTTP transport. This is a Service — it owns the listener and
	// background accept loop and must be closed first on shutdown.
	if s.inbound.http, err = httpserver.New(ctx, cfg, s.inbound.auth, s.inbound.readmodel, log.New("httpserver")); err != nil {
		return nil, err
	}

	return s, nil
}

// Close shuts the adapter graph down in dependency order: inbound first (so
// no new work enters the graph), then outbound persistence. Errors from both
// stages are joined so the caller sees every failure.
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("stopping adapters")
	var errs []error
	if s.inbound.http != nil {
		if err := s.inbound.http.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if s.outbound.pg != nil {
		if err := s.outbound.pg.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
