package configuration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SUNET/sis/pkg/configuration"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestNewLoadsFileAndDefaults(t *testing.T) {
	path := writeConfig(t, "sis:\n  db:\n    connection_string: \"postgres://x\"\n")
	t.Setenv("SIS_CONFIG_YAML", path)

	cfg, err := configuration.New(context.Background(), "sis")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cfg.SIS.DB.ConnectionString != "postgres://x" {
		t.Fatalf("expected db from file, got %q", cfg.SIS.DB.ConnectionString)
	}
	// Defaults fill unset fields.
	if cfg.SIS.APIServer.Addr != ":8080" {
		t.Fatalf("expected default addr :8080, got %q", cfg.SIS.APIServer.Addr)
	}
	if cfg.SIS.APIServer.MaxLimit != 500 {
		t.Fatalf("expected default max_limit 500, got %d", cfg.SIS.APIServer.MaxLimit)
	}
	if cfg.SIS.Auth.TokenTTL != time.Hour {
		t.Fatalf("expected default token_ttl 1h, got %s", cfg.SIS.Auth.TokenTTL)
	}
	if cfg.SIS.Bus.URL != "nats://localhost:4222" {
		t.Fatalf("expected default bus url, got %q", cfg.SIS.Bus.URL)
	}
}

func TestNewRequiresConfigPath(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", "")
	if _, err := configuration.New(context.Background(), "sis"); err == nil {
		t.Fatal("expected an error when SIS_CONFIG_YAML is unset")
	}
}

func TestNewErrorsOnMissingFile(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", "/no/such/config.yaml")
	if _, err := configuration.New(context.Background(), "sis"); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}
