package auth

import (
	"context"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth/token"
	"github.com/SUNET/sis/internal/core/domain"
)

// newAuthService builds a minimal auth.Client wired to in-memory repositories
// and a trivial token issuer. Internal (white-box) test helper: avoids going
// through New, which reads config and would try to initialise the OIDC RP.
func newAuthService() (*Client, *memory.AccountRepository) {
	accounts := memory.NewAccountRepository()
	clients := memory.NewClientRepository()
	return &Client{
		accounts: accounts,
		clients:  clients,
		issuer:   token.NewIssuer("secret", "sis", 0),
	}, accounts
}

func TestIssueForOIDC_ProvisionsReadOnlyOnFirstLogin(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()

	if _, err := svc.IssueForOIDC(ctx, "sub-1", "ada@uni.edu", "Ada"); err != nil {
		t.Fatalf("first OIDC login: %v", err)
	}

	acc, err := accounts.GetByUsername(ctx, "ada@uni.edu")
	if err != nil {
		t.Fatalf("expected provisioned account: %v", err)
	}
	if len(acc.Roles) != 1 || acc.Roles[0] != domain.AuthRoleReadOnly {
		t.Fatalf("expected read-only role, got %v", acc.Roles)
	}
}

func TestIssueForOIDC_ReusesExistingAccount(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()

	// Pre-create an admin account for this identity.
	if err := svc.EnsureAccount(ctx, "boss@uni.edu", "pw", []domain.AuthRole{domain.AuthRoleAdmin}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	if _, err := svc.IssueForOIDC(ctx, "sub-2", "boss@uni.edu", "Boss"); err != nil {
		t.Fatalf("OIDC login for existing account: %v", err)
	}

	acc, _ := accounts.GetByUsername(ctx, "boss@uni.edu")
	if len(acc.Roles) != 1 || acc.Roles[0] != domain.AuthRoleAdmin {
		t.Fatalf("existing account must keep its role, got %v", acc.Roles)
	}
}

func TestIssueForOIDC_FallsBackToSubjectWithoutEmail(t *testing.T) {
	svc, accounts := newAuthService()
	ctx := context.Background()

	if _, err := svc.IssueForOIDC(ctx, "sub-only", "", ""); err != nil {
		t.Fatalf("OIDC login without email: %v", err)
	}
	if _, err := accounts.GetByUsername(ctx, "sub-only"); err != nil {
		t.Fatalf("expected account keyed by subject: %v", err)
	}
}
