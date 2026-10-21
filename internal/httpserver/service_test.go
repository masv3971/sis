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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	return New(t.Context(), cfg, newVerifier(t, cfg), catalog, newCommands(t, catalog), log)
}

func newCommands(t *testing.T, catalog *readmodel.Client) *command.Client {
	t.Helper()
	commands, err := command.New(t.Context(), &model.Cfg{}, catalog, logger.NewWithWriter("test", false, "error", io.Discard))
	require.NoError(t, err, "command.New")
	return commands
}

func newVerifier(t *testing.T, cfg *model.Cfg) *auth.Verifier {
	t.Helper()
	verifier, err := auth.New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	require.NoError(t, err, "auth.New")
	return verifier
}

// startWithIssuer serves catalog behind a fresh local token issuer and
// returns the server and the issuer to mint tokens with.
func startWithIssuer(t *testing.T, cfg *model.Cfg, catalog *readmodel.Client) (*Service, *authtest.Issuer) {
	t.Helper()
	iss := authtest.NewIssuer(t)
	cfg.SIS.Auth.Issuers = []model.Issuer{iss.Config()}
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	srv, err := New(t.Context(), cfg, newVerifier(t, cfg), catalog, newCommands(t, catalog), log)
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	return srv, iss
}

func TestNewServesOnceReturned(t *testing.T) {
	srv, err := newTestServer(t, "127.0.0.1:0")
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	// No retry loop: New's contract is that the listener is bound on return.
	resp, err := httpGet(t, http.DefaultClient, "http://"+srv.Addr().String()+"/healthz")
	require.NoError(t, err, "GET /healthz")
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// httpGet is http.Get bound to the test's context.
func httpGet(t *testing.T, client *http.Client, target string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	require.NoError(t, err)
	return client.Do(req)
}

// TestServesTLS checks a configured certificate makes the listener serve
// https, and that a missing key file fails startup.
func TestServesTLS(t *testing.T) {
	certSrv := httptest.NewTLSServer(http.NotFoundHandler()) // only for its cert
	certSrv.Close()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	cert := certSrv.TLS.Certificates[0]
	keyDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	require.NoError(t, err)
	mustWrite(t, certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}))
	mustWrite(t, keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.HTTPServer.TLS = model.TLS{CertFile: certFile, KeyFile: keyFile}
	srv, err := newTestServerWithCfg(t, cfg)
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	pool.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"}}}
	resp, err := httpGet(t, client, "https://"+srv.Addr().String()+"/healthz")
	require.NoError(t, err, "GET https")
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "https /healthz")
	assert.NotNil(t, resp.TLS, "https /healthz must be served over TLS")

	cfg = testCfg("127.0.0.1:0")
	cfg.SIS.HTTPServer.TLS = model.TLS{CertFile: certFile, KeyFile: filepath.Join(dir, "missing.key")}
	if srv, err := newTestServerWithCfg(t, cfg); !assert.Error(t, err, "expected startup to fail with a missing key file") {
		_ = srv.Close(t.Context())
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func TestAuthConfigIsPublic(t *testing.T) {
	srv, err := newTestServer(t, "127.0.0.1:0")
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	resp, err := httpGet(t, http.DefaultClient, "http://"+srv.Addr().String()+"/auth/config")
	require.NoError(t, err, "GET /auth/config")
	defer resp.Body.Close()
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), "decode")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	if assert.NotNil(t, body.Enabled, `expected {"enabled":false}`) {
		assert.False(t, *body.Enabled)
	}
}

