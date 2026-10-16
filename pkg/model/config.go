package model

import "time"

// Cfg is the root of the configuration file.
type Cfg struct {
	Common Common `yaml:"common"`
	SIS    SIS    `yaml:"sis" validate:"required"`
}

type Common struct {
	// Production requires a jwt_secret of at least
	// MinProductionJWTSecretLen bytes and https OIDC issuer and redirect URLs.
	Production bool `yaml:"production"`
	Log        Log  `yaml:"log"`
}

type Log struct {
	Level string `yaml:"level" default:"info" validate:"oneof=debug info warn error"`
	// JSON selects JSON lines; false gives key=value text.
	JSON bool `yaml:"json" default:"true"`
}

type SIS struct {
	HTTPServer HTTPServer `yaml:"httpserver"`
	Auth       Auth       `yaml:"auth"`
	Adapter    Adapter    `yaml:"adapter"`
}

type HTTPServer struct {
	Addr         string        `yaml:"addr" default:":8080" validate:"required"`
	ReadTimeout  time.Duration `yaml:"read_timeout" default:"15s" validate:"gt=0"`
	WriteTimeout time.Duration `yaml:"write_timeout" default:"15s" validate:"gt=0"`
	// ShutdownTimeout is the grace period for in-flight requests on SIGTERM.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" default:"10s" validate:"gt=0"`
	Web             Web           `yaml:"web"`
	Swagger         Swagger       `yaml:"swagger"`
}

type Web struct {
	// Root is the directory with the built admin UI (index.html, assets/).
	// Empty disables the UI.
	Root string `yaml:"root"`
}

type Swagger struct {
	// Enabled serves the /api/v1 OpenAPI document and Swagger UI at
	// /swagger/index.html. The page itself is public; calls made from it
	// still need a Bearer token.
	Enabled bool `yaml:"enabled" default:"true"`
}

// MinProductionJWTSecretLen is the minimum jwt_secret length in bytes when
// common.production is true: 256 bits, matching the HS256 output size.
// Config validation enforces it through the jwt_secret_production_min tag on JWTSecret
// (a tag cannot reference a constant, so a test keeps the two in sync);
// auth.New uses the constant for its own defence-in-depth check.
const MinProductionJWTSecretLen = 32

type Auth struct {
	// JWTSecret signs access tokens (HS256). Must be the same on all
	// replicas. If empty outside production, a random secret is generated at
	// startup and tokens do not survive a restart.
	//
	// In production it must be at least MinProductionJWTSecretLen bytes and
	// randomly generated (e.g. `openssl rand -hex 32`). Anyone who recovers
	// it can forge admin tokens, and a short or human-chosen secret can be
	// brute-forced offline from a single captured token. Length is checked;
	// randomness cannot be, so never use a passphrase.
	JWTSecret string        `yaml:"jwt_secret" validate:"omitempty,min=8,jwt_secret_production_min=32"`
	TokenTTL  time.Duration `yaml:"token_ttl" default:"1h" validate:"gt=0"`
	Bootstrap Bootstrap     `yaml:"bootstrap"`
	OIDCRP    OIDCRP        `yaml:"oidcrp"`
}

// Bootstrap is an admin account and an OAuth2 client upserted at every
// startup. An empty username or client_id skips that entry.
type Bootstrap struct {
	AdminUsername string `yaml:"admin_username"`
	AdminPassword string `yaml:"admin_password" validate:"required_with=AdminUsername"`
	ClientID      string `yaml:"client_id"`
	ClientSecret  string `yaml:"client_secret" validate:"required_with=ClientID"`
	// ClientScopes defaults to sis.read and sis.write when empty.
	ClientScopes []string `yaml:"client_scopes"`
}

