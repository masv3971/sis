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
	"sync"
	"syscall"
	"time"

	"github.com/SUNET/sis/internal/adapter"
	"github.com/SUNET/sis/pkg/configuration"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/metric"
	"github.com/SUNET/sis/pkg/model"
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

// telemetryTimeout is how long closing the tracer and meter may take: the
// longest export timeout of those enabled, so a flush can finish what one
// export request may take.
func telemetryTimeout(cfg *model.Cfg) time.Duration {
	var timeout time.Duration
	if cfg.Common.Tracing.Enabled {
		timeout = cfg.Common.Tracing.Timeout
	}
	if cfg.Common.Metrics.Enabled {
		timeout = max(timeout, cfg.Common.Metrics.Timeout)
	}
	return timeout
}

// closeConcurrently closes every service at once and logs each failure. The
// services are independent, so a stuck one cannot use up the others' time.
func closeConcurrently(ctx context.Context, log *logger.Log, services []namedService) {
	var wg sync.WaitGroup
	for _, registered := range services {
		wg.Go(func() {
			if err := registered.svc.Close(ctx); err != nil {
				log.Error("service close failed", "service", registered.name, "err", err)
			}
		})
	}
	wg.Wait()
}

func main() {
	// ctx is cancelled by SIGINT/SIGTERM, so a signal during startup stops
	// it too. Long-lived subsystems detach from its cancellation
	// (context.WithoutCancel) and stop only on Close.
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	// services close in reverse order of registration; telemetry closes
	// afterwards, on its own deadline.
	var services, telemetry []namedService
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
	// globals, and closed after everything else, so it exports what shutdown
	// itself records. Both are no-ops unless common.tracing / common.metrics
	// are enabled.
	tracer, err := trace.New(ctx, cfg, serviceName, log)
	if err != nil {
		mainLog.Error("tracer init failed", "err", err)
		os.Exit(1)
	}
	telemetry = append(telemetry, namedService{"tracer", tracer})
	meter, err := metric.New(ctx, cfg, serviceName, log)
	if err != nil {
		mainLog.Error("meter init failed", "err", err)
		os.Exit(1) //nolint:gocritic // nothing worth flushing yet; the OS reaps the rest
	}
	telemetry = append(telemetry, namedService{"meter", meter})

	// Adapter composition root: outbound persistence (postgres or memory),
	// event publisher, support Clients (readmodel, auth) and the inbound
	// transports. A single Service owns the lifecycle of all adapters; its
	// Close drains inbound first, then releases outbound. Startup is
	// bounded by sis.startup_timeout.
	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.SIS.StartupTimeout)
	// Root span, so the migrations and file import show up in traces: otelpgx
	// needs a recording parent.
	startupCtx, endStartup := trace.Start(startupCtx, "startup")
	adapterSvc, err := adapter.New(startupCtx, cfg, log)
	if err == nil && startupCtx.Err() != nil {
		// Finished, but after a signal or the deadline: do not serve.
		err = fmt.Errorf("startup: %w", startupCtx.Err())
	}
	endStartup(err)
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

	// sis.shutdown_timeout bounds the teardown of every Service (for the
	// adapter: inbound drain, sweeper, postgres pool).
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.SIS.ShutdownTimeout)
	defer cancel()
	for _, registered := range slices.Backward(services) {
		if err := registered.svc.Close(shutdownCtx); err != nil {
			mainLog.Error("service close failed", "service", registered.name, "err", err)
		}
	}

	// The flush gets its own deadline, so a slow drain cannot cost the last
	// spans and a stalled exporter cannot cost the drain. It runs past
	// sis.shutdown_timeout by at most the longest export timeout.
	telemetryCtx, cancelTelemetry := context.WithTimeout(context.WithoutCancel(ctx), telemetryTimeout(cfg))
	defer cancelTelemetry()
	closeConcurrently(telemetryCtx, mainLog, telemetry)
	mainLog.Info("sis stopped")
}
