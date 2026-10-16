package oidcrp

import (
	"context"
	"encoding/json"
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

// TestExchangeBoundsStalledTokenEndpoint checks the login callback cannot
// hang on a provider that serves discovery but accepts the token request and
// never answers: Exchange must fail after exchangeTimeout even though the
// caller's context has no deadline.
func TestExchangeBoundsStalledTokenEndpoint(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/token", func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release); srv.Close() })

	old := exchangeTimeout
	exchangeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { exchangeTimeout = old })

	cfg := &model.Cfg{}
	cfg.SIS.Auth.OIDCRP.IssuerURL = srv.URL
	cfg.SIS.Auth.OIDCRP.ClientID = "sis"
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	c, err := New(context.Background(), cfg, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Exchange(context.Background(), "code", "nonce")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Exchange to fail on a stalled token endpoint")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exchange did not return: token request is unbounded")
	}
}
