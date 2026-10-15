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
	Level string `yaml:"level" default:"info" validate:"oneof=debug info warn error"`
	JSON  bool   `yaml:"json" default:"true"`
}

// SIS holds the Student Information System service configuration.
type SIS struct {
	APIServer APIServer `yaml:"api_server"`
	DB        DB        `yaml:"db"`
	Auth      Auth      `yaml:"auth"`
	Bus       Bus       `yaml:"bus"`
	Web       Web       `yaml:"web"`
}

// Web configures static-file serving of the built React SPA. When Root is
// empty sis is API-only (useful in dev when the SPA runs under `vite dev` on
// a separate port). When set the httpserver mounts the directory at /assets
// and serves index.html for any non-API GET so client-side routing works.
type Web struct {
	Root string `yaml:"root"`
}

// APIServer configures the HTTP server.
type APIServer struct {
	Addr            string        `yaml:"addr" default:":8080" validate:"required"`
	ReadTimeout     time.Duration `yaml:"read_timeout" default:"15s" validate:"gt=0"`
	WriteTimeout    time.Duration `yaml:"write_timeout" default:"15s" validate:"gt=0"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" default:"10s" validate:"gt=0"`
	// MaxLimit is the largest page size any list endpoint returns (limit on
	// /api/v1 and Edu-API, pageSize on OEAPI). Larger requests are clamped.
	MaxLimit int `yaml:"max_limit" default:"500" validate:"gt=0"`
}

// DB configures persistence. An empty ConnectionString selects the in-memory store.
type DB struct {
	ConnectionString string `yaml:"connection_string"`
}

// Auth configures authentication and token issuance.
type Auth struct {
	JWTSecret string        `yaml:"jwt_secret" validate:"omitempty,min=8"`
	TokenTTL  time.Duration `yaml:"token_ttl" default:"1h" validate:"gt=0"`
	Bootstrap Bootstrap     `yaml:"bootstrap"`
	OIDC      OIDC          `yaml:"oidc"`
}

// Bootstrap holds optional seed credentials applied on startup. These values
// seed a local admin account and a machine client at service boot — they are
// dev conveniences, not production credentials, so the only requirement is
// non-empty when the matching username/client_id is set. Length and strength
// are an operator concern, not a config-schema one.
type Bootstrap struct {
	AdminUsername string   `yaml:"admin_username"`
	AdminPassword string   `yaml:"admin_password" validate:"required_with=AdminUsername"`
	ClientID      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret" validate:"required_with=ClientID"`
	ClientScopes  []string `yaml:"client_scopes"`
}

// OIDC configures the OIDC Relying Party (delegated login).
type OIDC struct {
	IssuerURL            string `yaml:"issuer_url" validate:"omitempty,httpurl"`
	ClientID             string `yaml:"client_id" validate:"required_with=IssuerURL"`
	ClientSecret         string `yaml:"client_secret" validate:"required_with=IssuerURL"`
	RedirectURL          string `yaml:"redirect_url" validate:"required_with=IssuerURL,omitempty,redirect_uri"`
	PostLoginRedirectURL string `yaml:"post_login_redirect_url" default:"/" validate:"required"`
}

// Bus configures the message-bus (NATS) adapter. Scaffold for phase 5.
type Bus struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url" default:"nats://localhost:4222" validate:"required_if=Enabled true,omitempty,uri"`
	Subject string `yaml:"subject" default:"sis.events" validate:"required_if=Enabled true"`
}
