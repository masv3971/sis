package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/authtest"
	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func testCfg(addr string) *model.Cfg {
	return &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: addr, ReadTimeout: time.Second, WriteTimeout: time.Second},
	}}
}

func newTestServer(t *testing.T, addr string) (*Service, error) {
	t.Helper()
	return newTestServerWithCfg(t, testCfg(addr))
}

func newTestServerWithCfg(t *testing.T, cfg *model.Cfg) (*Service, error) {
	t.Helper()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{}
	return New(context.Background(), cfg, newVerifier(t, cfg), catalog, newCommands(t, catalog), log)
}

func newCommands(t *testing.T, catalog *readmodel.Client) *command.Client {
	t.Helper()
	c, err := command.New(context.Background(), &model.Cfg{}, catalog, logger.NewWithWriter("test", false, "error", io.Discard))
	if err != nil {
		t.Fatalf("command.New: %v", err)
	}
	return c
}

func newVerifier(t *testing.T, cfg *model.Cfg) *auth.Verifier {
	t.Helper()
	v, err := auth.New(context.Background(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return v
}

// startWithIssuer serves catalog behind a fresh local token issuer and
// returns the server and the issuer to mint tokens with.
func startWithIssuer(t *testing.T, cfg *model.Cfg, catalog *readmodel.Client) (*Service, *authtest.Issuer) {
	t.Helper()
	iss := authtest.NewIssuer(t)
	cfg.SIS.Auth.Issuers = []model.Issuer{iss.Config()}
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	s, err := New(context.Background(), cfg, newVerifier(t, cfg), catalog, newCommands(t, catalog), log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s, iss
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

// TestServesTLS checks a configured certificate makes the listener serve
// https, and that a missing key file fails startup.
func TestServesTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler()) // only for its cert
	srv.Close()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	c := srv.TLS.Certificates[0]
	keyDER, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]}))
	mustWrite(t, keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.HTTPServer.TLS = model.TLS{CertFile: certFile, KeyFile: keyFile}
	s, err := newTestServerWithCfg(t, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(c.Certificate[0])
	pool.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"}}}
	resp, err := client.Get("https://" + s.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET https: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.TLS == nil {
		t.Fatalf("https /healthz: %d tls=%v", resp.StatusCode, resp.TLS != nil)
	}

	cfg = testCfg("127.0.0.1:0")
	cfg.SIS.HTTPServer.TLS = model.TLS{CertFile: certFile, KeyFile: filepath.Join(dir, "missing.key")}
	if s, err := newTestServerWithCfg(t, cfg); err == nil {
		_ = s.Close(context.Background())
		t.Fatal("expected startup to fail with a missing key file")
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuthConfigIsPublic(t *testing.T) {
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
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body.Enabled == nil || *body.Enabled {
		t.Fatalf("expected 200 {\"enabled\":false}, got %d %+v", resp.StatusCode, body)
	}
}

// TestTokenIssuingRoutesAreGone checks SIS no longer issues tokens: the old
// login and token endpoints are plain 404s.
func TestTokenIssuingRoutesAreGone(t *testing.T) {
	s, err := newTestServer(t, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	for _, p := range []string{"/auth/login", "/oauth/token", "/auth/oidc/session"} {
		resp, err := http.Post("http://"+s.Addr().String()+p, "application/x-www-form-urlencoded", strings.NewReader("grant_type=client_credentials"))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s: %d, want 404", p, resp.StatusCode)
		}
	}
}

// TestScopesGateAPIs checks reads need sis.read and writes sis.write on every
// API surface, and that write does not include read.
func TestScopesGateAPIs(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{Persons: service.New(memory.NewPersonRepository(), nil, nil, log)}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.RestAdmin.DefaultLimit, cfg.SIS.Adapter.Inbound.RestAdmin.MaxLimit = 10, 10
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	s, iss := startWithIssuer(t, cfg, catalog)
	base := "http://" + s.Addr().String()

	read, write, both := iss.Token(t, "sunet-vc", "sis.read"), iss.Token(t, "w", "sis.write"), iss.Token(t, "liu", "sis.read sis.write")
	status := func(tok, method, path, body string) int {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	person := `{"sourcedId":"p1","givenName":"A","familyName":"B","status":"active"}`
	cases := []struct {
		name, tok, method, path, body string
		want                          int
	}{
		{"read cannot create", read, http.MethodPost, "/api/v1/persons", person, http.StatusForbidden},
		{"write creates", write, http.MethodPost, "/api/v1/persons", person, http.StatusCreated},
		{"write cannot list", write, http.MethodGet, "/api/v1/persons", "", http.StatusForbidden},
		{"write cannot use eduapi", write, http.MethodGet, "/eduapi/persons", "", http.StatusForbidden},
		{"read lists", read, http.MethodGet, "/api/v1/persons", "", http.StatusOK},
		{"read eduapi", read, http.MethodGet, "/eduapi/persons/p1", "", http.StatusOK},
		{"read oeapi", read, http.MethodGet, "/oeapi/persons/p1", "", http.StatusOK},
		{"both lists", both, http.MethodGet, "/api/v1/persons", "", http.StatusOK},
		{"no token", "", http.MethodGet, "/eduapi/persons", "", http.StatusUnauthorized},
		{"untrusted issuer", authtest.NewIssuer(t).Token(t, "x", "sis.read sis.write"), http.MethodGet, "/api/v1/persons", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := status(c.tok, c.method, c.path, c.body); got != c.want {
			t.Errorf("%s: %s %s = %d, want %d", c.name, c.method, c.path, got, c.want)
		}
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
	_ = ctx
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	s, iss := startWithIssuer(t, cfg, catalog)
	tok := iss.Token(t, "writer", "sis.read sis.write")
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
