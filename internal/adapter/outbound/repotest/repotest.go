// Package repotest holds behaviour tests every implementation of the
// outbound repository ports must pass, so the memory and Postgres
// repositories are checked against the same expectations.
package repotest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// PersonSearch checks port.PersonRepository.Search. prefix is put in front
// of every id, sourcedId and name, so runs against a shared database only
// see their own rows (searches always include it as a term).
func PersonSearch(t *testing.T, repo port.PersonRepository, prefix string) {
	ctx := t.Context()
	now := time.Now().UTC()
	tag := "zq" + strings.ToLower(prefix) // a term only this run's rows hold
	person := func(id, given, family string, ids []domain.Identifier, emails []domain.Contact) domain.Person {
		return domain.Person{
			ID: tag + id, SourcedID: tag + "s-" + id, Status: domain.StatusActive,
			GivenName: given, FamilyName: family + " " + tag,
			Identifiers: ids, Emails: emails, CreatedAt: now, UpdatedAt: now,
		}
	}
	persons := []domain.Person{
		person("1", "Anna", "Andersson", []domain.Identifier{{Type: "studentNumber", Value: "LIU-0001"}}, []domain.Contact{{Type: "work", Value: "anna@liu.se"}}),
		person("2", "Johan", "Berg", nil, []domain.Contact{{Type: "work", Value: "johan@kth.se"}}),
		person("3", "Anna-Karin", "Ek", nil, nil),
		// Sorts last under both byte order and a linguistic collation.
		person("4", "Under_score", "Zz 100%", nil, nil),
	}
	t.Cleanup(func() {
		for _, p := range persons {
			_ = repo.Delete(context.Background(), p.ID)
		}
	})
	for _, p := range persons {
		require.NoError(t, repo.Create(ctx, p), "create %s", p.ID)
	}

	search := func(terms ...string) []string {
		t.Helper()
		page, err := repo.Search(ctx, append([]string{tag}, terms...), port.ListParams{Limit: 10})
		require.NoError(t, err, "search %v", terms)
		ids := make([]string, 0, len(page.Items))
		for _, p := range page.Items {
			ids = append(ids, strings.TrimPrefix(p.ID, tag))
		}
		assert.Len(t, ids, page.Total, "search %v: total", terms)
		return ids
	}

	assert.Equal(t, []string{"1", "2", "3", "4"}, search(), "only the tag: everyone, ordered by family name")
	assert.Equal(t, []string{"1", "3"}, search("anna"), "given name, case-insensitive")
	assert.Equal(t, []string{"2"}, search("BERG"), "family name")
	assert.Equal(t, []string{"1"}, search("liu-0001"), "identifier value")
	assert.Equal(t, []string{"2"}, search("kth.se"), "email value")
	assert.Equal(t, []string{"3"}, search("anna", "ek"), "every term must match")
	assert.Equal(t, []string{"2"}, search(tag+"s-2"), "sourcedId")
	assert.Empty(t, search("nobody"), "no match")
	assert.Equal(t, []string{"4"}, search("_"), "LIKE wildcards match literally")
	assert.Equal(t, []string{"4"}, search("%"), "LIKE wildcards match literally")

	page, err := repo.Search(ctx, []string{tag}, port.ListParams{Limit: 2, Offset: 1})
	require.NoError(t, err, "paged search")
	assert.Equal(t, 4, page.Total, "paged search: total")
	require.Len(t, page.Items, 2, "paged search")
	assert.Equal(t, tag+"2", page.Items[0].ID, "paged search")

	exp := now.Add(-time.Minute)
	expired := person("5", "Anna", "Gone", nil, nil)
	expired.ExpiresAt = &exp
	persons = append(persons, expired)
	require.NoError(t, repo.Create(ctx, expired), "create expired")
	assert.Equal(t, []string{"1", "3"}, search("anna"), "expired persons are not found")
}

// ListByRef checks port.Repository.ListByRef on enrollments. prefix keeps
// the ids of runs against a shared database apart.
func ListByRef(t *testing.T, repo port.Repository[domain.Enrollment, *domain.Enrollment], prefix string) {
	ctx := t.Context()
	now := time.Now().UTC()
	p := prefix + "ref-"
	enrollment := func(id, person string) *domain.Enrollment {
		return &domain.Enrollment{
			Base:     domain.Base{ID: p + id, SourcedID: p + "s-" + id, CreatedAt: now, UpdatedAt: now},
			PersonID: p + person, OfferingID: "o",
		}
	}
	enrollments := []*domain.Enrollment{enrollment("1", "anna"), enrollment("2", "johan"), enrollment("3", "anna")}
	t.Cleanup(func() {
		for _, e := range enrollments {
			_ = repo.Delete(context.Background(), e.ID)
		}
	})
	for _, e := range enrollments {
		require.NoError(t, repo.Create(ctx, e), "create %s", e.ID)
	}

	page, err := repo.ListByRef(ctx, "personId", p+"anna", port.ListParams{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Items, 2)
	assert.Equal(t, p+"1", page.Items[0].ID, "ordered by sourcedId")
	assert.Equal(t, p+"3", page.Items[1].ID, "ordered by sourcedId")

	page, err = repo.ListByRef(ctx, "personId", p+"anna", port.ListParams{Limit: 1, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, page.Total, "paged: total")
	require.Len(t, page.Items, 1, "paged")
	assert.Equal(t, p+"3", page.Items[0].ID, "paged")

	page, err = repo.ListByRef(ctx, "personId", p+"nobody", port.ListParams{Limit: 10})
	require.NoError(t, err)
	assert.Zero(t, page.Total, "no match")
	assert.Empty(t, page.Items, "no match")

	page, err = repo.ListByRef(ctx, "offeringId", p+"anna", port.ListParams{Limit: 10})
	require.NoError(t, err)
	assert.Zero(t, page.Total, "another field does not match")
}
