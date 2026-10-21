package adapter

import (
	"context"
	"io"
	"net/http"
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

	page, err := svc.inbound.readmodel.Persons.ListPersons(ctx, port.ListParams{Limit: 1})
	require.NoError(t, err, "imported persons")
	require.NotZero(t, page.Total, "want the file import in the catalog")

	url := "http://" + svc.inbound.http.Addr().String() + "/healthz"
	resp, err := http.Get(url)
	require.NoError(t, err, "GET /healthz")
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /healthz")

	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, svc.Close(closeCtx), "Close")
	if resp, err := http.Get(url); err == nil {
		resp.Body.Close()
		assert.Fail(t, "server still answering after Close")
	}
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
