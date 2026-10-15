// Package auth is the cross-cutting authentication/authorization layer: it
// verifies credentials, issues tokens, and provides gin middleware. It depends
// on the account/client ports, so storage stays swappable.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/auth/oidcrp"
	"github.com/SUNET/sis/internal/auth/password"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// ErrInvalidCredentials is returned for any failed authentication, without
// revealing which factor failed.
var ErrInvalidCredentials = errors.New("invalid credentials")

// OIDCClaims is the subset of verified ID-token claims the service uses. It
// re-exports oidcrp.Claims so transport-layer handlers can type the Exchange
// result without importing the oidcrp sub-package directly.
type OIDCClaims = oidcrp.Claims

// Client authenticates principals and issues tokens. It is a "Client" rather
// than a "Service" because it holds no runtime resources (no connections, no
// goroutines) and therefore has no Close: it is a pure in-process helper over
// the injected repositories, the token issuer, and the optional OIDC RP.
//
// auth.New is the composition root for authentication: it builds the token
// issuer, the OIDC Relying Party (nil when disabled), and seeds any bootstrap
// credentials. All auth flows — local login, client-credentials, and OIDC —
// go through methods on this one Client, so consumers only ever see
// *auth.Client.
type Client struct {
	accounts port.AccountRepository
	clients  port.ClientRepository
	issuer   *token.Issuer
	oidcRP   *oidcrp.Client // nil when OIDC is disabled
	log      *logger.Log
}

