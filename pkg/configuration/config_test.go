package configuration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/configuration"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600), "write config")
	return path
}

// assertErr checks err against wantErr: "" means no error, anything else
// must appear in the error message.
func assertErr(t *testing.T, wantErr string, err error) {
	t.Helper()
	if wantErr == "" {
		assert.NoError(t, err)
		return
	}
	assert.ErrorContains(t, err, wantErr)
}

func TestNewLoadsFileAndDefaults(t *testing.T) {
	path := writeConfig(t, "sis:\n  adapter:\n    outbound:\n      postgres:\n        connection_string: \"postgres://x\"\n")
	t.Setenv("SIS_CONFIG_YAML", path)

	cfg, err := configuration.New(t.Context(), "sis")
	require.NoError(t, err)
	assert.Equal(t, "postgres://x", cfg.SIS.Adapter.Outbound.Postgres.ConnectionString, "connection string from file")
	// Defaults fill unset fields.
	assert.Equal(t, ":8080", cfg.SIS.HTTPServer.Addr)
	assert.Equal(t, 2*time.Minute, cfg.SIS.StartupTimeout)
	assert.Equal(t, 10*time.Second, cfg.SIS.ShutdownTimeout)
	inbound := cfg.SIS.Adapter.Inbound
	assert.Equal(t, 50, inbound.RestAdmin.DefaultLimit)
	assert.Equal(t, 500, inbound.RestAdmin.MaxLimit)
	assert.Equal(t, 100, inbound.EduAPI.DefaultLimit)
	assert.Equal(t, 500, inbound.EduAPI.MaxLimit)
	assert.Equal(t, 10, inbound.OEAPI.DefaultPageSize)
	assert.Equal(t, 500, inbound.OEAPI.MaxPageSize)
	assert.Equal(t, "openid sis.read sis.write", inbound.AuthAPI.Scope)
	assert.Equal(t, time.Minute, cfg.SIS.Adapter.Sweeper.Interval)
	assert.Equal(t, 30*time.Second, cfg.SIS.Adapter.Sweeper.Timeout)
	assert.Equal(t, "nats://localhost:4222", cfg.SIS.Adapter.Outbound.Events.URL)
	assert.Equal(t, 10*time.Second, cfg.SIS.Adapter.Outbound.Postgres.ConnectTimeout)
	assert.Equal(t, 30*time.Second, cfg.SIS.Adapter.Outbound.Postgres.StatementTimeout)
	amqpCfg := inbound.AMQPAPI
	assert.False(t, amqpCfg.Enabled)
	assert.Equal(t, 10, amqpCfg.Prefetch)
	assert.Equal(t, int64(5<<20), amqpCfg.MaxMessageBytes)
	assert.Equal(t, 2*time.Minute, amqpCfg.CommandTimeout)
}

func TestNewTimeoutRules(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"sweep may take the whole interval", "sis:\n  adapter:\n    sweeper:\n      interval: 1m\n      timeout: 1m\n", ""},
		{"sweep longer than interval", "sis:\n  adapter:\n    sweeper:\n      interval: 1m\n      timeout: 2m\n", "sweeper.timeout"},
		{"zero startup timeout", "sis:\n  startup_timeout: 0s\n", "sis.startup_timeout"},
		{"zero shutdown timeout", "sis:\n  shutdown_timeout: 0s\n", "sis.shutdown_timeout"},
		{"shutdown timeout is not under httpserver", "sis:\n  httpserver:\n    shutdown_timeout: 10s\n", "shutdown_timeout"},
		{"zero command timeout", "sis:\n  adapter:\n    inbound:\n      amqpapi:\n        command_timeout: 0s\n", "amqpapi.command_timeout"},
		{"postgres timeouts may be 0", "sis:\n  adapter:\n    outbound:\n      postgres:\n        connect_timeout: 0s\n        statement_timeout: 0s\n", ""},
		{"negative statement timeout", "sis:\n  adapter:\n    outbound:\n      postgres:\n        statement_timeout: -1s\n", "postgres.statement_timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, tc.body))
			_, err := configuration.New(t.Context(), "sis")
			assertErr(t, tc.wantErr, err)
		})
	}
}

func TestNewAMQPAPIRules(t *testing.T) {
	amqp := func(prod bool, body string) string {
		common := ""
		if prod {
			common = "common:\n  production: true\n"
		}
		return common + "sis:\n  adapter:\n    inbound:\n      amqpapi:\n" + body
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, tc.body))
			_, err := configuration.New(t.Context(), "sis")
			assertErr(t, tc.wantErr, err)
		})
	}
}

