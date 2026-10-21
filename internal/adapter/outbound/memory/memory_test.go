package memory

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

func TestPersonUpdateRejectsForeignSourcedID(t *testing.T) {
	ctx := t.Context()
	repo := NewPersonRepository()
	_ = repo.Create(ctx, domain.Person{ID: "1", SourcedID: "a"})
	_ = repo.Create(ctx, domain.Person{ID: "2", SourcedID: "b"})

	require.ErrorIs(t, repo.Update(ctx, domain.Person{ID: "2", SourcedID: "a"}, nil), domain.ErrConflict)
	// Keeping its own sourcedId is fine.
	assert.NoError(t, repo.Update(ctx, domain.Person{ID: "2", SourcedID: "b", GivenName: "x"}, nil), "self update")
}

// TestPersonSourcedIDIndex checks the sourcedId index follows renames,
// deletes and sweeps, so a freed sourcedId resolves to nothing and can be
// reused.
func TestPersonSourcedIDIndex(t *testing.T) {
	ctx := t.Context()
	repo := NewPersonRepository()
	require.NoError(t, repo.Create(ctx, domain.Person{ID: "1", SourcedID: "a"}))
	require.NoError(t, repo.Update(ctx, domain.Person{ID: "1", SourcedID: "b"}, nil))
	_, err := repo.GetBySourcedID(ctx, "a")
	require.ErrorIs(t, err, domain.ErrNotFound, "old sourcedId after rename")
	persons, _ := repo.GetBySourcedIDs(ctx, []string{"a", "b"})
	require.Len(t, persons, 1, "GetBySourcedIDs")
	require.Equal(t, "1", persons[0].ID, "GetBySourcedIDs")
	require.NoError(t, repo.Create(ctx, domain.Person{ID: "2", SourcedID: "a"}))

	require.NoError(t, repo.Delete(ctx, "1"))
	require.NotContains(t, repo.sourcedToID, "b", "Delete must clean the index")
	exp := time.Now().Add(-time.Minute)
	require.NoError(t, repo.Update(ctx, domain.Person{ID: "2", SourcedID: "a", ExpiresAt: &exp}, nil))
	_, err = repo.DeleteExpired(ctx, time.Now())
	require.NoError(t, err)
	assert.Empty(t, repo.sourcedToID, "DeleteExpired must clean the index")
	assert.Empty(t, repo.byID, "DeleteExpired must clean the index")
}

func TestPersonListLimitDoesNotOverflow(t *testing.T) {
	ctx := t.Context()
	repo := NewPersonRepository()
	_ = repo.Create(ctx, domain.Person{ID: "1", SourcedID: "a"})
	_ = repo.Create(ctx, domain.Person{ID: "2", SourcedID: "b"})

	page, err := repo.List(ctx, port.ListParams{Offset: 1, Limit: math.MaxInt})
	require.NoError(t, err, "List")
	assert.Len(t, page.Items, 1)
	assert.Equal(t, 2, page.Total)
}

func TestPersonListStableForSameName(t *testing.T) {
	ctx := t.Context()
	repo := NewPersonRepository()
	for _, id := range []string{"c", "a", "b"} {
		_ = repo.Create(ctx, domain.Person{ID: id, SourcedID: id, GivenName: "Ada", FamilyName: "Lovelace"})
	}
	for range 20 {
		page, _ := repo.List(ctx, port.ListParams{})
		require.Len(t, page.Items, 3)
		ids := []string{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID}
		require.Equal(t, []string{"a", "b", "c"}, ids, "unstable order")
	}
}

func TestDocRepoUpdateRejectsForeignSourcedID(t *testing.T) {
	ctx := t.Context()
	repo := NewDocRepo[domain.Course]()
	a := &domain.Course{ID: "1", SourcedID: "a", Title: "A"}
	b := &domain.Course{ID: "2", SourcedID: "b", Title: "B"}
	_ = repo.Create(ctx, a)
	_ = repo.Create(ctx, b)

	steal := &domain.Course{ID: "2", SourcedID: "a", Title: "B"}
	require.ErrorIs(t, repo.Update(ctx, steal, nil), domain.ErrConflict)
	// The original owner must still be reachable by its business key.
	got, err := repo.GetBySourcedID(ctx, "a")
	require.NoError(t, err, "sourcedId index corrupted")
	assert.Equal(t, "1", got.GetID(), "sourcedId index corrupted")
}

