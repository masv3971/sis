package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	course := &domain.Course{ID: "xc-" + suffix, SourcedID: "xcs-" + suffix, CreatedAt: now, UpdatedAt: now, ExpiresAt: &past, Title: "A"}
	t.Cleanup(func() {
		_, _ = courses.DeleteExpired(context.Background(), time.Now().Add(time.Hour))
		_ = courses.Delete(context.Background(), "xc2-"+suffix)
	})
	require.NoError(t, courses.Create(ctx, course), "create")
	_, err := courses.Get(ctx, course.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound, "Get expired")
	_, err = courses.GetBySourcedID(ctx, course.SourcedID)
	assert.ErrorIs(t, err, domain.ErrNotFound, "GetBySourcedID expired")
	gotCourses, _ := courses.GetByIDs(ctx, []string{course.ID})
	assert.Empty(t, gotCourses, "GetByIDs expired")
	assert.ErrorIs(t, courses.Update(ctx, course, course.ExpiresAt), domain.ErrNotFound, "Update expired")

	// Reusing the sourcedId replaces the expired row.
	replacement := &domain.Course{ID: "xc2-" + suffix, SourcedID: course.SourcedID, CreatedAt: now, UpdatedAt: now, Title: "B"}
	require.NoError(t, courses.Create(ctx, replacement), "create over expired")
	got, err := courses.GetBySourcedID(ctx, course.SourcedID)
	require.NoError(t, err, "replacement")
	assert.Equal(t, replacement.ID, got.ID, "replacement")

	// DeleteExpired returns the expired ids.
	expiring := &domain.Course{ID: "xc3-" + suffix, SourcedID: "xcs3-" + suffix, CreatedAt: now, UpdatedAt: now, ExpiresAt: &past, Title: "C"}
	require.NoError(t, courses.Create(ctx, expiring), "create c3")
	ids, err := courses.DeleteExpired(ctx, now)
	require.NoError(t, err, "DeleteExpired")
	assert.Contains(t, ids, expiring.ID, "DeleteExpired")

	persons := svc.Persons()
	person := domain.Person{ID: "xp-" + suffix, SourcedID: "xps-" + suffix, Status: domain.StatusActive, GivenName: "A", FamilyName: "B", CreatedAt: now, UpdatedAt: now, ExpiresAt: &past}
	t.Cleanup(func() { _, _ = persons.DeleteExpired(context.Background(), time.Now().Add(time.Hour)) })
	require.NoError(t, persons.Create(ctx, person), "create person")
	_, err = persons.GetBySourcedID(ctx, person.SourcedID)
	assert.ErrorIs(t, err, domain.ErrNotFound, "person GetBySourcedID expired")
	personIDs, err := persons.DeleteExpired(ctx, now)
	require.NoError(t, err, "person DeleteExpired")
	assert.Contains(t, personIDs, person.ID, "person DeleteExpired")
}

// TestUpdateIsConditionalOnExpiry checks Update applies only while the
// stored expiry equals prevExpiresAt (ErrStale otherwise, row untouched) and
// still reports ErrNotFound for a missing row.
func TestUpdateIsConditionalOnExpiry(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC().Truncate(time.Microsecond)
	exp := now.Add(time.Hour)

	courses := postgres.NewDocRepo[domain.Course](svc)
	course := &domain.Course{ID: "cc-" + suffix, SourcedID: "ccs-" + suffix, CreatedAt: now, UpdatedAt: now, ExpiresAt: &exp, Title: "A"}
	t.Cleanup(func() { _ = courses.Delete(context.Background(), course.ID) })
	require.NoError(t, courses.Create(ctx, course))
	course.Title, course.ExpiresAt = "B", nil
	assert.ErrorIs(t, courses.Update(ctx, course, nil), domain.ErrStale, "doc")
	got, _ := courses.Get(ctx, course.ID)
	assert.Equal(t, "A", got.Title, "doc: stale update was applied")
	assert.NotNil(t, got.ExpiresAt, "doc: stale update was applied")
	assert.NoError(t, courses.Update(ctx, course, &exp), "doc: matching precondition")
	missing := &domain.Course{ID: "cc-missing-" + suffix, SourcedID: "ccm-" + suffix, Title: "M"}
	assert.ErrorIs(t, courses.Update(ctx, missing, nil), domain.ErrNotFound, "doc: missing row")

	persons := svc.Persons()
	person := domain.Person{ID: "cp-" + suffix, SourcedID: "cps-" + suffix, Status: domain.StatusActive, GivenName: "A", FamilyName: "B", CreatedAt: now, UpdatedAt: now}
	t.Cleanup(func() { _ = persons.Delete(context.Background(), person.ID) })
	require.NoError(t, persons.Create(ctx, person))
	person.GivenName, person.ExpiresAt = "C", &exp
	assert.ErrorIs(t, persons.Update(ctx, person, &exp), domain.ErrStale, "person")
	assert.NoError(t, persons.Update(ctx, person, nil), "person: matching precondition")
	assert.ErrorIs(t, persons.Update(ctx, domain.Person{ID: "cp-missing-" + suffix, SourcedID: "x"}, nil), domain.ErrNotFound, "person: missing row")
}
