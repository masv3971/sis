package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/auth/password"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
)

// IssueForOIDC maps a verified OIDC identity to a local account and issues a
// user token. On first login the account is auto-provisioned read-only; an
// admin can elevate it later. The username is the email, falling back to the
// subject when no email is present.
func (s *Client) IssueForOIDC(ctx context.Context, subject, email, _ string) (Token, error) {
	username := email
	if username == "" {
		username = subject
	}

	acc, err := s.accounts.GetByUsername(ctx, username)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		acc, err = s.provisionOIDCAccount(ctx, username)
		if err != nil {
			return Token{}, err
		}
	case err != nil:
		return Token{}, err
	case acc.Status != domain.StatusActive:
		return Token{}, ErrInvalidCredentials
	}

	roles := make([]string, len(acc.Roles))
	for i, r := range acc.Roles {
		roles[i] = string(r)
	}
	return s.issue(acc.ID, token.KindUser, roles, nil)
}

// provisionOIDCAccount creates a read-only account with an unusable random
// password (local login can never match it; the account is OIDC-only).
func (s *Client) provisionOIDCAccount(ctx context.Context, username string) (domain.Account, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return domain.Account{}, err
	}
	hash, err := password.Hash(hex.EncodeToString(b))
	if err != nil {
		return domain.Account{}, err
	}
	now := time.Now().UTC()
	acc := domain.Account{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		Roles:        []domain.AuthRole{domain.AuthRoleReadOnly},
		Status:       domain.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.accounts.Upsert(ctx, acc); err != nil {
		return domain.Account{}, err
	}
	return acc, nil
}
