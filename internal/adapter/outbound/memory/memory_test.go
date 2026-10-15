package memory

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

func TestPersonUpdateRejectsForeignSourcedID(t *testing.T) {
	ctx := context.Background()
	r := NewPersonRepository()
	_ = r.Create(ctx, domain.Person{ID: "1", SourcedID: "a"})
	_ = r.Create(ctx, domain.Person{ID: "2", SourcedID: "b"})

	if err := r.Update(ctx, domain.Person{ID: "2", SourcedID: "a"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	// Keeping its own sourcedId is fine.
	if err := r.Update(ctx, domain.Person{ID: "2", SourcedID: "b", GivenName: "x"}); err != nil {
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
	if err := r.Update(ctx, steal); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	// The original owner must still be reachable by its business key.
	got, err := r.GetBySourcedID(ctx, "a")
	if err != nil || got.GetID() != "1" {
		t.Fatalf("sourcedId index corrupted: id=%v err=%v", got, err)
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
