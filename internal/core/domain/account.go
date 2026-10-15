package domain

import "time"

// AuthRole is a principal's authorization role (distinct from a person's Role).
type AuthRole string

const (
	AuthRoleAdmin     AuthRole = "admin"
	AuthRoleRegistrar AuthRole = "registrar"
	AuthRoleReadOnly  AuthRole = "readonly"
)

// OAuth2 scopes for machine clients on the native API.
const (
	ScopeRead  = "sis.read"
	ScopeWrite = "sis.write"
)

// Account is a human login. Local accounts sign in with Username and
// PasswordHash. OIDC accounts are bound to the provider identity
// (OIDCIssuer, OIDCSubject) and are only ever found by that pair, never by
// email, so a federated user cannot take over a local account.
type Account struct {
	ID           string
	Username     string
	PasswordHash string
	Roles        []AuthRole
	Status       Status
	OIDCIssuer   string
	OIDCSubject  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Client is an OAuth2 client-credentials machine principal (e.g. a business system).
type Client struct {
	ClientID   string
	SecretHash string
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
