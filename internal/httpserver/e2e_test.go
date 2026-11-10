package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/logger"
)

// TestServeE2E is not a test: Playwright (web/e2e) starts it to get a SIS
// with the built SPA, an in-memory catalog and a local token issuer. It
// skips unless SIS_E2E_ADDR is set, so go test ./... never runs it.
//
// It writes a read and a read/write token to SIS_E2E_TOKENS, then serves
// until it is signalled.
func TestServeE2E(t *testing.T) {
	addr := os.Getenv("SIS_E2E_ADDR")
	if addr == "" {
		t.Skip("set SIS_E2E_ADDR to serve the SPA for the browser tests")
	}
	tokensFile := os.Getenv("SIS_E2E_TOKENS")
	require.NotEmpty(t, tokensFile, "SIS_E2E_TOKENS must name the file the tokens are written to")
	// A file from an earlier run holds tokens of another key.
	require.NoError(t, removeIfExists(tokensFile))

	webRoot, err := filepath.Abs("../../web/dist")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(webRoot, "index.html"), "build the SPA first: make web-build")

	cfg := testCfg(addr)
	cfg.SIS.HTTPServer.ReadTimeout = 10 * time.Second
	cfg.SIS.HTTPServer.WriteTimeout = 10 * time.Second
	cfg.SIS.HTTPServer.Web.Root = webRoot
	cfg.SIS.Adapter.Inbound.RestAdmin.DefaultLimit, cfg.SIS.Adapter.Inbound.RestAdmin.MaxLimit = 50, 500
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	srv, iss := startWithIssuer(t, cfg, memCatalog(time.Now, log))

	tokens, err := json.Marshal(map[string]string{
		"reader": iss.Token(t, "e2e-reader", "sis.read"),
		"writer": iss.Token(t, "e2e-writer", "sis.read sis.write"),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokensFile, tokens, 0o600))

	t.Logf("serving on http://%s", srv.Addr())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	<-sig
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
