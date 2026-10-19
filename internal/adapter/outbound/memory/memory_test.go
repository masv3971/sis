package memory

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

func TestPersonUpdateRejectsForeignSourcedID(t *testing.T) {
	ctx := context.Background()
	r := NewPersonRepository()
	_ = r.Create(ctx, domain.Person{ID: "1", SourcedID: "a"})
	_ = r.Create(ctx, domain.Person{ID: "2", SourcedID: "b"})

	if err := r.Update(ctx, domain.Person{ID: "2", SourcedID: "a"}, nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	// Keeping its own sourcedId is fine.
	if err := r.Update(ctx, domain.Person{ID: "2", SourcedID: "b", GivenName: "x"}, nil); err != nil {
		t.Fatalf("self update: %v", err)
	}
}

func TestPersonListLimitDoesNotOverflow(t *testing.T) {
	ctx := context.Background()
	r := NewPersonRepository()
	_ = r.Create(ctx, domain.Person{ID: "1", SourcedID: "a"})
	_ = r.Create(ctx, domain.Person{ID: "2", SourcedID: "b"})

	page, err := r.List(ctx, port.ListParams{Offset: 1, Limit: math.MaxInt})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Total != 2 {
		t.Fatalf("expected 1 item of 2, got %d of %d", len(page.Items), page.Total)
	}
}

func TestPersonListStableForSameName(t *testing.T) {
	ctx := context.Background()
	r := NewPersonRepository()
	for _, id := range []string{"c", "a", "b"} {
		_ = r.Create(ctx, domain.Person{ID: id, SourcedID: id, GivenName: "Ada", FamilyName: "Lovelace"})
	}
	for range 20 {
		page, _ := r.List(ctx, port.ListParams{})
		if page.Items[0].ID != "a" || page.Items[1].ID != "b" || page.Items[2].ID != "c" {
			t.Fatalf("unstable order: %v %v %v", page.Items[0].ID, page.Items[1].ID, page.Items[2].ID)
		}
	}
}

func TestDocRepoUpdateRejectsForeignSourcedID(t *testing.T) {
	ctx := context.Background()
	r := NewDocRepo[domain.Course]()
	a := &domain.Course{Base: domain.Base{ID: "1", SourcedID: "a"}, Title: "A"}
	b := &domain.Course{Base: domain.Base{ID: "2", SourcedID: "b"}, Title: "B"}
	_ = r.Create(ctx, a)
	_ = r.Create(ctx, b)

	steal := &domain.Course{Base: domain.Base{ID: "2", SourcedID: "a"}, Title: "B"}
	if err := r.Update(ctx, steal, nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	// The original owner must still be reachable by its business key.
	got, err := r.GetBySourcedID(ctx, "a")
	if err != nil || got.GetID() != "1" {
		t.Fatalf("sourcedId index corrupted: id=%v err=%v", got, err)
	}
}

// TestDocRepoUpdateMarshalFailurePreservesIndex checks that an Update which
// changes sourcedId but fails to serialize leaves the stored record and its
// sourcedId lookup untouched.
func TestDocRepoUpdateMarshalFailurePreservesIndex(t *testing.T) {
	ctx := context.Background()
	r := NewDocRepo[domain.Course]()
	if err := r.Create(ctx, &domain.Course{Base: domain.Base{ID: "1", SourcedID: "a"}, Title: "A"}); err != nil {
		t.Fatal(err)
	}

	// Year 10000 is outside the RFC 3339 range, so json.Marshal fails.
	bad := &domain.Course{Base: domain.Base{ID: "1", SourcedID: "b", UpdatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, Title: "B"}
	if err := r.Update(ctx, bad, nil); err == nil {
		t.Fatal("expected marshal error")
	}

	got, err := r.GetBySourcedID(ctx, "a")
	if err != nil || got.GetID() != "1" || got.Title != "A" {
		t.Fatalf("failed update corrupted state: got=%+v err=%v", got, err)
	}
	if _, err := r.GetBySourcedID(ctx, "b"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("new sourcedId must not be indexed after a failed update, got %v", err)
	}
}

func TestPageBounds(t *testing.T) {
	cases := []struct{ total, off, lim, lo, hi int }{
		{10, 0, 0, 0, 10},
		{10, 0, 3, 0, 3},
		{10, 8, 5, 8, 10},
		{10, 20, 5, 10, 10},
		{10, 1, math.MaxInt, 1, 10},
		{10, -1, 2, 0, 2},
	}
	for _, c := range cases {
		lo, hi := pageBounds(c.total, c.off, c.lim)
		if lo != c.lo || hi != c.hi {
			t.Errorf("pageBounds(%d,%d,%d) = %d,%d; want %d,%d", c.total, c.off, c.lim, lo, hi, c.lo, c.hi)
		}
	}
}

func TestAccountUpsertRejectsDuplicateIdentity(t *testing.T) {
	ctx := context.Background()
	r := NewAccountRepository()
	base := domain.Account{ID: "1", Username: "oidc:a", OIDCIssuer: "https://idp", OIDCSubject: "sub"}
	if err := r.Upsert(ctx, base); err != nil {
		t.Fatalf("insert: %v", err)
	}

	sameIdentity := domain.Account{ID: "2", Username: "oidc:b", OIDCIssuer: "https://idp", OIDCSubject: "sub"}
	if err := r.Upsert(ctx, sameIdentity); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate OIDC identity: expected ErrConflict, got %v", err)
	}
	sameID := domain.Account{ID: "1", Username: "local"}
	if err := r.Upsert(ctx, sameID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate id: expected ErrConflict, got %v", err)
	}

	// Updating the same username keeps its id and binding.
	if err := r.Upsert(ctx, domain.Account{ID: "ignored", Username: "oidc:a", OIDCSubject: "other"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := r.GetByOIDC(ctx, "https://idp", "sub")
	if err != nil || got.ID != "1" {
		t.Fatalf("binding not preserved: %+v %v", got, err)
	}
	// Local accounts (no OIDC subject) never conflict with each other.
	if err := r.Upsert(ctx, domain.Account{ID: "3", Username: "l1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Upsert(ctx, domain.Account{ID: "4", Username: "l2"}); err != nil {
		t.Fatal(err)
	}
}

// TestPersonRepositoryIsolatesSlices checks that neither the value passed to
// a write nor a value returned by a read shares slice storage with the
// repository, so mutating them cannot change stored data.
func TestPersonRepositoryIsolatesSlices(t *testing.T) {
	ctx := context.Background()
	r := NewPersonRepository()
	p := domain.Person{
		ID: "1", SourcedID: "a",
		Roles:       []domain.Role{domain.RoleStudent},
		Identifiers: []domain.Identifier{{Type: "nin", Value: "x"}},
		Emails:      []domain.Contact{{Type: "work", Value: "a@x"}},
		Phones:      []domain.Contact{{Type: "work", Value: "1"}},
	}
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.Roles[0], p.Identifiers[0].Value, p.Emails[0].Value, p.Phones[0].Value = domain.RoleStaff, "y", "b@x", "2"

	check := func(stage string) {
		t.Helper()
		got, err := r.Get(ctx, "1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Roles[0] != domain.RoleStudent || got.Identifiers[0].Value != "x" ||
			got.Emails[0].Value != "a@x" || got.Phones[0].Value != "1" {
			t.Fatalf("%s: stored person changed: %+v", stage, got)
		}
	}
	check("after mutating Create argument")

	mutate := func(q domain.Person) {
		q.Roles[0], q.Identifiers[0].Value, q.Emails[0].Value, q.Phones[0].Value = domain.RoleGuest, "z", "c@x", "3"
	}
	got, _ := r.Get(ctx, "1")
	mutate(got)
	check("after mutating Get result")
	got, _ = r.GetBySourcedID(ctx, "a")
	mutate(got)
	check("after mutating GetBySourcedID result")
	page, _ := r.List(ctx, port.ListParams{})
	mutate(page.Items[0])
	check("after mutating List result")

	upd, _ := r.Get(ctx, "1")
	if err := r.Update(ctx, upd, nil); err != nil {
		t.Fatal(err)
	}
	mutate(upd)
	check("after mutating Update argument")
}

// TestAuthRepositoriesIsolateSlices checks Roles and Scopes are cloned on
// Upsert and on every lookup, so callers cannot change stored authorization.
func TestAuthRepositoriesIsolateSlices(t *testing.T) {
	ctx := context.Background()
	accounts := NewAccountRepository()
	a := domain.Account{Username: "u", Roles: []domain.AuthRole{domain.AuthRoleReadOnly}, OIDCIssuer: "i", OIDCSubject: "s"}
	if err := accounts.Upsert(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Roles[0] = domain.AuthRoleAdmin
	byName, _ := accounts.GetByUsername(ctx, "u")
	byName.Roles[0] = domain.AuthRoleAdmin
	byOIDC, _ := accounts.GetByOIDC(ctx, "i", "s")
	if byOIDC.Roles[0] != domain.AuthRoleReadOnly {
		t.Fatalf("stored roles changed: %v", byOIDC.Roles)
	}
	byOIDC.Roles[0] = domain.AuthRoleAdmin
	if err := accounts.Upsert(ctx, byName); err != nil { // update path
		t.Fatal(err)
	}
	byName.Roles[0] = domain.AuthRoleRegistrar
	if got, _ := accounts.GetByUsername(ctx, "u"); got.Roles[0] != domain.AuthRoleAdmin {
		t.Fatalf("roles after update path: %v", got.Roles)
	}

	clients := NewClientRepository()
	c := domain.Client{ClientID: "c", Scopes: []string{domain.ScopeRead}}
	if err := clients.Upsert(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.Scopes[0] = domain.ScopeWrite
	got, _ := clients.GetByClientID(ctx, "c")
	got.Scopes[0] = domain.ScopeWrite
	if got, _ := clients.GetByClientID(ctx, "c"); got.Scopes[0] != domain.ScopeRead {
		t.Fatalf("stored scopes changed: %v", got.Scopes)
	}
}
