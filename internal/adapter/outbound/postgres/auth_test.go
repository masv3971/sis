package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
)

func TestAccountOIDCBinding(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Accounts()
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()

	local := domain.Account{ID: "l-" + suffix, Username: "local-" + suffix, PasswordHash: "x", Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repo.Upsert(ctx, local); err != nil {
		t.Fatalf("upsert local: %v", err)
	}
	if _, err := repo.GetByOIDC(ctx, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty subject must never match a local account, got %v", err)
	}

	fed := domain.Account{
		ID: "o-" + suffix, Username: "oidc-" + suffix, PasswordHash: "x", Status: domain.StatusActive,
		OIDCIssuer: "https://idp.example", OIDCSubject: "sub-" + suffix, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Upsert(ctx, fed); err != nil {
		t.Fatalf("upsert oidc: %v", err)
	}
	got, err := repo.GetByOIDC(ctx, fed.OIDCIssuer, fed.OIDCSubject)
	if err != nil || got.ID != fed.ID {
		t.Fatalf("GetByOIDC: %+v %v", got, err)
	}

	// Re-upserting the same username must not rebind the identity.
	rebind := fed
	rebind.OIDCSubject = "someone-else"
	if err := repo.Upsert(ctx, rebind); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if _, err := repo.GetByOIDC(ctx, fed.OIDCIssuer, "someone-else"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("upsert rebound the OIDC identity: %v", err)
	}

	// A second account with the same issuer+subject is a conflict.
	dup := fed
	dup.ID, dup.Username = "d-"+suffix, "dup-"+suffix
	if err := repo.Upsert(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate identity, got %v", err)
	}
}

func TestAccountCreateNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Accounts()
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()

	a := domain.Account{
		ID: "c-" + suffix, Username: "create-" + suffix, Status: domain.StatusInactive,
		Roles:      []domain.AuthRole{domain.AuthRoleRegistrar},
		OIDCIssuer: "https://idp.example", OIDCSubject: "csub-" + suffix, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	again := a
	again.ID = "c2-" + suffix
	again.Status = domain.StatusActive
	again.Roles = []domain.AuthRole{domain.AuthRoleReadOnly}
	if err := repo.Create(ctx, again); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	got, err := repo.GetByOIDC(ctx, a.OIDCIssuer, a.OIDCSubject)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Status != domain.StatusInactive || got.Roles[0] != domain.AuthRoleRegistrar {
		t.Fatalf("Create modified the existing account: %+v", got)
	}
}
