package auth

import (
	"context"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth/password"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
)

// newAuthService builds a minimal auth.Client wired to in-memory repositories
// and a trivial token issuer, without going through New.
func newAuthService() (*Client, *memory.AccountRepository) {
	accounts := memory.NewAccountRepository()
	return &Client{
		accounts: accounts,
		clients:  memory.NewClientRepository(),
		issuer:   token.NewIssuer("secret", "sis", time.Hour),
	}, accounts
}

const testIssuer = "https://idp.example.org"

func TestIssueForOIDC_ProvisionsReadOnlyOnFirstLogin(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()

	if _, err := svc.IssueForOIDC(ctx, &OIDCClaims{Issuer: testIssuer, Subject: "sub-1", Email: "ada@uni.edu"}); err != nil {
		t.Fatalf("first OIDC login: %v", err)
	}
	acc, err := accounts.GetByOIDC(ctx, testIssuer, "sub-1")
	if err != nil {
		t.Fatalf("expected provisioned account: %v", err)
	}
	if len(acc.Roles) != 1 || acc.Roles[0] != domain.AuthRoleReadOnly {
		t.Fatalf("expected read-only role, got %v", acc.Roles)
	}
}

func TestIssueForOIDC_SameIdentityReusesAccount(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()
	claims := &OIDCClaims{Issuer: testIssuer, Subject: "sub-1"}

	if _, err := svc.IssueForOIDC(ctx, claims); err != nil {
		t.Fatal(err)
	}
	first, _ := accounts.GetByOIDC(ctx, testIssuer, "sub-1")
	if _, err := svc.IssueForOIDC(ctx, claims); err != nil {
		t.Fatal(err)
	}
	second, _ := accounts.GetByOIDC(ctx, testIssuer, "sub-1")
	if first.ID != second.ID {
		t.Fatalf("second login created a new account: %s != %s", first.ID, second.ID)
	}
}

// TestIssueForOIDC_EmailDoesNotLinkToLocalAccount is the regression test for
// the review finding: an OIDC identity claiming an admin's email must not
// receive the admin's roles.
func TestIssueForOIDC_EmailDoesNotLinkToLocalAccount(t *testing.T) {
	svc, _ := newAuthService()
	ctx := context.Background()
	if err := svc.EnsureAccount(ctx, "boss@uni.edu", "pw", []domain.AuthRole{domain.AuthRoleAdmin}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	tok, err := svc.IssueForOIDC(ctx, &OIDCClaims{Issuer: testIssuer, Subject: "attacker", Email: "boss@uni.edu"})
	if err != nil {
		t.Fatalf("OIDC login: %v", err)
	}
	claims, err := svc.issuer.Parse(tok.AccessToken)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != string(domain.AuthRoleReadOnly) {
		t.Fatalf("OIDC identity inherited roles via email: %v", claims.Roles)
	}
}

func TestIssueForOIDC_SameSubjectDifferentIssuerIsDistinct(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()
	for _, iss := range []string{testIssuer, "https://other.example.org"} {
		if _, err := svc.IssueForOIDC(ctx, &OIDCClaims{Issuer: iss, Subject: "same"}); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := accounts.GetByOIDC(ctx, testIssuer, "same")
	b, _ := accounts.GetByOIDC(ctx, "https://other.example.org", "same")
	if a.ID == "" || a.ID == b.ID {
		t.Fatalf("expected two distinct accounts, got %q and %q", a.ID, b.ID)
	}
}

// TestIssueForOIDC_ProvisionsWithoutPassword checks that provisioning stores
// no Argon2 hash and that the account cannot be used for local login.
func TestIssueForOIDC_ProvisionsWithoutPassword(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()
	if _, err := svc.IssueForOIDC(ctx, &OIDCClaims{Issuer: testIssuer, Subject: "sub-1"}); err != nil {
		t.Fatal(err)
	}
	acc, err := accounts.GetByOIDC(ctx, testIssuer, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if acc.PasswordHash != "" {
		t.Fatalf("expected no password hash, got %q", acc.PasswordHash)
	}
	if _, err := svc.Authenticate(ctx, acc.Username, ""); err != ErrInvalidCredentials {
		t.Fatalf("expected local login to be rejected, got %v", err)
	}
}

// TestAuthenticate_RejectsOIDCAccountWithLegacyHash covers OIDC rows created
// before provisioning stopped hashing: they still carry a valid hash, but
// local login must be refused because the account is OIDC-bound.
func TestAuthenticate_RejectsOIDCAccountWithLegacyHash(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()
	hash, err := password.Hash("known")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := accounts.Upsert(ctx, domain.Account{
		ID: "legacy", Username: "oidc:legacy", PasswordHash: hash,
		Roles: []domain.AuthRole{domain.AuthRoleReadOnly}, Status: domain.StatusActive,
		OIDCIssuer: testIssuer, OIDCSubject: "legacy", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, "oidc:legacy", "known"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestIssueForOIDC_RejectsMissingIdentity(t *testing.T) {
	svc, _ := newAuthService()
	if _, err := svc.IssueForOIDC(context.Background(), &OIDCClaims{Issuer: testIssuer}); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}