// TestDocRepoUpdateMarshalFailurePreservesIndex checks that an Update which
// changes sourcedId but fails to serialize leaves the stored record and its
// sourcedId lookup untouched.
func TestDocRepoUpdateMarshalFailurePreservesIndex(t *testing.T) {
	ctx := t.Context()
	repo := NewDocRepo[domain.Course]()
	require.NoError(t, repo.Create(ctx, &domain.Course{ID: "1", SourcedID: "a", Title: "A"}))

	// Year 10000 is outside the RFC 3339 range, so json.Marshal fails.
	bad := &domain.Course{ID: "1", SourcedID: "b", UpdatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), Title: "B"}
	require.Error(t, repo.Update(ctx, bad, nil), "expected marshal error")

	got, err := repo.GetBySourcedID(ctx, "a")
	require.NoError(t, err, "failed update corrupted state")
	assert.Equal(t, "1", got.GetID(), "failed update corrupted state")
	assert.Equal(t, "A", got.Title, "failed update corrupted state")
	_, err = repo.GetBySourcedID(ctx, "b")
	assert.ErrorIs(t, err, domain.ErrNotFound, "new sourcedId must not be indexed after a failed update")
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
	for _, tc := range cases {
		lo, hi := pageBounds(tc.total, tc.off, tc.lim)
		assert.Equal(t, tc.lo, lo, "pageBounds(%d,%d,%d) lo", tc.total, tc.off, tc.lim)
		assert.Equal(t, tc.hi, hi, "pageBounds(%d,%d,%d) hi", tc.total, tc.off, tc.lim)
	}
}

// TestPersonRepositoryIsolatesSlices checks that neither the value passed to
// a write nor a value returned by a read shares slice storage with the
// repository, so mutating them cannot change stored data.
func TestPersonRepositoryIsolatesSlices(t *testing.T) {
	ctx := t.Context()
	repo := NewPersonRepository()
	person := domain.Person{
		ID: "1", SourcedID: "a",
		Roles:       []domain.Role{domain.RoleStudent},
		Identifiers: []domain.Identifier{{Type: "nin", Value: "x"}},
		Emails:      []domain.Contact{{Type: "work", Value: "a@x"}},
		Phones:      []domain.Contact{{Type: "work", Value: "1"}},
	}
	require.NoError(t, repo.Create(ctx, person))
	person.Roles[0], person.Identifiers[0].Value, person.Emails[0].Value, person.Phones[0].Value = domain.RoleStaff, "y", "b@x", "2"

	check := func(stage string) {
		t.Helper()
		got, err := repo.Get(ctx, "1")
		require.NoError(t, err)
		require.Equal(t, domain.RoleStudent, got.Roles[0], "%s: stored person changed", stage)
		require.Equal(t, "x", got.Identifiers[0].Value, "%s: stored person changed", stage)
		require.Equal(t, "a@x", got.Emails[0].Value, "%s: stored person changed", stage)
		require.Equal(t, "1", got.Phones[0].Value, "%s: stored person changed", stage)
	}
	check("after mutating Create argument")

	mutate := func(q domain.Person) {
		q.Roles[0], q.Identifiers[0].Value, q.Emails[0].Value, q.Phones[0].Value = domain.RoleGuest, "z", "c@x", "3"
	}
	got, _ := repo.Get(ctx, "1")
	mutate(got)
	check("after mutating Get result")
	got, _ = repo.GetBySourcedID(ctx, "a")
	mutate(got)
	check("after mutating GetBySourcedID result")
	page, _ := repo.List(ctx, port.ListParams{})
	require.NotEmpty(t, page.Items)
	mutate(page.Items[0])
	check("after mutating List result")

	upd, _ := repo.Get(ctx, "1")
	require.NoError(t, repo.Update(ctx, upd, nil))
	mutate(upd)
	check("after mutating Update argument")
}
