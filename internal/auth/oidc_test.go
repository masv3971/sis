package auth

import (
	"context"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
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

func TestIssueForOIDC_RejectsMissingIdentity(t *testing.T) {
	svc, _ := newAuthService()
	if _, err := svc.IssueForOIDC(context.Background(), &OIDCClaims{Issuer: testIssuer}); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}
