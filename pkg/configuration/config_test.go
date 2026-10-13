package configuration_test

import (
	"context"
	"testing"
	"time"

	"github.com/SUNET/sis/pkg/configuration"
)

func TestNewAppliesDefaults(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", "")

	cfg, err := configuration.New(context.Background(), "sis")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cfg.SIS.APIServer.Addr != ":8080" {
		t.Fatalf("expected default addr :8080, got %q", cfg.SIS.APIServer.Addr)
	}
	if cfg.SIS.Auth.TokenTTL != time.Hour {
		t.Fatalf("expected default token_ttl 1h, got %s", cfg.SIS.Auth.TokenTTL)
	}
	if cfg.SIS.Bus.URL != "nats://localhost:4222" {
		t.Fatalf("expected default bus url, got %q", cfg.SIS.Bus.URL)
	}
}

func TestEnvOverridesWin(t *testing.T) {
	t.Setenv("SIS_ADDR", ":9999")
	t.Setenv("SIS_DATABASE_URL", "postgres://x")
	t.Setenv("SIS_TOKEN_TTL", "30m")

	cfg, err := configuration.New(context.Background(), "sis")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cfg.SIS.APIServer.Addr != ":9999" {
		t.Fatalf("env override addr failed: %q", cfg.SIS.APIServer.Addr)
	}
	if cfg.SIS.DB.ConnectionString != "postgres://x" {
		t.Fatalf("env override db failed: %q", cfg.SIS.DB.ConnectionString)
	}
	if cfg.SIS.Auth.TokenTTL != 30*time.Minute {
		t.Fatalf("env override token_ttl failed: %s", cfg.SIS.Auth.TokenTTL)
	}
}
