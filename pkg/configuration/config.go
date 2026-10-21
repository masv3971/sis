// Package configuration loads the typed model.Cfg from a YAML file named by the
// SIS_CONFIG_YAML environment variable, applies struct defaults, and validates
// the result. Inspired by SUNET/vc's pkg/configuration.
package configuration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/creasty/defaults"
	"github.com/kelseyhightower/envconfig"
	"gopkg.in/yaml.v3"

	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/model"
)

// envVars is the only environment input: the path to the config file.
type envVars struct {
	ConfigYAML string `envconfig:"SIS_CONFIG_YAML" required:"true"`
}

// New reads SIS_CONFIG_YAML, fills model.Cfg with struct defaults, decodes the
// file over them (rejecting unknown keys), and validates the result.
func New(_ context.Context, _ string) (*model.Cfg, error) {
	var env envVars
	if err := envconfig.Process("", &env); err != nil {
		return nil, err
	}

	// Defaults first, then the file on top: applying defaults after decoding
	// would overwrite explicit zero values (e.g. `json: false` against a
	// `default:"true"` tag), since a zero bool is indistinguishable from unset.
	cfg := &model.Cfg{}
	if err := defaults.Set(cfg); err != nil {
		return nil, fmt.Errorf("apply defaults: %w", err)
	}
	if err := loadFile(env.ConfigYAML, cfg); err != nil {
		return nil, err
	}
	if err := helpers.Check(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	if err := checkProduction(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	if err := checkWebLogin(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	resolveImportPaths(cfg, filepath.Dir(env.ConfigYAML))
	return cfg, nil
}

// checkWebLogin requires the web UI's sign-in issuer to be a trusted one.
// Otherwise the user signs in successfully and every API call then fails
// with 401.
func checkWebLogin(cfg *model.Cfg) error {
	web := cfg.SIS.Adapter.Inbound.AuthAPI.Issuer
	if web == "" {
		return nil
	}
	for _, iss := range cfg.SIS.Auth.Issuers {
		if iss.Issuer == web {
			return nil
		}
	}
	return fmt.Errorf("sis.adapter.inbound.authapi.issuer %q is not in sis.auth.issuers", web)
}

// resolveImportPaths makes relative file paths in the config (import files,
// TLS certificate and key, CA files) relative to the config file's
// directory, so they do not depend on the working directory SIS is started
// from.
func resolveImportPaths(cfg *model.Cfg, dir string) {
	resolve := func(p *string) {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(dir, *p)
		}
	}
	for i := range cfg.SIS.FileImport.Files {
		resolve(&cfg.SIS.FileImport.Files[i])
	}
	resolve(&cfg.SIS.HTTPServer.TLS.CertFile)
	resolve(&cfg.SIS.HTTPServer.TLS.KeyFile)
	for i := range cfg.SIS.Auth.CAFiles {
		resolve(&cfg.SIS.Auth.CAFiles[i])
	}
	for i := range cfg.SIS.Adapter.Inbound.AMQPAPI.CAFiles {
		resolve(&cfg.SIS.Adapter.Inbound.AMQPAPI.CAFiles[i])
	}
}

func loadFile(path string, cfg *model.Cfg) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config %q: %w", path, err)
	}
	if info.IsDir() {
		return errors.New("config path is a directory, not a file")
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	// KnownFields rejects unknown keys, so a typo such as `jwt_secert` fails
	// startup instead of silently falling back to a default.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty file: defaults only
		}
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	// Decode reads one document only. Anything after a "---" separator (even
	// an empty document) would otherwise be ignored without a word, so
	// production settings placed there would silently not apply, and unknown
	// keys or malformed YAML there would go unchecked. Require EOF.
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("parse config %q: %w", path, err)
	default:
		return fmt.Errorf("parse config %q: multiple YAML documents; put all settings in a single document", path)
	}
}

// checkProduction enforces the settings that are only optional for local
// development. With common.production set, every trusted issuer must be
// reached over https (its keys decide who gets in, so they must not be
// fetched over a tamperable channel) and must name an audience (otherwise a
// token the issuer minted for any other service would be accepted here).
// A jwks_uri found by discovery, and every redirect, is held to https at
// run time by internal/auth.
func checkProduction(cfg *model.Cfg) error {
	if !cfg.Common.Production {
		return nil
	}
	for i, iss := range cfg.SIS.Auth.Issuers {
		field := fmt.Sprintf("sis.auth.issuers[%d]", i)
		if !isHTTPS(iss.Issuer) {
			return fmt.Errorf("%s.issuer must use https when common.production is true", field)
		}
		if iss.JWKSURL != "" && !isHTTPS(iss.JWKSURL) {
			return fmt.Errorf("%s.jwks_url must use https when common.production is true", field)
		}
		if iss.Audience == "" {
			return fmt.Errorf("%s.audience is required when common.production is true", field)
		}
	}
	if a := cfg.SIS.Adapter.Inbound.AMQPAPI; a.Enabled {
		if u, err := url.Parse(a.URL); err != nil || !strings.EqualFold(u.Scheme, "amqps") {
			return errors.New("sis.adapter.inbound.amqpapi.url must use amqps when common.production is true")
		}
	}
	return nil
}

func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Scheme, "https")
}
