package configuration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	path := writeConfig(t, "sis:\n  adapter:\n    outbound:\n      postgres:\n        connection_string: \"postgres://x\"\n")
	t.Setenv("SIS_CONFIG_YAML", path)

	cfg, err := configuration.New(context.Background(), "sis")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := cfg.SIS.Adapter.Outbound.Postgres.ConnectionString; got != "postgres://x" {
		t.Fatalf("expected connection string from file, got %q", got)
	}
	// Defaults fill unset fields.
	if cfg.SIS.HTTPServer.Addr != ":8080" {
		t.Fatalf("expected default addr :8080, got %q", cfg.SIS.HTTPServer.Addr)
	}
	in := cfg.SIS.Adapter.Inbound
	if in.RestAdmin.DefaultLimit != 50 || in.RestAdmin.MaxLimit != 500 {
		t.Fatalf("unexpected restadmin paging defaults: %+v", in.RestAdmin)
	}
	if in.EduAPI.DefaultLimit != 100 || in.EduAPI.MaxLimit != 500 {
		t.Fatalf("unexpected eduapi paging defaults: %+v", in.EduAPI)
	}
	if in.OEAPI.DefaultPageSize != 10 || in.OEAPI.MaxPageSize != 500 {
		t.Fatalf("unexpected oeapi paging defaults: %+v", in.OEAPI)
	}
	if in.AuthAPI.PostLoginRedirectURL != "/" {
		t.Fatalf("expected default post_login_redirect_url /, got %q", in.AuthAPI.PostLoginRedirectURL)
	}
	if cfg.SIS.Auth.TokenTTL != time.Hour {
		t.Fatalf("expected default token_ttl 1h, got %s", cfg.SIS.Auth.TokenTTL)
	}
	if cfg.SIS.Adapter.Outbound.Events.URL != "nats://localhost:4222" {
		t.Fatalf("expected default events url, got %q", cfg.SIS.Adapter.Outbound.Events.URL)
	}
}

// TestMaxBelowDefaultRejected checks that a maximum smaller than its default
// fails startup rather than silently shrinking the default.
func TestMaxBelowDefaultRejected(t *testing.T) {
	body := "sis:\n  adapter:\n    inbound:\n      oeapi:\n        default_page_size: 50\n        max_page_size: 20\n"
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, body))
	_, err := configuration.New(context.Background(), "sis")
	if err == nil || !strings.Contains(err.Error(), "max_page_size") {
		t.Fatalf("expected max_page_size validation error, got %v", err)
	}
}

func TestNewRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, "sis:\n  auth:\n    jwt_secert: \"typo-typo\"\n")
	t.Setenv("SIS_CONFIG_YAML", path)
	_, err := configuration.New(context.Background(), "sis")
	if err == nil || !strings.Contains(err.Error(), "jwt_secert") {
		t.Fatalf("expected unknown-key error naming jwt_secert, got %v", err)
	}
}

// TestExplicitFalseOverridesTrueDefault guards against defaults clobbering an
// explicit zero value from the file (common.log.json defaults to true).
func TestExplicitFalseOverridesTrueDefault(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, "common:\n  log:\n    json: false\n    level: debug\n"))
	cfg, err := configuration.New(context.Background(), "sis")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cfg.Common.Log.JSON {
		t.Fatal("explicit json: false was overwritten by the default")
	}
	if cfg.Common.Log.Level != "debug" {
		t.Fatalf("expected level debug, got %q", cfg.Common.Log.Level)
	}
	if cfg.SIS.HTTPServer.Addr != ":8080" {
		t.Fatalf("defaults must still fill unset fields, got addr %q", cfg.SIS.HTTPServer.Addr)
	}
}

func TestNewAcceptsEmptyFile(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, ""))
	if _, err := configuration.New(context.Background(), "sis"); err != nil {
		t.Fatalf("empty file should load with defaults: %v", err)
	}
}

func TestNewProductionRules(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing jwt_secret", "common:\n  production: true\nsis: {}\n", "jwt_secret"},
		{"http issuer", "common:\n  production: true\nsis:\n  auth:\n    jwt_secret: \"0123456789\"\n    oidcrp:\n      issuer_url: \"http://idp.example\"\n      client_id: c\n      client_secret: s\n      redirect_url: \"https://sis.example/cb\"\n", "https"},
		{"ok", "common:\n  production: true\nsis:\n  auth:\n    jwt_secret: \"0123456789\"\n    oidcrp:\n      issuer_url: \"https://idp.example\"\n      client_id: c\n      client_secret: s\n      redirect_url: \"https://sis.example/cb\"\n", ""},
		{"dev allows http + no secret", "sis:\n  auth:\n    oidcrp:\n      issuer_url: \"http://localhost:9080/realms/x\"\n      client_id: c\n      client_secret: s\n      redirect_url: \"http://localhost:8080/cb\"\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, c.body))
			_, err := configuration.New(context.Background(), "sis")
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestSampleConfigLoads keeps config.yaml.sample in step with pkg/model: with
// unknown keys rejected, any key renamed or removed in the model breaks this.
func TestSampleConfigLoads(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", filepath.Join("..", "..", "config.yaml.sample"))
	if _, err := configuration.New(context.Background(), "sis"); err != nil {
		t.Fatalf("config.yaml.sample no longer matches pkg/model: %v", err)
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
