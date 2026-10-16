package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

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
	if errors.Is(err, domain.ErrNotFound) {
		acc, err = s.provisionOIDCAccount(ctx, claims.Issuer, claims.Subject)
	}
	if err != nil {
		return Token{}, err
	}
	// Checked on every path, including provisioning: a concurrent callback
	// may have created the account first and an admin may already have
	// disabled it.
	if acc.Status != domain.StatusActive {
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

// provisionOIDCAccount creates a read-only account bound to issuer+subject.
// It stores no password hash: OIDC accounts never sign in locally
// (Authenticate rejects them outright), so there is nothing to verify and no
// reason to pay for an Argon2 hash on every first login. Keeping provisioning
// free of Argon2 also keeps OIDC callbacks outside the memory budget that
// max_concurrent_logins protects.
//
// Provisioning is insert-if-absent: a delayed first-login callback that lost
// the race to another one gets domain.ErrConflict and re-reads the stored
// account, so it never resets roles or status set in the meantime.
func (s *Client) provisionOIDCAccount(ctx context.Context, issuer, subject string) (domain.Account, error) {
	now := time.Now().UTC()
	acc := domain.Account{
		ID:          uuid.NewString(),
		Username:    oidcUsername(issuer, subject),
		Roles:       []domain.AuthRole{domain.AuthRoleReadOnly},
		Status:      domain.StatusActive,
		OIDCIssuer:  issuer,
		OIDCSubject: subject,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err := s.accounts.Create(ctx, acc)
	if err == nil {
		return acc, nil
	}
	if !errors.Is(err, domain.ErrConflict) {
		return domain.Account{}, err
	}
	// Another callback provisioned this identity first; use its stored row.
	return s.accounts.GetByOIDC(ctx, issuer, subject)
}
