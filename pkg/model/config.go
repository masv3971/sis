// Package model holds the typed configuration tree shared across the service.
// Inspired by SUNET/vc's pkg/model: YAML + default + validate struct tags,
// loaded by pkg/configuration.
package model

import "time"

// Cfg is the root configuration tree.
type Cfg struct {
	Common Common `yaml:"common"`
	SIS    SIS    `yaml:"sis" validate:"required"`
}

// Common holds settings shared by any future SIS service.
type Common struct {
	Production bool `yaml:"production"`
	Log        Log  `yaml:"log"`
}

// Log configures logging.
type Log struct {
	Level string `yaml:"level" default:"info"`
	JSON  bool   `yaml:"json" default:"true"`
}

// SIS holds the Student Information System service configuration.
type SIS struct {
	APIServer APIServer `yaml:"api_server"`
	DB        DB        `yaml:"db"`
	Auth      Auth      `yaml:"auth"`
	Bus       Bus       `yaml:"bus"`
}

// APIServer configures the HTTP server.
type APIServer struct {
	Addr            string        `yaml:"addr" default:":8080"`
	ReadTimeout     time.Duration `yaml:"read_timeout" default:"15s"`
	WriteTimeout    time.Duration `yaml:"write_timeout" default:"15s"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" default:"10s"`
}

// DB configures persistence. An empty ConnectionString selects the in-memory store.
type DB struct {
	ConnectionString string `yaml:"connection_string"`
}

// Auth configures authentication and token issuance.
type Auth struct {
	JWTSecret string        `yaml:"jwt_secret"`
	TokenTTL  time.Duration `yaml:"token_ttl" default:"1h"`
	Bootstrap Bootstrap     `yaml:"bootstrap"`
	OIDC      OIDC          `yaml:"oidc"`
}

// Bootstrap holds optional seed credentials applied on startup.
type Bootstrap struct {
	AdminUsername string   `yaml:"admin_username"`
	AdminPassword string   `yaml:"admin_password"`
	ClientID      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret"`
	ClientScopes  []string `yaml:"client_scopes"`
}

// OIDC configures the OIDC Relying Party (delegated login). Scaffold: the
// redirect/callback flow is not wired yet.
type OIDC struct {
	IssuerURL    string `yaml:"issuer_url"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
}

// Bus configures the message-bus (NATS) adapter. Scaffold for phase 5.
type Bus struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url" default:"nats://localhost:4222"`
	Subject string `yaml:"subject" default:"sis.events"`
}
