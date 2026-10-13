// Command sis is the Student Information System service entrypoint. Following a
// microservice pattern, each subsystem is a lifecycle service assembled into a
// services map and shut down together on signal.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SUNET/sis/internal/adapter/inbound/restadmin"
	"github.com/SUNET/sis/internal/adapter/outbound/events"
	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	coresvc "github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/httpserver"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/configuration"
	"github.com/SUNET/sis/pkg/model"
)

// service is any subsystem with a lifecycle; all are closed together on shutdown.
type service interface {
	Close(ctx context.Context) error
}

func main() {
	ctx := context.Background()
	services := make(map[string]service)
	serviceName := "sis"

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	cfg, err := configuration.New(ctx, serviceName)
	if err != nil {
		logger.Error("configuration failed", "err", err)
		os.Exit(1)
	}

	// Outbound (driven) adapters. Postgres when configured, else in-memory.
	var (
		pgStore     *postgres.Store
		personRepo  port.PersonRepository
		accountRepo port.AccountRepository
		clientRepo  port.ClientRepository
	)
	if cfg.SIS.DB.ConnectionString != "" {
		store, err := postgres.New(ctx, cfg.SIS.DB.ConnectionString, logger)
		if err != nil {
			logger.Error("postgres init failed", "err", err)
			os.Exit(1)
		}
		services["store"] = store
		pgStore = store
		personRepo, accountRepo, clientRepo = store.Persons(), store.Accounts(), store.Clients()
	} else {
		logger.Info("db.connection_string unset; using in-memory store")
		personRepo = memory.NewPersonRepository()
		accountRepo = memory.NewAccountRepository()
		clientRepo = memory.NewClientRepository()
	}
	publisher := events.NewLogPublisher(logger)

	// Core hub.
	core := coresvc.New(personRepo, publisher, nil)

	// Typed entity services, shared by the native /api/v1 resources and the
	// standards read providers (via the catalog).
	orgSvc := entitySvc[domain.Organization](pgStore, publisher)
	sessionSvc := entitySvc[domain.AcademicSession](pgStore, publisher)
	courseSvc := entitySvc[domain.Course](pgStore, publisher)
	offeringSvc := entitySvc[domain.Offering](pgStore, publisher)
	enrollmentSvc := entitySvc[domain.Enrollment](pgStore, publisher)

	resources := []restadmin.Registrar{
		restadmin.Resource[domain.Organization, *domain.Organization]("organizations", orgSvc),
		restadmin.Resource[domain.AcademicSession, *domain.AcademicSession]("academic-sessions", sessionSvc),
		restadmin.Resource[domain.Course, *domain.Course]("courses", courseSvc),
		restadmin.Resource[domain.Offering, *domain.Offering]("offerings", offeringSvc),
		restadmin.Resource[domain.Enrollment, *domain.Enrollment]("enrollments", enrollmentSvc),
	}

	catalog := &readmodel.Catalog{
		Persons:          core,
		Organizations:    orgSvc,
		AcademicSessions: sessionSvc,
		Courses:          courseSvc,
		Offerings:        offeringSvc,
		Enrollments:      enrollmentSvc,
	}

	// Auth: token issuer + service, seeded from config.
	issuer := token.NewIssuer(jwtSecret(cfg, logger), serviceName, cfg.SIS.Auth.TokenTTL)
	authSvc := auth.New(accountRepo, clientRepo, issuer)
	seedBootstrap(ctx, cfg, authSvc, logger)

	// Inbound (driving) adapter: HTTP (gin).
	httpService, err := httpserver.New(ctx, cfg, core, authSvc, issuer, resources, catalog, logger)
	if err != nil {
		logger.Error("httpserver init failed", "err", err)
		os.Exit(1)

	}
	services["httpService"] = httpService

	// Block until interrupted.
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)
	<-termChan
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.SIS.APIServer.ShutdownTimeout)
	defer cancel()
	for name, svc := range services {
		if err := svc.Close(shutdownCtx); err != nil {
			logger.Error("service close failed", "service", name, "err", err)
		}
	}
	logger.Info("sis stopped")
}

// entitySvc builds a typed service for entity T, backed by the Postgres doc
// store when available, else in-memory.
func entitySvc[T any, PT domain.EntityPtr[T]](store *postgres.Store, publisher port.EventPublisher) *coresvc.Resource[T, PT] {
	var repo port.Repository[T, PT]
	if store != nil {
		repo = postgres.NewDocRepo[T, PT](store)
	} else {
		repo = memory.NewDocRepo[T, PT]()
	}
	return coresvc.NewResource[T, PT](repo, publisher, nil)
}

// jwtSecret returns the configured signing secret, or a random ephemeral one
// (dev only — tokens do not survive a restart) with a warning.
func jwtSecret(cfg *model.Cfg, log *slog.Logger) string {
	if cfg.SIS.Auth.JWTSecret != "" {
		return cfg.SIS.Auth.JWTSecret
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	log.Warn("auth.jwt_secret unset; using an ephemeral secret (tokens will not survive restart)")
	return hex.EncodeToString(b)
}

// seedBootstrap upserts the optional admin account and machine client from config.
func seedBootstrap(ctx context.Context, cfg *model.Cfg, authSvc *auth.Service, log *slog.Logger) {
	bs := cfg.SIS.Auth.Bootstrap
	if bs.AdminUsername != "" && bs.AdminPassword != "" {
		if err := authSvc.EnsureAccount(ctx, bs.AdminUsername, bs.AdminPassword,
			[]domain.AuthRole{domain.AuthRoleAdmin}); err != nil {
			log.Error("seed admin account failed", "err", err)
		} else {
			log.Info("seeded bootstrap admin account", "username", bs.AdminUsername)
		}
	}
	if bs.ClientID != "" && bs.ClientSecret != "" {
		scopes := bs.ClientScopes
		if len(scopes) == 0 {
			scopes = []string{domain.ScopeRead, domain.ScopeWrite}
		}

		if err := authSvc.EnsureClient(ctx, bs.ClientID, bs.ClientSecret,
			"bootstrap", scopes); err != nil {
			log.Error("seed client failed", "err", err)
		} else {
			log.Info("seeded bootstrap client", "client_id", bs.ClientID, "scopes", scopes)
		}
	}
}