// TestMaxBelowDefaultRejected checks that a maximum smaller than its default
// fails startup rather than silently shrinking the default.
func TestMaxBelowDefaultRejected(t *testing.T) {
	body := "sis:\n  adapter:\n    inbound:\n      oeapi:\n        default_page_size: 50\n        max_page_size: 20\n"
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, body))
	_, err := configuration.New(t.Context(), "sis")
	assert.ErrorContains(t, err, "max_page_size")
}

func TestNewRejectsUnknownKeys(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, "sis:\n  auth:\n    issuer: \"https://idp.example\"\n"))
	_, err := configuration.New(t.Context(), "sis")
	assert.ErrorContains(t, err, "issuer", "unknown-key error names the key")
}

// TestExplicitFalseOverridesTrueDefault guards against defaults clobbering an
// explicit zero value from the file (common.log.json defaults to true).
func TestExplicitFalseOverridesTrueDefault(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, "common:\n  log:\n    json: false\n    level: debug\n"))
	cfg, err := configuration.New(t.Context(), "sis")
	require.NoError(t, err)
	assert.False(t, cfg.Common.Log.JSON, "explicit json: false was overwritten by the default")
	assert.Equal(t, "debug", cfg.Common.Log.Level)
	assert.Equal(t, ":8080", cfg.SIS.HTTPServer.Addr, "defaults must still fill unset fields")
}

func TestNewAcceptsEmptyFile(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", writeConfig(t, ""))
	_, err := configuration.New(t.Context(), "sis")
	assert.NoError(t, err, "empty file should load with defaults")
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
			_, err := configuration.New(t.Context(), "sis")
			assert.Error(t, err, "a config file with more than one document")
		})
	}
	// A leading "---" or trailing "..." still marks a single document.
	for name, body := range map[string]string{
		"leading separator": "---\ncommon:\n  production: false\n",
		"document end":      "common:\n  production: false\n...\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, body))
			_, err := configuration.New(t.Context(), "sis")
			assert.NoError(t, err, "single document should load")
		})
	}
}

func TestNewAuthRules(t *testing.T) {
	issuers := func(entries ...string) string {
		return "  auth:\n    issuers:\n" + strings.Join(entries, "")
	}
	issuer := func(url, audience, jwksURL string) string {
		entry := "      - issuer: \"" + url + "\"\n"
		if audience != "" {
			entry += "        audience: \"" + audience + "\"\n"
		}
		if jwksURL != "" {
			entry += "        jwks_url: \"" + jwksURL + "\"\n"
		}
		return entry
	}
	const prod = "common:\n  production: true\nsis:\n"
	const dev = "sis:\n"
	web := func(webIssuer string) string {
		return "  adapter:\n    inbound:\n      authapi:\n        issuer: \"" + webIssuer + "\"\n        client_id: sis-web\n"
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SIS_CONFIG_YAML", writeConfig(t, tc.body))
			_, err := configuration.New(t.Context(), "sis")
			assertErr(t, tc.wantErr, err)
		})
	}
}

// TestFileImportPathsAreRelativeToConfig checks relative file_import paths
// resolve against the config file's directory, not the working directory.
func TestFileImportPathsAreRelativeToConfig(t *testing.T) {
	path := writeConfig(t, "sis:\n  file_import:\n    files:\n      - testdata/students.json\n      - /abs/other.json\n")
	t.Setenv("SIS_CONFIG_YAML", path)
	cfg, err := configuration.New(t.Context(), "sis")
	require.NoError(t, err)
	want := []string{filepath.Join(filepath.Dir(path), "testdata/students.json"), "/abs/other.json"}
	assert.Equal(t, want, cfg.SIS.FileImport.Files)
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
			cfg, err := configuration.New(t.Context(), "sis")
			require.NoError(t, err, "%s no longer matches pkg/model", name)
			for _, importFile := range cfg.SIS.FileImport.Files {
				assert.FileExists(t, importFile, "file_import")
			}
		})
	}
}

func TestNewRequiresConfigPath(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", "")
	_, err := configuration.New(t.Context(), "sis")
	assert.Error(t, err, "SIS_CONFIG_YAML unset")
}

func TestNewErrorsOnMissingFile(t *testing.T) {
	t.Setenv("SIS_CONFIG_YAML", "/no/such/config.yaml")
	_, err := configuration.New(t.Context(), "sis")
	assert.Error(t, err, "missing config file")
}