// New builds the auth Client from configuration, following the SUNET/vc shape
// New(ctx, cfg, deps..., log). It derives the JWT signing secret from
// cfg.SIS.Auth (generating a random ephemeral one with a warning when unset
// outside production; refusing to start in production), builds the token issuer, wires
// the OIDC Relying Party when configured, and seeds any bootstrap
// admin/machine credentials from config so main does not have to.
func New(ctx context.Context, cfg *model.Cfg, accounts port.AccountRepository, clients port.ClientRepository, log *logger.Log) (*Client, error) {
	log = log.New("auth")
	secret, err := jwtSecret(cfg, log)
	if err != nil {
		return nil, err
	}
	client := &Client{
		accounts: accounts,
		clients:  clients,
		issuer:   token.NewIssuer(secret, "sis", cfg.SIS.Auth.TokenTTL),
		log:      log,
	}

	client.oidcRP, err = oidcrp.New(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	client.seedBootstrap(ctx, cfg.SIS.Auth.Bootstrap)
	return client, nil
}

// OIDCEnabled reports whether delegated login is configured.
func (s *Client) OIDCEnabled() bool { return s.oidcRP != nil }

// OIDCAuthCodeURL builds the provider authorization URL for the given state
// and nonce. It is only valid to call when OIDCEnabled returns true.
func (s *Client) OIDCAuthCodeURL(state, nonce string) string {
	return s.oidcRP.AuthCodeURL(state, nonce)
}

// OIDCExchange completes the OIDC code flow: it trades the code for tokens,
// verifies the ID token and nonce, and returns the identity claims. It is
// only valid to call when OIDCEnabled returns true.
func (s *Client) OIDCExchange(ctx context.Context, code, nonce string) (*OIDCClaims, error) {
	return s.oidcRP.Exchange(ctx, code, nonce)
}

// Issuer returns the configured token issuer, so transport layers can wire its
// Parse into their auth middleware without taking a second dependency.
func (s *Client) Issuer() *token.Issuer {
	return s.issuer
}

// errEphemeralSecretInProduction is returned when production config omits
// jwt_secret. Configuration validation already rejects this; auth.New checks
// again so the guarantee holds for any caller.
var errEphemeralSecretInProduction = errors.New("auth: sis.auth.jwt_secret is required when common.production is true")

// jwtSecret returns the configured signing secret. Outside production an unset
// secret is replaced by a random ephemeral one (dev only — tokens do not
// survive a restart and differ per replica) with a warning; in production it
// is an error.
func jwtSecret(cfg *model.Cfg, log *logger.Log) (string, error) {
	if cfg.SIS.Auth.JWTSecret != "" {
		return cfg.SIS.Auth.JWTSecret, nil
	}
	if cfg.Common.Production {
		return "", errEphemeralSecretInProduction
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	log.Warn("sis.auth.jwt_secret unset; using an ephemeral secret (tokens will not survive restart)")
	return hex.EncodeToString(b), nil
}

// seedBootstrap upserts the optional admin account and machine client from
// config. Any error is logged but non-fatal: a transient repo error at startup
// must not stop the service from coming up.
func (s *Client) seedBootstrap(ctx context.Context, bs model.Bootstrap) {
	if bs.AdminUsername != "" && bs.AdminPassword != "" {
		if err := s.EnsureAccount(ctx, bs.AdminUsername, bs.AdminPassword,
			[]domain.AuthRole{domain.AuthRoleAdmin}); err != nil {
			s.log.Error("seed admin account failed", "err", err)
		} else {
			s.log.Info("seeded bootstrap admin account", "username", bs.AdminUsername)
		}
	}
	if bs.ClientID != "" && bs.ClientSecret != "" {
		scopes := bs.ClientScopes
		if len(scopes) == 0 {
			scopes = []string{domain.ScopeRead, domain.ScopeWrite}
		}
		if err := s.EnsureClient(ctx, bs.ClientID, bs.ClientSecret,
			"bootstrap", scopes); err != nil {
			s.log.Error("seed client failed", "err", err)
		} else {
			s.log.Info("seeded bootstrap client", "client_id", bs.ClientID, "scopes", scopes)
		}
	}
}

// Token is an issued access token.
type Token struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

// Authenticate verifies a username/password and issues a user token.
func (s *Client) Authenticate(ctx context.Context, username, plain string) (Token, error) {
	acc, err := s.accounts.GetByUsername(ctx, username)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return Token{}, ErrInvalidCredentials
	case err != nil:
		return Token{}, err
	case acc.Status != domain.StatusActive:
		return Token{}, ErrInvalidCredentials
	case acc.OIDCIssuer != "" || acc.PasswordHash == "":
		// OIDC-provisioned accounts have no local password and must sign
		// in through their provider. Rejecting before Verify also covers
		// legacy OIDC rows that still carry a random hash.
		return Token{}, ErrInvalidCredentials
	}
	ok, err := password.Verify(plain, acc.PasswordHash)
	if err != nil || !ok {
		return Token{}, ErrInvalidCredentials
	}
	roles := make([]string, len(acc.Roles))
	for i, r := range acc.Roles {
		roles[i] = string(r)
	}
	return s.issue(acc.ID, token.KindUser, roles, nil)
}

// ClientCredentials verifies a client_id/secret and issues a client token.
func (s *Client) ClientCredentials(ctx context.Context, clientID, secret string) (Token, error) {
	c, err := s.clients.GetByClientID(ctx, clientID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return Token{}, ErrInvalidCredentials
	case err != nil:
		return Token{}, err
	}
	ok, err := password.Verify(secret, c.SecretHash)
	if err != nil || !ok {
		return Token{}, ErrInvalidCredentials
	}
	return s.issue(c.ClientID, token.KindClient, nil, c.Scopes)
}

func (s *Client) issue(subject string, kind token.Kind, roles, scopes []string) (Token, error) {
	signed, exp, err := s.issuer.Issue(subject, kind, roles, scopes)
	if err != nil {
		return Token{}, err
	}
	return Token{AccessToken: signed, TokenType: "Bearer", ExpiresAt: exp}, nil
}

// EnsureAccount upserts a local account with the given plaintext password; used
// for bootstrap/seeding. On update the repository preserves id and created_at.
func (s *Client) EnsureAccount(ctx context.Context, username, plain string, roles []domain.AuthRole) error {
	hash, err := password.Hash(plain)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.accounts.Upsert(ctx, domain.Account{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		Roles:        roles,
		Status:       domain.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
}

// EnsureClient upserts an OAuth2 machine client with the given plaintext secret.
func (s *Client) EnsureClient(ctx context.Context, clientID, secret, name string, scopes []string) error {
	hash, err := password.Hash(secret)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.clients.Upsert(ctx, domain.Client{
		ClientID:   clientID,
		SecretHash: hash,
		Name:       name,
		Scopes:     scopes,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
}
