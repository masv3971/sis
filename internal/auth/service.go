// Package auth is the cross-cutting authentication/authorization layer: it
// verifies credentials, issues tokens, and provides gin middleware. It depends
// on the account/client ports, so storage stays swappable.
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/auth/password"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// ErrInvalidCredentials is returned for any failed authentication, without
// revealing which factor failed.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Service authenticates principals and issues tokens.
type Service struct {
	accounts port.AccountRepository
	clients  port.ClientRepository
	issuer   *token.Issuer
}

// New builds an auth Service.
func New(accounts port.AccountRepository, clients port.ClientRepository, issuer *token.Issuer) *Service {
	return &Service{accounts: accounts, clients: clients, issuer: issuer}
}

// Token is an issued access token.
type Token struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

// Authenticate verifies a username/password and issues a user token.
func (s *Service) Authenticate(ctx context.Context, username, plain string) (Token, error) {
	acc, err := s.accounts.GetByUsername(ctx, username)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return Token{}, ErrInvalidCredentials
	case err != nil:
		return Token{}, err
	case acc.Status != domain.StatusActive:
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
func (s *Service) ClientCredentials(ctx context.Context, clientID, secret string) (Token, error) {
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

func (s *Service) issue(subject string, kind token.Kind, roles, scopes []string) (Token, error) {
	signed, exp, err := s.issuer.Issue(subject, kind, roles, scopes)
	if err != nil {
		return Token{}, err
	}
	return Token{AccessToken: signed, TokenType: "Bearer", ExpiresAt: exp}, nil
}

// EnsureAccount upserts a local account with the given plaintext password; used
// for bootstrap/seeding. On update the repository preserves id and created_at.
func (s *Service) EnsureAccount(ctx context.Context, username, plain string, roles []domain.AuthRole) error {
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
func (s *Service) EnsureClient(ctx context.Context, clientID, secret, name string, scopes []string) error {
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
