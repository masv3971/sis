// Package configuration loads the typed model.Cfg from a YAML file named by the
// SIS_CONFIG_YAML environment variable, applies struct defaults, and validates
// the result. Inspired by SUNET/vc's pkg/configuration.
package configuration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// New reads SIS_CONFIG_YAML, loads that file into model.Cfg, fills unset fields
// from struct defaults, and validates the result.
func New(_ context.Context, _ string) (*model.Cfg, error) {
	var env envVars
	if err := envconfig.Process("", &env); err != nil {
		return nil, err
	}

	cfg := &model.Cfg{}
	if err := loadFile(env.ConfigYAML, cfg); err != nil {
		return nil, err
	}
	if err := defaults.Set(cfg); err != nil {
		return nil, fmt.Errorf("apply defaults: %w", err)
	}
	if err := helpers.Check(cfg); err != nil {
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
