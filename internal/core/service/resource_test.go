package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOrgResource() *service.Resource[domain.Organization, *domain.Organization] {
	repo := memory.NewDocRepo[domain.Organization]()
	return service.NewResource(repo, nil, nil, nil)
}

func TestResource_CreateValidationAndID(t *testing.T) {
	svc := newOrgResource()
	ctx := t.Context()

	_, err := svc.Create(ctx, &domain.Organization{})
	require.Error(t, err, "expected validation error for missing name")

	org, err := svc.Create(ctx, &domain.Organization{Name: "Uppsala University", Type: "institution"})
	require.NoError(t, err, "create")
	assert.NotEmpty(t, org.GetID(), "expected generated id")
	assert.Equal(t, org.GetID(), org.GetSourcedID(), "expected sourcedId defaulting to id")
}

func TestResource_UpsertIsIdempotent(t *testing.T) {
	svc := newOrgResource()
	ctx := t.Context()

	first, err := svc.UpsertBySourcedID(ctx, &domain.Organization{SourcedID: "org-1", Name: "Dept A"})
	require.NoError(t, err, "first upsert")
	second, err := svc.UpsertBySourcedID(ctx, &domain.Organization{SourcedID: "org-1", Name: "Dept A (renamed)"})
	require.NoError(t, err, "second upsert")
	assert.Equal(t, first.GetID(), second.GetID(), "upsert must reuse id")

	page, err := svc.List(ctx, port.ListParams{Limit: 10})
	require.NoError(t, err, "list")
	assert.Equal(t, 1, page.Total, "expected 1 org after repeated upsert")
	require.NotEmpty(t, page.Items)
	assert.Equal(t, "Dept A (renamed)", page.Items[0].Name, "expected updated name")
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
	first, created, err := svc.UpsertWithExpiry(ctx, &domain.Organization{SourcedID: "o", Name: "A"}, &exp1)
	require.NoError(t, err, "first")
	assert.True(t, created, "first")
	require.NotNil(t, first.ExpiresAt, "first")
	assert.True(t, first.ExpiresAt.Equal(exp1), "first: expiresAt = %v, want %v", first.ExpiresAt, exp1)

	exp2 := now.Add(2 * time.Hour)
	second, created, err := svc.UpsertWithExpiry(ctx, &domain.Organization{SourcedID: "o", Name: "B"}, &exp2)
	require.NoError(t, err, "refresh")
	assert.False(t, created, "refresh")
	assert.Equal(t, first.GetID(), second.GetID(), "refresh")
	require.NotNil(t, second.ExpiresAt, "refresh")
	assert.True(t, second.ExpiresAt.Equal(exp2), "refresh: expiresAt = %v, want %v", second.ExpiresAt, exp2)

	// Update keeps the stored expiry whatever the body says.
	updated, err := svc.Update(ctx, &domain.Organization{ID: first.GetID(), SourcedID: "o", Name: "C"})
	require.NoError(t, err, "update")
	require.NotNil(t, updated.ExpiresAt, "update must keep expiry")
	assert.True(t, updated.ExpiresAt.Equal(exp2), "update must keep expiry: %v", updated.ExpiresAt)

	// A permanent record stays permanent.
	perm, err := svc.Create(ctx, &domain.Organization{SourcedID: "p", ExpiresAt: &exp1, Name: "P"})
	require.NoError(t, err, "create")
	assert.Nil(t, perm.ExpiresAt, "Create must drop caller expiry")
	again, _, err := svc.UpsertWithExpiry(ctx, &domain.Organization{SourcedID: "p", Name: "P2"}, &exp1)
	require.NoError(t, err)
	assert.Nil(t, again.ExpiresAt, "permanent record must stay permanent")
	assert.Equal(t, "P2", again.Name, "permanent record must take the data")
}

// interleavedDocs runs before once, just ahead of the first Update, to
// simulate a concurrent writer landing between update's read and write.
type interleavedDocs struct {
	*memory.DocRepo[domain.Organization, *domain.Organization]
	before func()
}

func (repo *interleavedDocs) Update(ctx context.Context, entity *domain.Organization, prev *time.Time) error {
	if before := repo.before; before != nil {
		repo.before = nil
		before()
	}
	return repo.DocRepo.Update(ctx, entity, prev)
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
	_, _, err := svc.UpsertWithExpiry(ctx, &domain.Organization{SourcedID: "o", Name: "A"}, &exp)
	require.NoError(t, err)
	repo.before = func() {
		_, err := svc.UpsertBySourcedID(ctx, &domain.Organization{SourcedID: "o", Name: "Permanent"})
		assert.NoError(t, err, "concurrent permanent upsert")
	}
	later := now.Add(2 * time.Hour)
	out, _, err := svc.UpsertWithExpiry(ctx, &domain.Organization{SourcedID: "o", Name: "Late"}, &later)
	require.NoError(t, err)
	assert.Nil(t, out.ExpiresAt, "late TTL write restored expiry on a permanent record")
	stored, err := svc.GetBySourcedID(ctx, "o")
	require.NoError(t, err)
	assert.Nil(t, stored.ExpiresAt, "want permanent")
	assert.Equal(t, "Late", stored.Name, "want the late data")
}

func TestMergeExpiry(t *testing.T) {
	earlier, later := time.Unix(1, 0), time.Unix(2, 0)
	assert.Nil(t, service.MergeExpiry(nil, &earlier), "permanent stays permanent")
	got := service.MergeExpiry(&earlier, &later)
	if assert.NotNil(t, got, "incoming refreshes") {
		assert.True(t, got.Equal(later), "incoming refreshes")
	}
	assert.Nil(t, service.MergeExpiry(&earlier, nil), "nil incoming makes permanent")
}
