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

	"github.com/SUNET/sis/internal/adapter/inbound/restadmin"
	"github.com/SUNET/sis/internal/config"
	"github.com/SUNET/sis/internal/core/service"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Service owns the gin engine and the HTTP server.
type Service struct {
	server *http.Server
	log    *slog.Logger
}

// New builds the gin engine, registers routes, and starts serving in the
// background. It returns once the listener is accepting connections.
func New(_ context.Context, cfg config.Config, core *service.Service, log *slog.Logger) (*Service, error) {
	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderDisallowUnknownFields() // reject unknown fields on writes

	engine := gin.New()
	engine.Use(requestLogger(log), gin.Recovery(), bodyLimit(maxBodyBytes))

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	restadmin.New(core).Register(engine.Group("/api/v1"))

	s := &Service{
		server: &http.Server{
			Addr:         cfg.Addr,
			Handler:      engine,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		},
		log: log,
	}

	go func() {
		log.Info("sis listening", "addr", cfg.Addr)
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
