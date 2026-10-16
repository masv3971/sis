package postgres_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/domain"
)

// TestExpiry checks expired rows are invisible to reads, that their keys can
// be reused by Create, and that DeleteExpired removes and reports them.
func TestExpiry(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()
	past := now.Add(-time.Minute)

	courses := postgres.NewDocRepo[domain.Course](svc)
	c := &domain.Course{Base: domain.Base{ID: "xc-" + suffix, SourcedID: "xcs-" + suffix, CreatedAt: now, UpdatedAt: now, ExpiresAt: &past}, Title: "A"}
	t.Cleanup(func() {
		_, _ = courses.DeleteExpired(ctx, time.Now().Add(time.Hour))
		_ = courses.Delete(ctx, "xc2-"+suffix)
	})
	if err := courses.Create(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := courses.Get(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get expired: %v", err)
	}
	if _, err := courses.GetBySourcedID(ctx, c.SourcedID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetBySourcedID expired: %v", err)
	}
	if cs, _ := courses.GetByIDs(ctx, []string{c.ID}); len(cs) != 0 {
		t.Fatalf("GetByIDs expired: %v", cs)
	}
	if err := courses.Update(ctx, c); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update expired: %v", err)
	}

	// Reusing the sourcedId replaces the expired row.
	c2 := &domain.Course{Base: domain.Base{ID: "xc2-" + suffix, SourcedID: c.SourcedID, CreatedAt: now, UpdatedAt: now}, Title: "B"}
	if err := courses.Create(ctx, c2); err != nil {
		t.Fatalf("create over expired: %v", err)
	}
	got, err := courses.GetBySourcedID(ctx, c.SourcedID)
	if err != nil || got.ID != c2.ID {
		t.Fatalf("replacement: %+v %v", got, err)
	}

	// DeleteExpired returns the expired ids.
	c3 := &domain.Course{Base: domain.Base{ID: "xc3-" + suffix, SourcedID: "xcs3-" + suffix, CreatedAt: now, UpdatedAt: now, ExpiresAt: &past}, Title: "C"}
	if err := courses.Create(ctx, c3); err != nil {
		t.Fatalf("create c3: %v", err)
	}
	ids, err := courses.DeleteExpired(ctx, now)
	if err != nil || !slices.Contains(ids, c3.ID) {
		t.Fatalf("DeleteExpired = %v, %v", ids, err)
	}

	persons := svc.Persons()
	p := domain.Person{ID: "xp-" + suffix, SourcedID: "xps-" + suffix, Status: domain.StatusActive, GivenName: "A", FamilyName: "B", CreatedAt: now, UpdatedAt: now, ExpiresAt: &past}
	t.Cleanup(func() { _, _ = persons.DeleteExpired(ctx, time.Now().Add(time.Hour)) })
	if err := persons.Create(ctx, p); err != nil {
		t.Fatalf("create person: %v", err)
	}
	if _, err := persons.GetBySourcedID(ctx, p.SourcedID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("person GetBySourcedID expired: %v", err)
	}
	pids, err := persons.DeleteExpired(ctx, now)
	if err != nil || !slices.Contains(pids, p.ID) {
		t.Fatalf("person DeleteExpired = %v, %v", pids, err)
	}
}
