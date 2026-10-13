// Package config loads runtime configuration from the environment.
package config

import (
	"os"
	"time"
)

// Config holds runtime configuration.
type Config struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

// Load reads configuration from environment variables, applying defaults.
func Load() Config {
	return Config{
		Addr:            env("SIS_ADDR", ":8080"),
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    15 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
