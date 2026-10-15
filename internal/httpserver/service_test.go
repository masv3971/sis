package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func testCfg(addr string) *model.Cfg {
	return &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: addr, ReadTimeout: time.Second, WriteTimeout: time.Second},
		Auth:       model.Auth{JWTSecret: "0123456789"},
	}}
}

func newTestServer(t *testing.T, addr string) (*Service, error) {
	t.Helper()
	ctx := context.Background()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	cfg := testCfg(addr)
	a, err := auth.New(ctx, cfg, memory.NewAccountRepository(), memory.NewClientRepository(), log)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return New(ctx, cfg, a, &readmodel.Client{}, log)
}

func TestNewServesOnceReturned(t *testing.T) {
	s, err := newTestServer(t, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	// No retry loop: New's contract is that the listener is bound on return.
	resp, err := http.Get("http://" + s.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthConfigReportsOIDCDisabled(t *testing.T) {
	s, err := newTestServer(t, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	resp, err := http.Get("http://" + s.Addr().String() + "/auth/config")
	if err != nil {
		t.Fatalf("GET /auth/config: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		OIDC *bool `json:"oidc"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body.OIDC == nil || *body.OIDC {
		t.Fatalf("expected 200 {\"oidc\":false}, got %d %+v", resp.StatusCode, body)
	}
}

func TestNewFailsWhenAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	if s, err := newTestServer(t, ln.Addr().String()); err == nil {
		_ = s.Close(context.Background())
		t.Fatal("expected New to fail on a busy address")
	}
}

// TestCloseCancelsInFlightRequestsAfterDeadline checks that a handler blocked
// on downstream work (simulated by waiting on its request context) is aborted
// once the shutdown deadline passes, and that Close returns within it.
func TestCloseCancelsInFlightRequestsAfterDeadline(t *testing.T) {
	started, aborted := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(aborted)
	})
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	s, err := start(context.Background(), testCfg("127.0.0.1:0").SIS.HTTPServer, h, log)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	go func() {
		if resp, err := http.Get("http://" + s.Addr().String() + "/"); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if err := s.Close(ctx); err == nil {
		t.Fatal("expected deadline error from Close")
	}
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("Close took %v, want it bounded by ctx", d)
	}
	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("in-flight request context was not cancelled")
	}
}