// TestTokenIssuingRoutesAreGone checks SIS no longer issues tokens: the old
// login and token endpoints are plain 404s.
func TestTokenIssuingRoutesAreGone(t *testing.T) {
	srv, err := newTestServer(t, "127.0.0.1:0")
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	for _, path := range []string{"/auth/login", "/oauth/token", "/auth/oidc/session"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+srv.Addr().String()+path, strings.NewReader("grant_type=client_credentials"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "POST %s", path)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "POST %s", path)
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
	srv, iss := startWithIssuer(t, cfg, catalog)
	base := "http://" + srv.Addr().String()

	read, write, both := iss.Token(t, "sunet-vc", "sis.read"), iss.Token(t, "w", "sis.write"), iss.Token(t, "liu", "sis.read sis.write")
	status := func(token, method, path, body string) int {
		req, _ := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%s %s", method, path)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	person := `{"sourcedId":"p1","givenName":"A","familyName":"B","status":"active"}`
	cases := []struct {
		name, token, method, path, body string
		want                            int
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
	for _, tc := range cases {
		assert.Equal(t, tc.want, status(tc.token, tc.method, tc.path, tc.body), "%s: %s %s", tc.name, tc.method, tc.path)
	}
}

// TestSwaggerServesSpecOnlyWhenEnabled checks the generated /api/v1 document
// is registered and served, and that disabling the toggle unmounts it.
func TestSwaggerServesSpecOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		cfg := testCfg("127.0.0.1:0")
		cfg.SIS.HTTPServer.Swagger.Enabled = enabled
		srv, err := newTestServerWithCfg(t, cfg)
		require.NoError(t, err, "New")
		t.Cleanup(func() { _ = srv.Close(context.Background()) })

		resp, err := httpGet(t, http.DefaultClient, "http://"+srv.Addr().String()+"/swagger/doc.json")
		require.NoError(t, err, "GET /swagger/doc.json")
		var doc struct {
			BasePath string                     `json:"basePath"`
			Paths    map[string]json.RawMessage `json:"paths"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&doc)
		_ = resp.Body.Close()

		if !enabled {
			require.Equal(t, http.StatusNotFound, resp.StatusCode, "disabled")
			continue
		}
		require.Equal(t, http.StatusOK, resp.StatusCode, "enabled")
		require.NoError(t, decodeErr, "enabled: expected JSON")
		assert.Equal(t, "/api/v1", doc.BasePath)
		for _, path := range []string{"/persons", "/persons/{id}", "/enrollments/{id}"} {
			assert.Contains(t, doc.Paths, path, "spec is missing path %s", path)
		}
	}
}

// TestIDsWithSlashesRoundTrip checks a record whose id and sourcedId contain
// "/" can be read, updated and deleted through /api/v1 and read through both
// standards providers when the slash is sent escaped (%2F), for persons and
// for the generic resources.
func TestIDsWithSlashesRoundTrip(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:       service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations: service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
	}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "writer", "sis.read sis.write")
	base := "http://" + srv.Addr().String()

	do := func(t *testing.T, method, path, body string, want int) map[string]any {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequestWithContext(t.Context(), method, base+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%s %s", method, path)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		require.Equal(t, want, resp.StatusCode, "%s %s: body=%s", method, path, raw)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}
	esc := url.PathEscape

	t.Run("persons", func(t *testing.T) {
		const id, sid = "external/42", "src/42"
		do(t, http.MethodPost, "/api/v1/persons",
			`{"id":"`+id+`","sourcedId":"`+sid+`","givenName":"A","familyName":"B","status":"active"}`, http.StatusCreated)
		require.Equal(t, id, do(t, http.MethodGet, "/api/v1/persons/"+esc(id), "", http.StatusOK)["id"], "GET id")
		got := do(t, http.MethodPut, "/api/v1/persons/"+esc(id),
			`{"givenName":"C","familyName":"B","status":"active"}`, http.StatusOK)
		require.Equal(t, id, got["id"], "PUT = %v", got)
		require.Equal(t, "C", got["givenName"], "PUT = %v", got)
		do(t, http.MethodGet, "/oeapi/persons/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodGet, "/eduapi/persons/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodDelete, "/api/v1/persons/"+esc(id), "", http.StatusNoContent)
		do(t, http.MethodGet, "/api/v1/persons/"+esc(id), "", http.StatusNotFound)
	})

	t.Run("generic resource", func(t *testing.T) {
		const id, sid = "org/1", "uni/1"
		do(t, http.MethodPost, "/api/v1/organizations",
			`{"id":"`+id+`","sourcedId":"`+sid+`","name":"U"}`, http.StatusCreated)
		require.Equal(t, id, do(t, http.MethodGet, "/api/v1/organizations/"+esc(id), "", http.StatusOK)["id"], "GET id")
		got := do(t, http.MethodPut, "/api/v1/organizations/"+esc(id), `{"name":"V"}`, http.StatusOK)
		require.Equal(t, id, got["id"], "PUT = %v", got)
		require.Equal(t, "V", got["name"], "PUT = %v", got)
		do(t, http.MethodGet, "/oeapi/organizations/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodGet, "/eduapi/orgs/"+esc(sid), "", http.StatusOK)
		do(t, http.MethodDelete, "/api/v1/organizations/"+esc(id), "", http.StatusNoContent)
		do(t, http.MethodGet, "/api/v1/organizations/"+esc(id), "", http.StatusNotFound)
	})
}

func TestNewFailsWhenAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	t.Cleanup(func() { _ = ln.Close() })

	if srv, err := newTestServer(t, ln.Addr().String()); !assert.Error(t, err, "expected New to fail on a busy address") {
		_ = srv.Close(t.Context())
	}
}

// TestCloseCancelsInFlightRequestsAfterDeadline checks that a handler blocked
// on downstream work (simulated by waiting on its request context) is aborted
// once the shutdown deadline passes, and that Close returns within it.
func TestCloseCancelsInFlightRequestsAfterDeadline(t *testing.T) {
	started, aborted := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
		close(aborted)
	})
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	srv, err := start(t.Context(), testCfg("127.0.0.1:0").SIS.HTTPServer, handler, log)
	require.NoError(t, err, "start")

	go func() {
		if resp, err := http.Get("http://" + srv.Addr().String() + "/"); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	begin := time.Now()
	require.Error(t, srv.Close(ctx), "expected deadline error from Close")
	assert.LessOrEqual(t, time.Since(begin), time.Second, "Close must be bounded by ctx")
	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("in-flight request context was not cancelled")
	}
}
