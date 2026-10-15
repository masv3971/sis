package model

import "time"

// Cfg is the root of the configuration file.
type Cfg struct {
	Common Common `yaml:"common"`
	SIS    SIS    `yaml:"sis" validate:"required"`
}

type Common struct {
	// Production requires jwt_secret and an https OIDC issuer.
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
}

type Web struct {
	// Root is the directory with the built admin UI (index.html, assets/).
	// Empty disables the UI.
	Root string `yaml:"root"`
}

type Auth struct {
	// JWTSecret signs access tokens (HS256). Must be the same on all
	// replicas. If empty outside production, a random secret is generated at
	// startup and tokens do not survive a restart.
	JWTSecret string        `yaml:"jwt_secret" validate:"omitempty,min=8"`
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
	RedirectURL  string `yaml:"redirect_url" validate:"required_with=IssuerURL,omitempty,redirect_uri"`
}

type Adapter struct {
	Inbound  Inbound  `yaml:"inbound"`
	Outbound Outbound `yaml:"outbound"`
}

// Inbound holds the API settings. Page sizes above the max are clamped to
// it; max must be >= default.
type Inbound struct {
	AuthAPI   AuthAPI   `yaml:"authapi"`
	RestAdmin RestAdmin `yaml:"restadmin"`
	OEAPI     OEAPI     `yaml:"oeapi"`
	EduAPI    EduAPI    `yaml:"eduapi"`
}

type AuthAPI struct {
	// PostLoginRedirectURL is where the browser goes after OIDC login, with
	// the token in the URL fragment.
	PostLoginRedirectURL string `yaml:"post_login_redirect_url" default:"/" validate:"required"`
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
