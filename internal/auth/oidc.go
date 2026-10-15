package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/auth/password"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
)

// oidcUsernamePrefix marks usernames of OIDC-provisioned accounts. Local
// usernames are chosen by an operator and never start with it, so an OIDC
// account can never shadow or be matched to a local one.
const oidcUsernamePrefix = "oidc:"

// IssueForOIDC maps a verified OIDC identity to an account and issues a user
// token. The account is looked up only by the (issuer, subject) pair from the
// ID token; email is never used for matching, so an identity that claims an
// existing user's email cannot inherit that user's roles. On first login a
// read-only account is provisioned; an admin can grant more roles later.
func (s *Client) IssueForOIDC(ctx context.Context, claims *OIDCClaims) (Token, error) {
	if claims == nil || claims.Issuer == "" || claims.Subject == "" {
		return Token{}, ErrInvalidCredentials
	}

	acc, err := s.accounts.GetByOIDC(ctx, claims.Issuer, claims.Subject)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		acc, err = s.provisionOIDCAccount(ctx, claims.Issuer, claims.Subject)
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

// oidcUsername derives a stable, unique username for an OIDC identity. The
// issuer is hashed so the username stays short and free of URL characters.
func oidcUsername(issuer, subject string) string {
	sum := sha256.Sum256([]byte(issuer))
	return oidcUsernamePrefix + hex.EncodeToString(sum[:6]) + ":" + subject
}

// provisionOIDCAccount creates a read-only account bound to issuer+subject,
// with an unusable random password (local login can never match it).
func (s *Client) provisionOIDCAccount(ctx context.Context, issuer, subject string) (domain.Account, error) {
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
		Username:     oidcUsername(issuer, subject),
		PasswordHash: hash,
		Roles:        []domain.AuthRole{domain.AuthRoleReadOnly},
		Status:       domain.StatusActive,
		OIDCIssuer:   issuer,
		OIDCSubject:  subject,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.accounts.Upsert(ctx, acc); err != nil {
		return domain.Account{}, err
	}
	// Re-read so concurrent first logins converge on the stored row.
	return s.accounts.GetByOIDC(ctx, issuer, subject)
}
