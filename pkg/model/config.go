package model

import "time"

// Cfg is the root of the configuration file.
type Cfg struct {
	Common Common `yaml:"common"`
	SIS    SIS    `yaml:"sis" validate:"required"`
}

type Common struct {
	// Production requires every trusted token issuer (and jwks_url) to use
	// https and to have an audience.
	Production bool `yaml:"production"`
	Log        Log  `yaml:"log"`
}

type Log struct {
	Level string `yaml:"level" default:"info" validate:"oneof=debug info warn error"`
	// JSON selects JSON lines; false gives key=value text.
	JSON bool `yaml:"json" default:"true"`
}

type SIS struct {
	// StartupTimeout bounds startup: connecting to and migrating the
	// database, the file import and binding the listener. SIGINT/SIGTERM
	// during startup stops it at once.
	StartupTimeout time.Duration `yaml:"startup_timeout" default:"2m" validate:"gt=0"`
	HTTPServer     HTTPServer    `yaml:"httpserver"`
	Auth           Auth          `yaml:"auth"`
	Adapter        Adapter       `yaml:"adapter"`
	FileImport     FileImport    `yaml:"file_import"`
}

// FileImport loads records from files at every startup, e.g. fake students
// for development.
type FileImport struct {
	// Files are imported in order. A relative path is relative to the
	// directory of the config file. See internal/fileimport for the format.
	// Every record is upserted by sourcedId, so on each start the file wins
	// over changes made through the API. Any error stops startup.
	Files []string `yaml:"files" validate:"dive,required"`
}

type HTTPServer struct {
	Addr        string        `yaml:"addr" default:":8080" validate:"required"`
	ReadTimeout time.Duration `yaml:"read_timeout" default:"15s" validate:"gt=0"`
	// WriteTimeout bounds the request's work, reading its body included:
	// when it passes, the handler's context is cancelled, so database
	// calls stop and the request ends with 503. Sending the response may take 5s more, so
	// that 503 reaches the client.
	WriteTimeout time.Duration `yaml:"write_timeout" default:"15s" validate:"gt=0"`
	// ShutdownTimeout is the grace period for in-flight requests on SIGTERM.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" default:"10s" validate:"gt=0"`
	Web             Web           `yaml:"web"`
	Swagger         Swagger       `yaml:"swagger"`
	TLS             TLS           `yaml:"tls"`
}

// TLS makes the listener serve https from a certificate and key file. Both
// empty serves plain http (e.g. behind a TLS-terminating proxy). The web
// UI's sign-in needs https, or http on localhost: browsers only offer the
// crypto it uses on secure pages.
type TLS struct {
	// CertFile is the PEM certificate, optionally followed by its chain.
	CertFile string `yaml:"cert_file" validate:"required_with=KeyFile"`
	// KeyFile is the PEM private key.
	KeyFile string `yaml:"key_file" validate:"required_with=CertFile"`
}

// Enabled reports whether TLS is configured.
func (t TLS) Enabled() bool { return t.CertFile != "" }

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

// Auth configures how SIS checks access tokens. SIS issues no tokens and
// stores no credentials: callers get a token from a trusted issuer, and the
// token's scope claim says what they may do (sis.read, sis.write). Access is
// granted and revoked at the issuer, without restarting SIS.
type Auth struct {
	// Issuers are the token issuers SIS trusts. A token from any other
	// issuer is rejected; with none, every API request is rejected.
	Issuers []Issuer `yaml:"issuers" validate:"unique=Issuer,dive"`
	// CAFiles are extra PEM CA certificates, added to the system roots, for
	// verifying the issuers' https certificates, e.g. a development CA.
	CAFiles []string `yaml:"ca_files" validate:"dive,required"`
}

// Issuer is one trusted token issuer, e.g. an OIDC provider or a Keycloak
// realm.
type Issuer struct {
	// Issuer must equal the iss claim of its tokens exactly, e.g.
	// https://idp.example.org/realms/sis. Must be https in production.
	Issuer string `yaml:"issuer" validate:"required,httpurl"`
	// Audience, when set, must be in the aud claim of every token. Set it so
	// SIS only accepts tokens meant for SIS. Required in production.
	Audience string `yaml:"audience"`
	// JWKSURL is where the issuer's signing keys are fetched. Empty finds it
	// through the issuer's discovery document. Set it when SIS reaches the
	// issuer under another address than the one in iss (e.g. a container
	// name). Must be https in production.
	JWKSURL string `yaml:"jwks_url" validate:"omitempty,httpurl"`
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
	// Timeout bounds one sweep; what is left is deleted by the next one.
	Timeout time.Duration `yaml:"timeout" default:"30s" validate:"gt=0,ltefield=Interval"`
}

