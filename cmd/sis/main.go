// Command sis is the Student Information System service entrypoint. Following
// the SUNET/vc convention, each subsystem is built by a New(ctx, cfg, deps,
// log) constructor that owns its own init (logging, config reads, bootstrap);
// main's only job is to build the top-level Services, register them for
// coordinated shutdown, and close them all on signal.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/SUNET/sis/internal/adapter"
	"github.com/SUNET/sis/pkg/configuration"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/metric"
	"github.com/SUNET/sis/pkg/trace"
)

// service is any subsystem with a lifecycle; all are closed together on
// shutdown. The adapter composition root is one such Service, beside the
// tracer and the meter.
type service interface {
	Close(ctx context.Context) error
}

// namedService is a registered service, for the log line when its Close fails.
type namedService struct {
	name string
	svc  service
}

func main() {
	// ctx is cancelled by SIGINT/SIGTERM, so a signal during startup stops
	// it too. Long-lived subsystems detach from its cancellation
	// (context.WithoutCancel) and stop only on Close.
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	// services close in reverse order of registration.
	var services []namedService
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

	// Telemetry first, so everything built below can find the OpenTelemetry
	// globals, and closed last, so it exports what shutdown itself records.
	// Both are no-ops unless common.tracing / common.metrics are enabled.
	tracer, err := trace.New(ctx, cfg, serviceName, log)
	if err != nil {
		mainLog.Error("tracer init failed", "err", err)
		os.Exit(1)
	}
	services = append(services, namedService{"tracer", tracer})
	meter, err := metric.New(ctx, cfg, serviceName, log)
	if err != nil {
		mainLog.Error("meter init failed", "err", err)
		os.Exit(1) //nolint:gocritic // nothing worth flushing yet; the OS reaps the rest
	}
	services = append(services, namedService{"meter", meter})

	// Adapter composition root: outbound persistence (postgres or memory),
	// event publisher, support Clients (readmodel, auth) and the inbound
	// transports. A single Service owns the lifecycle of all adapters; its
	// Close drains inbound first, then releases outbound. Startup is
	// bounded by sis.startup_timeout.
	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.SIS.StartupTimeout)
	adapterSvc, err := adapter.New(startupCtx, cfg, log)
	if err == nil && startupCtx.Err() != nil {
		// Finished, but after a signal or the deadline: do not serve.
		err = fmt.Errorf("startup: %w", startupCtx.Err())
	}
	cancelStartup()
	if err != nil {
		mainLog.Error("adapter init failed", "err", err)
		os.Exit(1) //nolint:gocritic // nothing to close yet; the OS reaps the rest
	}
	services = append(services, namedService{"adapter", adapterSvc})

	// Block until interrupted.
	<-ctx.Done()
	stopSignals() // a second signal now kills the process at once
	mainLog.Info("shutdown signal received")

	// One budget, sis.shutdown_timeout, bounds the whole teardown of every
	// Service (for the adapter: inbound drain, sweeper, postgres pool).
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.SIS.ShutdownTimeout)
	defer cancel()
	for _, registered := range slices.Backward(services) {
		if err := registered.svc.Close(shutdownCtx); err != nil {
			mainLog.Error("service close failed", "service", registered.name, "err", err)
		}
	}
	mainLog.Info("sis stopped")
}
