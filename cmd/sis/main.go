// Command sis is the Student Information System service entrypoint. Following
// the SUNET/vc convention, each subsystem is built by a New(ctx, cfg, deps,
// log) constructor that owns its own init (logging, config reads, bootstrap);
// main's only job is to build the top-level Services, register them for
// coordinated shutdown, and close them all on signal.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/SUNET/sis/internal/adapter"
	"github.com/SUNET/sis/pkg/configuration"
	"github.com/SUNET/sis/pkg/logger"
)

// service is any subsystem with a lifecycle; all are closed together on
// shutdown. The adapter composition root is one such Service; future
// top-level Services (e.g. a metrics server) register alongside it.
type service interface {
	Close(ctx context.Context) error
}

func main() {
	ctx := context.Background()
	services := make(map[string]service)
	serviceName := "sis"

	// Configuration comes first so the real logger can honour
	// common.log.{json,level}. A fixed JSON/info bootstrap logger reports a
	// configuration failure, since there is no config to build from yet.
	cfg, err := configuration.New(ctx, serviceName)
	if err != nil {
		logger.New(serviceName, true, "info").New("main").Error("configuration failed", "err", err)
		os.Exit(1)
	}

	log := logger.New(serviceName, cfg.Common.Log.JSON, cfg.Common.Log.Level)
	mainLog := log.New("main")

	// Adapter composition root: outbound persistence (postgres or memory),
	// event publisher, support Clients (readmodel, auth, oidcrp) and the
	// inbound HTTP transport. A single Service owns the lifecycle of all
	// adapters; its Close drains inbound first, then releases outbound.
	adapterSvc, err := adapter.New(ctx, cfg, log)
	if err != nil {
		mainLog.Error("adapter init failed", "err", err)
		os.Exit(1)
	}
	services["adapter"] = adapterSvc

	// Block until interrupted.
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)
	<-termChan
	mainLog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.SIS.HTTPServer.ShutdownTimeout)
	defer cancel()
	for name, svc := range services {
		if err := svc.Close(shutdownCtx); err != nil {
			mainLog.Error("service close failed", "service", name, "err", err)
		}
	}
	mainLog.Info("sis stopped")
}
