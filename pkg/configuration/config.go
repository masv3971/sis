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
	return cfg, nil
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
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	return nil
}

// checkProduction enforces the settings that are only optional for local
// development. With common.production set, an ephemeral JWT secret (random
// per process, so tokens break across restarts and replicas) and a plain-HTTP
// OIDC issuer are refused.
func checkProduction(cfg *model.Cfg) error {
	if !cfg.Common.Production {
		return nil
	}
	if cfg.SIS.Auth.JWTSecret == "" {
		return errors.New("sis.auth.jwt_secret is required when common.production is true")
	}
	if issuer := cfg.SIS.Auth.OIDCRP.IssuerURL; issuer != "" {
		u, err := url.Parse(issuer)
		if err != nil || !strings.EqualFold(u.Scheme, "https") {
			return errors.New("sis.auth.oidcrp.issuer_url must use https when common.production is true")
		}
	}
	return nil
}
