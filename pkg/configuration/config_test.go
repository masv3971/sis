package configuration_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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
	if in.AuthAPI.Scope != "openid sis.read sis.write" {
		t.Fatalf("expected default authapi scope, got %q", in.AuthAPI.Scope)
	}
	if cfg.SIS.Adapter.Sweeper.Interval != time.Minute {
		t.Fatalf("expected default sweeper interval 1m, got %s", cfg.SIS.Adapter.Sweeper.Interval)
	}
	if cfg.SIS.Adapter.Outbound.Events.URL != "nats://localhost:4222" {
		t.Fatalf("expected default events url, got %q", cfg.SIS.Adapter.Outbound.Events.URL)
	}
	if a := in.AMQPAPI; a.Enabled || a.Prefetch != 10 || a.MaxMessageBytes != 5<<20 {
		t.Fatalf("unexpected amqpapi defaults: %+v", a)
	}
}

func TestNewAMQPAPIRules(t *testing.T) {
	amqp := func(prod bool, body string) string {
		s := ""
		if prod {
			s = "common:\n  production: true\n"
		}
		return s + "sis:\n  adapter:\n    inbound:\n      amqpapi:\n" + body
	}
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"disabled needs nothing", amqp(true, "        enabled: false\n"), ""},
		{"enabled needs url", amqp(false, "        enabled: true\n        address: q\n"), "url"},
		{"enabled needs address", amqp(false, "        enabled: true\n        url: amqp://b\n"), "address"},
		{"dev allows amqp", amqp(false, "        enabled: true\n        url: amqp://b\n        address: q\n"), ""},
		{"prod requires amqps", amqp(true, "        enabled: true\n        url: amqp://b\n        address: q\n"), "amqps"},
		{"prod amqps ok", amqp(true, "        enabled: true\n        url: amqps://b\n        address: q\n"), ""},
		{"only sis.write as fallback scope", amqp(false, "        scopes: [sis.read]\n"), "scopes"},
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
	path := writeConfig(t, "sis:\n  auth:\n    issuer: \"https://idp.example\"\n")
	t.Setenv("SIS_CONFIG_YAML", path)
	_, err := configuration.New(context.Background(), "sis")
	if err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("expected unknown-key error naming issuer, got %v", err)
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

// TestNewRejectsMultipleDocuments checks that only a single YAML document is
// accepted: settings after "---" must fail startup rather than be ignored.
func TestNewRejectsMultipleDocuments(t *testing.T) {
	cases := map[string]string{
		"second document with settings": "common:\n  production: false\n---\ncommon:\n  production: true\n",
		"empty second document":         "common:\n  production: false\n---\n",
		"malformed second document":     "common:\n  production: false\n---\n: bad: [\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, body))
			if _, err := configuration.New(context.Background(), "sis"); err == nil {
				t.Fatal("expected an error for a config file with more than one document")
			}
		})
	}
	// A leading "---" or trailing "..." still marks a single document.
	for name, body := range map[string]string{
		"leading separator": "---\ncommon:\n  production: false\n",
		"document end":      "common:\n  production: false\n...\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, body))
			if _, err := configuration.New(context.Background(), "sis"); err != nil {
				t.Fatalf("single document should load: %v", err)
			}
		})
	}
}

func TestNewAuthRules(t *testing.T) {
	issuers := func(entries ...string) string {
		return "  auth:\n    issuers:\n" + strings.Join(entries, "")
	}
	issuer := func(url, aud, jwks string) string {
		s := "      - issuer: \"" + url + "\"\n"
		if aud != "" {
			s += "        audience: \"" + aud + "\"\n"
		}
		if jwks != "" {
			s += "        jwks_url: \"" + jwks + "\"\n"
		}
		return s
	}
	const prod = "common:\n  production: true\nsis:\n"
	const dev = "sis:\n"
	web := func(iss string) string {
		return "  adapter:\n    inbound:\n      authapi:\n        issuer: \"" + iss + "\"\n        client_id: sis-web\n"
	}
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"no issuers", prod + "  auth: {}\n", ""},
		{"prod ok", prod + issuers(issuer("https://idp.example/realms/sis", "sis", "")), ""},
		{"prod http issuer", prod + issuers(issuer("http://idp.example", "sis", "")), "must use https"},
		{"prod http jwks_url", prod + issuers(issuer("https://idp.example", "sis", "http://idp/certs")), "jwks_url must use https"},
		{"prod no audience", prod + issuers(issuer("https://idp.example", "", "")), "audience is required"},
		{"dev allows http, no audience", dev + issuers(issuer("http://localhost:9080/realms/sis", "", "http://mellon:8080/certs")), ""},
		{"several issuers", prod + issuers(issuer("https://a.example", "sis", ""), issuer("https://b.example", "sis", "")), ""},
		{"duplicate issuer", dev + issuers(issuer("https://a.example", "", ""), issuer("https://a.example", "", "")), "unique"},
		{"issuer not a url", dev + issuers(issuer("idp", "", "")), "httpurl"},
		{"web login trusted", dev + issuers(issuer("https://a.example", "", "")) + web("https://a.example"), ""},
		{"web login untrusted", dev + issuers(issuer("https://a.example", "", "")) + web("https://b.example"), "not in sis.auth.issuers"},
		{"web login without client_id", dev + issuers(issuer("https://a.example", "", "")) + "  adapter:\n    inbound:\n      authapi:\n        issuer: \"https://a.example\"\n", "client_id"},
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

// TestFileImportPathsAreRelativeToConfig checks relative file_import paths
// resolve against the config file's directory, not the working directory.
func TestFileImportPathsAreRelativeToConfig(t *testing.T) {
	path := writeConfig(t, "sis:\n  file_import:\n    files:\n      - testdata/students.json\n      - /abs/other.json\n")
	t.Setenv("SIS_CONFIG_YAML", path)
	cfg, err := configuration.New(context.Background(), "sis")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(filepath.Dir(path), "testdata/students.json"), "/abs/other.json"}
	if !slices.Equal(cfg.SIS.FileImport.Files, want) {
		t.Fatalf("files = %v, want %v", cfg.SIS.FileImport.Files, want)
	}
}

// TestShippedConfigsLoad keeps config.yaml.sample and the dev config.yaml
// (when present) in step with pkg/model: with unknown keys rejected, any key
// renamed or removed in the model breaks this. Their import files must exist.
func TestShippedConfigsLoad(t *testing.T) {
	for _, name := range []string{"config.yaml.sample", "config.yaml"} {
		path := filepath.Join("..", "..", name)
		if _, err := os.Stat(path); err != nil {
			continue // config.yaml is local and may be absent
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", path)
			cfg, err := configuration.New(context.Background(), "sis")
			if err != nil {
				t.Fatalf("%s no longer matches pkg/model: %v", name, err)
			}
			for _, f := range cfg.SIS.FileImport.Files {
				if _, err := os.Stat(f); err != nil {
					t.Errorf("file_import: %v", err)
				}
			}
		})
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
