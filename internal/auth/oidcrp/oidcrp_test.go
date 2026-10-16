package oidcrp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestNewDisabledWithoutIssuer checks an empty issuer disables the RP.
func TestNewDisabledWithoutIssuer(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	c, err := New(context.Background(), &model.Cfg{}, log)
	if c != nil || err != nil {
		t.Fatalf("New() = %v, %v; want nil, nil", c, err)
	}
}

// TestNewBoundsDiscovery checks a provider that accepts the connection but
// never answers discovery makes New fail after discoveryTimeout instead of
// hanging startup.
func TestNewBoundsDiscovery(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	old := discoveryTimeout
	discoveryTimeout = 100 * time.Millisecond
	t.Cleanup(func() { discoveryTimeout = old })

	cfg := &model.Cfg{}
	cfg.SIS.Auth.OIDCRP.IssuerURL = srv.URL
	log := logger.NewWithWriter("test", false, "error", io.Discard)

	done := make(chan error, 1)
	go func() {
		_, err := New(context.Background(), cfg, log)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected discovery to fail on a stalled provider")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("New did not return: discovery is unbounded")
	}
}
