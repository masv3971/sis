package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
)

func newOrgResource() *service.Resource[domain.Organization, *domain.Organization] {
	repo := memory.NewDocRepo[domain.Organization]()
	return service.NewResource(repo, nil, nil, nil)
}

func TestResource_CreateValidationAndID(t *testing.T) {
	svc := newOrgResource()
	ctx := context.Background()

	if _, err := svc.Create(ctx, &domain.Organization{}); err == nil {
		t.Fatal("expected validation error for missing name")
	}

	org, err := svc.Create(ctx, &domain.Organization{Name: "Uppsala University", Type: "institution"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if org.GetID() == "" || org.GetSourcedID() != org.GetID() {
		t.Fatalf("expected generated id with sourcedId defaulting to it: %+v", org)
	}
}

func TestResource_UpsertIsIdempotent(t *testing.T) {
	svc := newOrgResource()
	ctx := context.Background()

	first, err := svc.UpsertBySourcedID(ctx, &domain.Organization{Base: domain.Base{SourcedID: "org-1"}, Name: "Dept A"})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := svc.UpsertBySourcedID(ctx, &domain.Organization{Base: domain.Base{SourcedID: "org-1"}, Name: "Dept A (renamed)"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.GetID() != second.GetID() {
		t.Fatalf("upsert must reuse id: %s != %s", first.GetID(), second.GetID())
	}

	page, err := svc.List(ctx, port.ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected 1 org after repeated upsert, got %d", page.Total)
	}
	if page.Items[0].Name != "Dept A (renamed)" {
		t.Fatalf("expected updated name, got %q", page.Items[0].Name)
	}
}

// TestResource_UpsertWithExpiry checks expiry is set on create, refreshed on
// re-upsert, never added to a permanent record, and that /api/v1-style
// Create/Update cannot set or clear it.
func TestResource_UpsertWithExpiry(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc := service.NewResource(memory.NewDocRepo[domain.Organization]().WithClock(clock), nil, clock, nil)

	exp1 := now.Add(time.Hour)
	first, created, err := svc.UpsertWithExpiry(ctx, &domain.Organization{Base: domain.Base{SourcedID: "o"}, Name: "A"}, &exp1)
	if err != nil || !created || first.ExpiresAt == nil || !first.ExpiresAt.Equal(exp1) {
		t.Fatalf("first: %+v created=%v err=%v", first, created, err)
	}

	exp2 := now.Add(2 * time.Hour)
	second, created, err := svc.UpsertWithExpiry(ctx, &domain.Organization{Base: domain.Base{SourcedID: "o"}, Name: "B"}, &exp2)
	if err != nil || created || second.GetID() != first.GetID() || !second.ExpiresAt.Equal(exp2) {
		t.Fatalf("refresh: %+v created=%v err=%v", second, created, err)
	}

	// Update keeps the stored expiry whatever the body says.
	upd, err := svc.Update(ctx, &domain.Organization{Base: domain.Base{ID: first.GetID(), SourcedID: "o"}, Name: "C"})
	if err != nil || upd.ExpiresAt == nil || !upd.ExpiresAt.Equal(exp2) {
		t.Fatalf("update must keep expiry: %+v %v", upd, err)
	}

	// A permanent record stays permanent.
	perm, err := svc.Create(ctx, &domain.Organization{Base: domain.Base{SourcedID: "p", ExpiresAt: &exp1}, Name: "P"})
	if err != nil || perm.ExpiresAt != nil {
		t.Fatalf("Create must drop caller expiry: %+v %v", perm, err)
	}
	again, _, err := svc.UpsertWithExpiry(ctx, &domain.Organization{Base: domain.Base{SourcedID: "p"}, Name: "P2"}, &exp1)
	if err != nil || again.ExpiresAt != nil || again.Name != "P2" {
		t.Fatalf("permanent record must stay permanent but take the data: %+v %v", again, err)
	}
}

// interleavedDocs runs before once, just ahead of the first Update, to
// simulate a concurrent writer landing between update's read and write.
type interleavedDocs struct {
	*memory.DocRepo[domain.Organization, *domain.Organization]
	before func()
}

func (r *interleavedDocs) Update(ctx context.Context, e *domain.Organization, prev *time.Time) error {
	if f := r.before; f != nil {
		r.before = nil
		f()
	}
	return r.DocRepo.Update(ctx, e, prev)
}

// TestResource_UpsertWithExpiryRace is the race from the PR #2 review: a TTL
// upsert reads a temporary record, a no-TTL upsert makes it permanent, then
// the TTL upsert writes. The late write must not restore a finite expiry
// (which would let the sweeper delete a permanent record).
func TestResource_UpsertWithExpiryRace(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	repo := &interleavedDocs{DocRepo: memory.NewDocRepo[domain.Organization]().WithClock(clock)}
	svc := service.NewResource(repo, nil, clock, nil)

	exp := now.Add(time.Hour)
	if _, _, err := svc.UpsertWithExpiry(ctx, &domain.Organization{Base: domain.Base{SourcedID: "o"}, Name: "A"}, &exp); err != nil {
		t.Fatal(err)
	}
	repo.before = func() {
		if _, err := svc.UpsertBySourcedID(ctx, &domain.Organization{Base: domain.Base{SourcedID: "o"}, Name: "Permanent"}); err != nil {
			t.Errorf("concurrent permanent upsert: %v", err)
		}
	}
	later := now.Add(2 * time.Hour)
	out, _, err := svc.UpsertWithExpiry(ctx, &domain.Organization{Base: domain.Base{SourcedID: "o"}, Name: "Late"}, &later)
	if err != nil {
		t.Fatal(err)
	}
	if out.ExpiresAt != nil {
		t.Fatalf("late TTL write restored expiry %v on a permanent record", out.ExpiresAt)
	}
	stored, err := svc.GetBySourcedID(ctx, "o")
	if err != nil || stored.ExpiresAt != nil || stored.Name != "Late" {
		t.Fatalf("stored = %+v, %v; want permanent with the late data", stored, err)
	}
}

func TestMergeExpiry(t *testing.T) {
	a, b := time.Unix(1, 0), time.Unix(2, 0)
	if service.MergeExpiry(nil, &a) != nil {
		t.Fatal("permanent stays permanent")
	}
	if got := service.MergeExpiry(&a, &b); got == nil || !got.Equal(b) {
		t.Fatal("incoming refreshes")
	}
	if service.MergeExpiry(&a, nil) != nil {
		t.Fatal("nil incoming makes permanent")
	}
}
