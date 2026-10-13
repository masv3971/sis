// Package httpserver is the inbound HTTP adapter: a gin engine served over an
// http.Server, exposed as a lifecycle service (New/Close) so main can manage it
// alongside the other microservice subsystems.
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/adapter/inbound/authapi"
	eduapiprovider "github.com/SUNET/sis/internal/adapter/inbound/eduapi"
	oeapiprovider "github.com/SUNET/sis/internal/adapter/inbound/oeapi"
	"github.com/SUNET/sis/internal/adapter/inbound/restadmin"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/model"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Service owns the gin engine and the HTTP server.
type Service struct {
	server *http.Server
	log    *slog.Logger
}

// New builds the gin engine, registers routes, and starts serving in the
// background. It returns once the listener is accepting connections.
func New(_ context.Context, cfg *model.Cfg, core *service.Service, authSvc *auth.Service, issuer *token.Issuer, resources []restadmin.Registrar, catalog *readmodel.Catalog, log *slog.Logger) (*Service, error) {
	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderDisallowUnknownFields() // reject unknown fields on writes

	engine := gin.New()
	engine.Use(requestLogger(log), gin.Recovery(), bodyLimit(maxBodyBytes))

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Unauthenticated auth endpoints (login + client-credentials token).
	authapi.New(authSvc).Register(engine)

	// Native /api/v1 requires a valid token; reads and writes gate on
	// scope (machine clients) or role (human logins).
	api := engine.Group("/api/v1", auth.RequireAuth(issuer))
	guards := restadmin.Guards{
		Read:  auth.RequireScopeOrRole(domain.ScopeRead, string(domain.AuthRoleAdmin), string(domain.AuthRoleRegistrar), string(domain.AuthRoleReadOnly)),
		Write: auth.RequireScopeOrRole(domain.ScopeWrite, string(domain.AuthRoleAdmin), string(domain.AuthRoleRegistrar)),
	}
	restadmin.New(core).Register(api, guards)
	for _, register := range resources {
		register(api, guards)
	}

	// Standards read/publish providers, behind the read scope.
	oeapiprovider.New(catalog).Register(engine.Group("/oeapi", auth.RequireAuth(issuer), guards.Read))
	eduapiprovider.New(catalog).Register(engine.Group("/eduapi", auth.RequireAuth(issuer), guards.Read))

	s := &Service{
		server: &http.Server{
			Addr:         cfg.SIS.APIServer.Addr,
			Handler:      engine,
			ReadTimeout:  cfg.SIS.APIServer.ReadTimeout,
			WriteTimeout: cfg.SIS.APIServer.WriteTimeout,
		},
		log: log,
	}

	go func() {
		log.Info("sis listening", "addr", cfg.SIS.APIServer.Addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
		}
	}()

	return s, nil
}

// Close gracefully shuts down the HTTP server.
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("stopping http server")
	return s.server.Shutdown(ctx)
}