// Inbound holds the API settings. Page sizes above the max are clamped to
// it; max must be >= default.
type Inbound struct {
	AuthAPI    AuthAPI    `yaml:"authapi"`
	RestAdmin  RestAdmin  `yaml:"restadmin"`
	OEAPI      OEAPI      `yaml:"oeapi"`
	EduAPI     EduAPI     `yaml:"eduapi"`
	ConvertAPI ConvertAPI `yaml:"convertapi"`
	AMQPAPI    AMQPAPI    `yaml:"amqpapi"`
}

// AMQPAPI consumes write commands from an AMQP 1.0 broker (e.g. RabbitMQ 4,
// Azure Service Bus, ActiveMQ Artemis), as an alternative to the /api/v1
// writes. SIS declares no queues: the broker admin creates the address, and
// should give it a delivery limit and a dead-letter target. A message's
// subject names the command (person.create, course.update, populate, ...).
type AMQPAPI struct {
	Enabled bool `yaml:"enabled"`
	// URL of the broker, amqp://user:password@host:5672 or amqps://... (TLS).
	// Without user and password SIS logs in anonymously. Must be amqps in
	// production.
	URL string `yaml:"url" validate:"required_if=Enabled true"`
	// CAFiles are extra PEM CA certificates, added to the system roots, for
	// verifying the broker's certificate (amqps only).
	CAFiles []string `yaml:"ca_files" validate:"dive,required"`
	// Address is the queue to consume, e.g. /queues/sis.commands (RabbitMQ
	// 4) or sis.commands (Service Bus, Artemis).
	Address string `yaml:"address" validate:"required_if=Enabled true"`
	// Prefetch is how many commands run at once.
	Prefetch int `yaml:"prefetch" default:"10" validate:"gt=0,lte=1000"`
	// MaxMessageBytes caps a command's body; larger ones are rejected. The
	// whole encoded message may be 64 KiB larger (properties, token); a
	// message above that closes the link, and the broker hands it back
	// until its delivery limit dead-letters it. Give the broker a matching
	// publish limit (RabbitMQ: max_message_size) so it refuses such
	// messages up front.
	MaxMessageBytes int64 `yaml:"max_message_bytes" default:"5242880" validate:"gt=0"`
	// CommandTimeout bounds one command. A command that runs past it is
	// cancelled and its message redelivered, like any server-side failure.
	CommandTimeout time.Duration `yaml:"command_timeout" default:"2m" validate:"gt=0"`
	// Scopes are granted to messages without an authorization property
	// ("Bearer <token>", checked like an API token). Empty requires a token
	// on every message; [sis.write] trusts everyone who may publish to the
	// address, so limit that at the broker.
	Scopes []string `yaml:"scopes" validate:"dive,oneof=sis.write"`
}

// ConvertAPI configures POST /api/v1/parse and /api/v1/populate, which take
// whole documents (e.g. an ELM credential) rather than single records.
type ConvertAPI struct {
	// MaxBodyBytes caps the request body of these two endpoints; every
	// other endpoint keeps the 1 MiB cap. Larger requests get 413.
	MaxBodyBytes int64 `yaml:"max_body_bytes" default:"5242880" validate:"gt=0"`
	// WriteTimeout replaces sis.httpserver.write_timeout for these two
	// endpoints: populate stores a large document record by record and can
	// take longer than other requests.
	WriteTimeout time.Duration `yaml:"write_timeout" default:"2m" validate:"gt=0"`
}

// AuthAPI configures sign-in for the web UI, served at GET /auth/config.
// The browser signs in at Issuer (authorization code flow with PKCE) and
// sends the access token it gets to SIS; SIS never sees a password. Empty
// Issuer disables sign-in in the web UI.
type AuthAPI struct {
	// Issuer must be one of sis.auth.issuers, so SIS accepts its tokens. It
	// must allow CORS from the web UI's origin on its discovery and token
	// endpoints.
	Issuer string `yaml:"issuer" validate:"omitempty,httpurl"`
	// ClientID is the web UI's public client at the issuer. Its redirect
	// URI is the web UI's root, e.g. https://sis.example.org/.
	ClientID string `yaml:"client_id" validate:"required_with=Issuer"`
	// Scope is requested at sign-in. The issuer decides what is granted.
	Scope string `yaml:"scope" default:"openid sis.read sis.write"`
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
	// ConnectTimeout bounds opening one connection. 0 keeps the
	// connection string's connect_timeout (default: none).
	ConnectTimeout time.Duration `yaml:"connect_timeout" default:"10s" validate:"gte=0"`
	// StatementTimeout makes the server abort a statement that runs
	// longer (PostgreSQL statement_timeout). 0 keeps the server's setting.
	// Migrations at startup are not limited by it.
	StatementTimeout time.Duration `yaml:"statement_timeout" default:"30s" validate:"gte=0"`
}

// Events are currently only logged; the NATS settings are not used yet.
type Events struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url" default:"nats://localhost:4222" validate:"required_if=Enabled true,omitempty,uri"`
	Subject string `yaml:"subject" default:"sis.events" validate:"required_if=Enabled true"`
}