// OIDCRP configures OIDC login. An empty IssuerURL disables it.
type OIDCRP struct {
	// IssuerURL is fetched for discovery at startup. Must be https in
	// production.
	IssuerURL    string `yaml:"issuer_url" validate:"omitempty,httpurl"`
	ClientID     string `yaml:"client_id" validate:"required_with=IssuerURL"`
	ClientSecret string `yaml:"client_secret" validate:"required_with=IssuerURL"`
	// RedirectURL is this service's /auth/oidc/callback as the browser sees
	// it. Must be https in production (the state/nonce cookies are Secure).
	RedirectURL string `yaml:"redirect_url" validate:"required_with=IssuerURL,omitempty,redirect_uri"`
}

type Adapter struct {
	Inbound  Inbound  `yaml:"inbound"`
	Outbound Outbound `yaml:"outbound"`
	Sweeper  Sweeper  `yaml:"sweeper"`
}

// Sweeper deletes records whose ttl (POST /api/v1/populate) has passed.
// Expired records are hidden from every API at once; the sweeper only
// reclaims their storage.
type Sweeper struct {
	// Interval between sweeps.
	Interval time.Duration `yaml:"interval" default:"1m" validate:"gt=0"`
}

// Inbound holds the API settings. Page sizes above the max are clamped to
// it; max must be >= default.
type Inbound struct {
	AuthAPI    AuthAPI    `yaml:"authapi"`
	RestAdmin  RestAdmin  `yaml:"restadmin"`
	OEAPI      OEAPI      `yaml:"oeapi"`
	EduAPI     EduAPI     `yaml:"eduapi"`
	ConvertAPI ConvertAPI `yaml:"convertapi"`
}

// ConvertAPI configures POST /api/v1/parse and /api/v1/populate, which take
// whole documents (e.g. an ELM credential) rather than single records.
type ConvertAPI struct {
	// MaxBodyBytes caps the request body of these two endpoints; every
	// other endpoint keeps the 1 MiB cap. Larger requests get 413.
	MaxBodyBytes int64 `yaml:"max_body_bytes" default:"5242880" validate:"gt=0"`
}

type AuthAPI struct {
	// PostLoginRedirectURL is where the browser goes after OIDC login. The
	// token itself travels in an HttpOnly cookie that the web UI collects
	// from /auth/oidc/session; only a "#oidc_login" marker is appended here.
	// It must not have a fragment of its own (e.g. "/#people"): the web UI
	// only acts on a fragment that is exactly #oidc_login. The web UI must be
	// served from this service's origin for the cookie to reach it.
	PostLoginRedirectURL string `yaml:"post_login_redirect_url" default:"/" validate:"required,post_login_redirect_url"`
	// MaxConcurrentLogins caps password checks running at once across
	// /auth/login and /oauth/token. Each check uses about 64 MiB; requests
	// over the cap get 429.
	MaxConcurrentLogins int `yaml:"max_concurrent_logins" default:"8" validate:"gt=0"`
}

type RestAdmin struct {
	DefaultLimit int `yaml:"default_limit" default:"50" validate:"gt=0"`
	MaxLimit     int `yaml:"max_limit" default:"500" validate:"gtefield=DefaultLimit"`
}

type OEAPI struct {
	DefaultPageSize int `yaml:"default_page_size" default:"10" validate:"gt=0"`
	MaxPageSize     int `yaml:"max_page_size" default:"500" validate:"gtefield=DefaultPageSize"`
}

type EduAPI struct {
	DefaultLimit int `yaml:"default_limit" default:"100" validate:"gt=0"`
	MaxLimit     int `yaml:"max_limit" default:"500" validate:"gtefield=DefaultLimit"`
}

type Outbound struct {
	Postgres Postgres `yaml:"postgres"`
	Events   Events   `yaml:"events"`
}

type Postgres struct {
	// ConnectionString is a PostgreSQL URL. Empty uses an in-memory store
	// that is lost on restart.
	ConnectionString string `yaml:"connection_string"`
}

// Events are currently only logged; the NATS settings are not used yet.
type Events struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url" default:"nats://localhost:4222" validate:"required_if=Enabled true,omitempty,uri"`
	Subject string `yaml:"subject" default:"sis.events" validate:"required_if=Enabled true"`
}
