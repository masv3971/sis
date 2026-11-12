package adapter

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestNewMemorySmoke wires the whole graph on the memory store: the file
// import lands in the catalog, the HTTP server answers, and Close stops it.
func TestNewMemorySmoke(t *testing.T) {
	ctx := t.Context()
	cfg := &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: time.Second},
		Adapter:    model.Adapter{Sweeper: model.Sweeper{Interval: time.Minute}},
		FileImport: model.FileImport{Files: []string{"../../testdata/fake_students.json"}},
	}}
	log := logger.NewWithWriter("test", false, "error", io.Discard)

	svc, err := New(ctx, cfg, log)
	require.NoError(t, err, "New")
	require.Nil(t, svc.outbound.pg, "empty connection_string must select the memory store")

	page, err := svc.inbound.readmodel.Persons.List(ctx, port.ListParams{Limit: 1})
	require.NoError(t, err, "imported persons")
	require.NotZero(t, page.Total, "want the file import in the catalog")

	url := "http://" + svc.inbound.http.Addr().String() + "/healthz"
	resp, err := http.Get(url) // #nosec G107 -- the test's own listener
	require.NoError(t, err, "GET /healthz")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /healthz")

	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, svc.Close(closeCtx), "Close")
	if resp, err := http.Get(url); err == nil { // #nosec G107 -- the test's own listener
		_ = resp.Body.Close()
		assert.Fail(t, "server still answering after Close")
	}
}

// freeAddr returns a loopback address nothing listens on, so a test can
// tell afterwards whether something does.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func listening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// TestNewFailureStopsWhatItStarted: the sweeper and the HTTP listener start
// before the AMQP consumer, so a failure there (here a missing CA file) must
// not leave them running while the caller winds down.
func TestNewFailureStopsWhatItStarted(t *testing.T) {
	addr := freeAddr(t)
	cfg := &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: addr, ReadTimeout: time.Second, WriteTimeout: time.Second},
		Adapter: model.Adapter{
			Sweeper: model.Sweeper{Interval: time.Minute},
			Inbound: model.Inbound{AMQPAPI: model.AMQPAPI{
				Enabled: true, URL: "amqps://127.0.0.1:1", Address: "q", Prefetch: 1,
				CAFiles: []string{"/nonexistent/ca.pem"},
			}},
		},
	}}

	svc, err := New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))

	require.ErrorContains(t, err, "ca file")
	assert.Nil(t, svc, "no half-built Service is returned")
	assert.False(t, listening(addr), "the HTTP listener was closed")
}

// cancelAtCall is a context whose Err reports cancellation from the n-th call
// on, so a test can cancel New at one chosen check.
type cancelAtCall struct {
	context.Context
	calls    atomic.Int32
	cancelAt int32 // 0: never
	onCancel func()
}

func (c *cancelAtCall) Err() error {
	call := c.calls.Add(1)
	if c.cancelAt == 0 || call < c.cancelAt {
		return nil
	}
	if call == c.cancelAt && c.onCancel != nil {
		c.onCancel()
	}
	return context.Canceled
}

// TestNewLateCancellationStopsTheAdapters: a startup that ends after a signal
// or the deadline fails, and by then the listener is up. New must close it
// instead of returning a live Service next to the error.
func TestNewLateCancellationStopsTheAdapters(t *testing.T) {
	addr := freeAddr(t)
	cfg := &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: addr, ReadTimeout: time.Second, WriteTimeout: time.Second},
		Adapter:    model.Adapter{Sweeper: model.Sweeper{Interval: time.Minute}},
	}}
	log := logger.NewWithWriter("test", false, "error", io.Discard)

	// Count the checks of a successful startup; the last one is the late check.
	counting := &cancelAtCall{Context: t.Context()}
	svc, err := New(counting, cfg, log)
	require.NoError(t, err, "uncancelled startup")
	require.NoError(t, svc.Close(t.Context()))
	checks := counting.calls.Load()
	require.GreaterOrEqual(t, checks, int32(2), "test setup: an early and a late check")

	var listeningAtCancel bool
	late := &cancelAtCall{Context: t.Context(), cancelAt: checks, onCancel: func() { listeningAtCancel = listening(addr) }}
	svc, err = New(late, cfg, log)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, svc, "no live Service is returned next to the error")
	assert.True(t, listeningAtCancel, "test setup: the listener was up when startup was cancelled")
	assert.False(t, listening(addr), "the listener was closed")
}

// TestNewStopsOnCancelledStartup checks a startup cancelled by a signal or
// sis.startup_timeout fails New instead of serving, also on the memory store
// (which ignores ctx).
func TestNewStopsOnCancelledStartup(t *testing.T) {
	cfg := &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: time.Second},
		Adapter:    model.Adapter{Sweeper: model.Sweeper{Interval: time.Minute}},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := New(ctx, cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	require.ErrorIs(t, err, context.Canceled)
}
