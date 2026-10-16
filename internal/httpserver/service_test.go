package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
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
	return newTestServerWithCfg(t, testCfg(addr))
}

func newTestServerWithCfg(t *testing.T, cfg *model.Cfg) (*Service, error) {
	t.Helper()
	ctx := context.Background()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
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

// TestSwaggerServesSpecOnlyWhenEnabled checks the generated /api/v1 document
// is registered and served, and that disabling the toggle unmounts it.
func TestSwaggerServesSpecOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		cfg := testCfg("127.0.0.1:0")
		cfg.SIS.HTTPServer.Swagger.Enabled = enabled
		s, err := newTestServerWithCfg(t, cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = s.Close(context.Background()) })

		resp, err := http.Get("http://" + s.Addr().String() + "/swagger/doc.json")
		if err != nil {
			t.Fatalf("GET /swagger/doc.json: %v", err)
		}
		var doc struct {
			BasePath string                     `json:"basePath"`
			Paths    map[string]json.RawMessage `json:"paths"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&doc)
		_ = resp.Body.Close()

		if !enabled {
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("disabled: expected 404, got %d", resp.StatusCode)
			}
			continue
		}
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			t.Fatalf("enabled: expected 200 JSON, got %d (%v)", resp.StatusCode, decodeErr)
		}
		if doc.BasePath != "/api/v1" {
			t.Errorf("basePath = %q, want /api/v1", doc.BasePath)
		}
		for _, p := range []string{"/persons", "/persons/{id}", "/enrollments/{id}"} {
			if _, ok := doc.Paths[p]; !ok {
				t.Errorf("spec is missing path %s", p)
			}
		}
	}
}

// TestIDsWithSlashesRoundTrip checks a record whose id and sourcedId contain
// "/" can be read, updated and deleted through /api/v1 and read through both
// standards providers when the slash is sent escaped (%2F), for persons and
// for the generic resources.
func TestIDsWithSlashesRoundTrip(t *testing.T) {
	ctx := context.Background()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:       service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations: service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
	}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Auth.TokenTTL = time.Hour
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	a, err := auth.New(ctx, cfg, memory.NewAccountRepository(), memory.NewClientRepository(), log)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	s, err := New(ctx, cfg, a, catalog, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tok, _, err := a.Issuer().Issue("writer", token.KindClient, nil, []string{domain.ScopeRead, domain.ScopeWrite})
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + s.Addr().String()

	do := func(t *testing.T, method, path, body string, want int) map[string]any {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, base+path, rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status %d, want %d; body=%s", method, path, resp.StatusCode, want, raw)
		}
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}
	esc := url.PathEscape

	t.Run("persons", func(t *testing.T) {
		const id, sid = "external/42", "src/42"
		do(t, http.MethodPost, "/api/v1/persons",
			`{"id":"`+id+`","sourcedId":"`+sid+`","givenName":"A","familyName":"B","status":"active"}`, http.StatusCreated)
		if got := do(t, http.MethodGet, "/api/v1/persons/"+esc(id), "", http.StatusOK)["id"]; got != id {
			t.Fatalf("GET id = %v, want %q", got, id)
		}
		got := do(t, http.MethodPut, "/api/v1/persons/"+esc(id),
			`{"givenName":"C","familyName":"B","status":"active"}`, http.StatusOK)
		if got["id"] != id || got["givenName"] != "C" {
			t.Fatalf("PUT = %v", got)
		}
		do(t, http.MethodGet, "/oeapi/persons/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodGet, "/eduapi/persons/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodDelete, "/api/v1/persons/"+esc(id), "", http.StatusNoContent)
		do(t, http.MethodGet, "/api/v1/persons/"+esc(id), "", http.StatusNotFound)
	})

	t.Run("generic resource", func(t *testing.T) {
		const id, sid = "org/1", "uni/1"
		do(t, http.MethodPost, "/api/v1/organizations",
			`{"id":"`+id+`","sourcedId":"`+sid+`","name":"U"}`, http.StatusCreated)
		if got := do(t, http.MethodGet, "/api/v1/organizations/"+esc(id), "", http.StatusOK)["id"]; got != id {
			t.Fatalf("GET id = %v, want %q", got, id)
		}
		got := do(t, http.MethodPut, "/api/v1/organizations/"+esc(id), `{"name":"V"}`, http.StatusOK)
		if got["id"] != id || got["name"] != "V" {
			t.Fatalf("PUT = %v", got)
		}
		do(t, http.MethodGet, "/oeapi/organizations/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodGet, "/eduapi/orgs/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodDelete, "/api/v1/organizations/"+esc(id), "", http.StatusNoContent)
		do(t, http.MethodGet, "/api/v1/organizations/"+esc(id), "", http.StatusNotFound)
	})
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
