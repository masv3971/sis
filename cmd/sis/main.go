// Command sis is the Student Information System service entrypoint. Following a
// microservice pattern, each subsystem is a lifecycle service assembled into a
// services map and shut down together on signal.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SUNET/sis/internal/adapter/outbound/events"
	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/config"
	coresvc "github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/httpserver"
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
	cfg := config.Load()

	// Outbound (driven) adapters. Swapped for Postgres + NATS in later phases.
	personRepo := memory.NewPersonRepository()
	publisher := events.NewLogPublisher(logger)

	// Core hub.
	core := coresvc.New(personRepo, publisher, nil)

	// Inbound (driving) adapter: HTTP (gin).
	httpService, err := httpserver.New(ctx, cfg, core, logger)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	for name, svc := range services {
		if err := svc.Close(shutdownCtx); err != nil {
			logger.Error("service close failed", "service", name, "err", err)
		}
	}
	logger.Info("sis stopped")
}
