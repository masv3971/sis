package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func TestTelemetryTimeout(t *testing.T) {
	cfg := func(traceOn bool, traceTimeout time.Duration, metricsOn bool, metricsTimeout time.Duration) *model.Cfg {
		return &model.Cfg{Common: model.Common{
			Tracing: model.Tracing{Enabled: traceOn, Timeout: traceTimeout},
			Metrics: model.Metrics{Enabled: metricsOn, Timeout: metricsTimeout},
		}}
	}
	cases := map[string]struct {
		cfg  *model.Cfg
		want time.Duration
	}{
		"both off":                      {cfg(false, 10*time.Second, false, 10*time.Second), 0},
		"tracing only":                  {cfg(true, 3*time.Second, false, 10*time.Second), 3 * time.Second},
		"metrics only":                  {cfg(false, 10*time.Second, true, 4*time.Second), 4 * time.Second},
		"the longer of the two":         {cfg(true, 3*time.Second, true, 7*time.Second), 7 * time.Second},
		"the longer, tracing this time": {cfg(true, 9*time.Second, true, 2*time.Second), 9 * time.Second},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, telemetryTimeout(tc.cfg))
		})
	}
}

type closeFunc func(context.Context) error

func (f closeFunc) Close(ctx context.Context) error { return f(ctx) }

// TestCloseConcurrentlyStalledServiceDoesNotStarveTheOther: a service that
// blocks until its deadline must not cost the other any time.
func TestCloseConcurrentlyStalledServiceDoesNotStarveTheOther(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	var otherCtxErr error
	otherDone := make(chan struct{})
	services := []namedService{
		{"stalled", closeFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})},
		{"other", closeFunc(func(ctx context.Context) error {
			otherCtxErr = ctx.Err()
			close(otherDone)
			return nil
		})},
	}
	closeConcurrently(ctx, log, services)

	select {
	case <-otherDone:
	default:
		t.Fatal("the other service was never closed")
	}
	require.NoError(t, otherCtxErr, "the other service started with its deadline intact")
}

// TestCloseTelemetryHasItsOwnDeadline: the flush must not inherit a spent
// shutdown budget or a cancelled context (a signal during startup), and must
// be bounded by the export timeout, so a stalled collector cannot hang exit.
func TestCloseTelemetryHasItsOwnDeadline(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	cfg := &model.Cfg{Common: model.Common{Tracing: model.Tracing{Enabled: true, Timeout: 100 * time.Millisecond}}}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	var flushCtxErr error
	var deadline time.Time
	var hasDeadline bool
	closed := closeFunc(func(ctx context.Context) error {
		flushCtxErr = ctx.Err()
		deadline, hasDeadline = ctx.Deadline()
		return nil
	})
	begin := time.Now()
	closeTelemetry(cancelled, cfg, log, []namedService{{"tracer", closed}})

	require.NoError(t, flushCtxErr, "the flush starts live, although its parent was cancelled")
	require.True(t, hasDeadline, "the flush is bounded")
	assert.WithinDuration(t, begin.Add(100*time.Millisecond), deadline, 50*time.Millisecond, "by the export timeout")
}

func TestCloseTelemetryStalledExporterIsBounded(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	cfg := &model.Cfg{Common: model.Common{Metrics: model.Metrics{Enabled: true, Timeout: 50 * time.Millisecond}}}
	stalled := closeFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	begin := time.Now()
	closeTelemetry(t.Context(), cfg, log, []namedService{{"meter", stalled}})
	assert.Less(t, time.Since(begin), time.Second, "a stalled exporter cannot hang the exit")
}

func TestCloseConcurrentlyClosesEveryServiceDespiteErrors(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	closed := make(chan string, 2)
	services := []namedService{
		{"first", closeFunc(func(context.Context) error { closed <- "first"; return errors.New("boom") })},
		{"second", closeFunc(func(context.Context) error { closed <- "second"; return nil })},
	}
	closeConcurrently(t.Context(), log, services)
	close(closed)

	var got []string
	for name := range closed {
		got = append(got, name)
	}
	assert.ElementsMatch(t, []string{"first", "second"}, got)
}
