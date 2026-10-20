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

// TestPersonSourcedIDIndex checks the sourcedId index follows renames,
// deletes and sweeps, so a freed sourcedId resolves to nothing and can be
// reused.
func TestPersonSourcedIDIndex(t *testing.T) {
	ctx := context.Background()
	r := NewPersonRepository()
	mustNoErr(t, r.Create(ctx, domain.Person{ID: "1", SourcedID: "a"}))
	mustNoErr(t, r.Update(ctx, domain.Person{ID: "1", SourcedID: "b"}, nil))
	if _, err := r.GetBySourcedID(ctx, "a"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("old sourcedId after rename: %v", err)
	}
	if ps, _ := r.GetBySourcedIDs(ctx, []string{"a", "b"}); len(ps) != 1 || ps[0].ID != "1" {
		t.Fatalf("GetBySourcedIDs = %+v", ps)
	}
	mustNoErr(t, r.Create(ctx, domain.Person{ID: "2", SourcedID: "a"}))

	mustNoErr(t, r.Delete(ctx, "1"))
	if _, ok := r.sourcedToID["b"]; ok {
		t.Fatal("Delete must clean the index")
	}
	exp := time.Now().Add(-time.Minute)
	mustNoErr(t, r.Update(ctx, domain.Person{ID: "2", SourcedID: "a", ExpiresAt: &exp}, nil))
	if _, err := r.DeleteExpired(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(r.sourcedToID) != 0 || len(r.byID) != 0 {
		t.Fatalf("DeleteExpired must clean the index: %v %v", r.sourcedToID, r.byID)
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
