// Package configuration loads the typed model.Cfg from an optional YAML file,
// applies struct defaults, overlays environment variables, and validates the
// result. Inspired by SUNET/vc's pkg/configuration.
package configuration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"

	"github.com/SUNET/sis/pkg/model"
)

// New builds the configuration for serviceName. When SIS_CONFIG_YAML names a
// file it is loaded first; defaults fill unset fields, environment variables
// override, and the result is validated.
func New(_ context.Context, _ string) (*model.Cfg, error) {
	cfg := &model.Cfg{}

	if path := os.Getenv("SIS_CONFIG_YAML"); path != "" {
		if err := loadFile(path, cfg); err != nil {
			return nil, err
		}
	}

	if err := defaults.Set(cfg); err != nil {
		return nil, fmt.Errorf("apply defaults: %w", err)
	}

	applyEnvOverrides(cfg)

	if err := validator.New().Struct(cfg); err != nil {
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
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	return nil
}

// applyEnvOverrides lets the established SIS_* variables override file/defaults,
// keeping the env-driven dev and test flow working.
func applyEnvOverrides(cfg *model.Cfg) {
	if v := os.Getenv("SIS_ADDR"); v != "" {
		cfg.SIS.APIServer.Addr = v
	}
	if v := os.Getenv("SIS_DATABASE_URL"); v != "" {
		cfg.SIS.DB.ConnectionString = v
	}
	if v := os.Getenv("SIS_JWT_SECRET"); v != "" {
		cfg.SIS.Auth.JWTSecret = v
	}
	if v := os.Getenv("SIS_TOKEN_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SIS.Auth.TokenTTL = d
		}
	}
	b := &cfg.SIS.Auth.Bootstrap
	if v := os.Getenv("SIS_BOOTSTRAP_ADMIN_USERNAME"); v != "" {
		b.AdminUsername = v
	}
	if v := os.Getenv("SIS_BOOTSTRAP_ADMIN_PASSWORD"); v != "" {
		b.AdminPassword = v
	}
	if v := os.Getenv("SIS_BOOTSTRAP_CLIENT_ID"); v != "" {
		b.ClientID = v
	}
	if v := os.Getenv("SIS_BOOTSTRAP_CLIENT_SECRET"); v != "" {
		b.ClientSecret = v
	}
	if v := os.Getenv("SIS_BOOTSTRAP_CLIENT_SCOPES"); v != "" {
		b.ClientScopes = splitCSV(v)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
