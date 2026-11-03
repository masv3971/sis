package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// TestDocRepoExpiry checks an expired entity disappears from every read,
// cannot be updated or deleted, is replaced by a Create holding its keys, and
// is removed by DeleteExpired.
func TestDocRepoExpiry(t *testing.T) {
	ctx := t.Context()
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	repo := NewDocRepo[domain.Course]().WithClock(clk.now)
	exp := clk.t.Add(time.Hour)
	require.NoError(t, repo.Create(ctx, &domain.Course{ID: "1", SourcedID: "a", ExpiresAt: &exp, Title: "A"}))
	require.NoError(t, repo.Create(ctx, &domain.Course{ID: "2", SourcedID: "b", Title: "B"}))

	_, err := repo.GetBySourcedID(ctx, "a")
	require.NoError(t, err, "live record must be readable")

	clk.t = exp // inclusive boundary
	assertGone(ctx, t, repo, "1", "a")
	page, err := repo.List(ctx, port.ListParams{})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total, "List must skip expired")
	require.NotEmpty(t, page.Items, "List must skip expired")
	assert.Equal(t, "2", page.Items[0].GetID(), "List must skip expired")
	assert.ErrorIs(t, repo.Update(ctx, &domain.Course{ID: "1", SourcedID: "a", Title: "x"}, &exp), domain.ErrNotFound, "Update of expired")
	assert.ErrorIs(t, repo.Delete(ctx, "1"), domain.ErrNotFound, "Delete of expired")

	// A new record may take the expired one's sourcedId.
	require.NoError(t, repo.Create(ctx, &domain.Course{ID: "3", SourcedID: "a", Title: "A2"}))
	got, err := repo.GetBySourcedID(ctx, "a")
	require.NoError(t, err, "replacement not found")
	assert.Equal(t, "3", got.GetID(), "replacement not found")
	assert.NotContains(t, repo.byID, "1", "expired record must be dropped when its sourcedId is reused")

	// DeleteExpired reports exactly the expired ids.
	exp2 := clk.t.Add(time.Minute)
	require.NoError(t, repo.Create(ctx, &domain.Course{ID: "4", SourcedID: "d", ExpiresAt: &exp2, Title: "D"}))
	ids, err := repo.DeleteExpired(ctx, exp2)
	require.NoError(t, err)
	assert.Equal(t, []string{"4"}, ids, "DeleteExpired")
	assert.NotContains(t, repo.sourcedToID, "d", "sourcedId index must be cleaned")
}

func TestPersonRepoExpiry(t *testing.T) {
	ctx := t.Context()
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	repo := NewPersonRepository().WithClock(clk.now)
	require.NoError(t, repo.Create(ctx, domain.Person{ID: "1", SourcedID: "a", ExpiresAt: new(clk.t.Add(time.Hour))}))
	require.NoError(t, repo.Create(ctx, domain.Person{ID: "2", SourcedID: "b"}))

	clk.t = clk.t.Add(2 * time.Hour)
	_, err := repo.Get(ctx, "1")
	assert.ErrorIs(t, err, domain.ErrNotFound, "Get expired")
	_, err = repo.GetBySourcedID(ctx, "a")
	assert.ErrorIs(t, err, domain.ErrNotFound, "GetBySourcedID expired")
	persons, _ := repo.GetByIDs(ctx, []string{"1", "2"})
	assert.Len(t, persons, 1, "GetByIDs must skip expired")
	page, _ := repo.List(ctx, port.ListParams{})
	assert.Equal(t, 1, page.Total, "List must skip expired")
	require.NoError(t, repo.Create(ctx, domain.Person{ID: "3", SourcedID: "a"}))
	got, err := repo.GetBySourcedID(ctx, "a")
	require.NoError(t, err, "replacement")
	assert.Equal(t, "3", got.ID, "replacement")
	ids, err := repo.DeleteExpired(ctx, clk.t)
	require.NoError(t, err)
	assert.Empty(t, ids, "expired record was already replaced")
}

// TestDocRepoCopiesExpiry checks Create and Update store a copy of the
// caller's expiry, so changing the caller's time afterwards cannot change
// visibility or sweeper eligibility without a repository write.
func TestDocRepoCopiesExpiry(t *testing.T) {
	ctx := t.Context()
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	repo := NewDocRepo[domain.Course]().WithClock(clk.now)
	later := clk.t.Add(time.Hour)

	exp := later
	require.NoError(t, repo.Create(ctx, &domain.Course{ID: "1", SourcedID: "a", ExpiresAt: &exp, Title: "A"}))
	exp = clk.t // would make it expired now if aliased
	_, err := repo.Get(ctx, "1")
	require.NoError(t, err, "after mutating Create expiry")

	upd := later.Add(time.Hour)
	require.NoError(t, repo.Update(ctx, &domain.Course{ID: "1", SourcedID: "a", ExpiresAt: &upd, Title: "A"}, &later))
	upd = clk.t
	_, err = repo.Get(ctx, "1")
	require.NoError(t, err, "after mutating Update expiry")
	ids, _ := repo.DeleteExpired(ctx, clk.t)
	assert.Empty(t, ids, "sweeper saw the caller's mutated expiry")
}

// TestUpdateIsConditionalOnExpiry checks Update applies only while the
// stored expiry equals prevExpiresAt, in both repositories, and leaves the
// record untouched otherwise.
func TestUpdateIsConditionalOnExpiry(t *testing.T) {
	ctx := t.Context()
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	exp, other := clk.t.Add(time.Hour), clk.t.Add(2*time.Hour)

	docs := NewDocRepo[domain.Course]().WithClock(clk.now)
	require.NoError(t, docs.Create(ctx, &domain.Course{ID: "1", SourcedID: "a", Title: "A"}))
	assert.ErrorIs(t, docs.Update(ctx, &domain.Course{ID: "1", SourcedID: "a", ExpiresAt: &other, Title: "B"}, &exp), domain.ErrStale, "doc")
	gotDoc, _ := docs.Get(ctx, "1")
	assert.Equal(t, "A", gotDoc.Title, "doc: stale update was applied")
	assert.Nil(t, gotDoc.ExpiresAt, "doc: stale update was applied")
	require.NoError(t, docs.Update(ctx, &domain.Course{ID: "1", SourcedID: "a", Title: "B"}, nil))

	persons := NewPersonRepository().WithClock(clk.now)
	require.NoError(t, persons.Create(ctx, domain.Person{ID: "1", SourcedID: "a", ExpiresAt: &exp}))
	assert.ErrorIs(t, persons.Update(ctx, domain.Person{ID: "1", SourcedID: "a", GivenName: "B"}, nil), domain.ErrStale, "person")
	gotPerson, _ := persons.Get(ctx, "1")
	assert.Empty(t, gotPerson.GivenName, "person: stale update was applied")
	assert.NotNil(t, gotPerson.ExpiresAt, "person: stale update was applied")
	require.NoError(t, persons.Update(ctx, domain.Person{ID: "1", SourcedID: "a", GivenName: "B"}, new(exp)))
}

func assertGone(ctx context.Context, t *testing.T, repo *DocRepo[domain.Course, *domain.Course], id, sid string) {
	t.Helper()
	_, err := repo.Get(ctx, id)
	assert.ErrorIs(t, err, domain.ErrNotFound, "Get expired")
	_, err = repo.GetBySourcedID(ctx, sid)
	assert.ErrorIs(t, err, domain.ErrNotFound, "GetBySourcedID expired")
	got, _ := repo.GetByIDs(ctx, []string{id})
	assert.Empty(t, got, "GetByIDs expired")
}
