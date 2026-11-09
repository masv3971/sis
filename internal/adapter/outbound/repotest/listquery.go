// Package repotest holds contract tests that every repository backend must
// pass, so the in-memory and Postgres repositories cannot drift apart.
package repotest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// ListQueryDocs checks sort and filter on a document repository, using
// courses. The database may hold other courses, so every query first narrows
// to this run's records with a title filter.
func ListQueryDocs(t *testing.T, repo port.Repository[domain.Course, *domain.Course]) {
	t.Helper()
	ctx := t.Context()
	mark := fmt.Sprintf("-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	course := func(id, title, code string) *domain.Course {
		return &domain.Course{Base: domain.Base{ID: id + mark, SourcedID: id + mark, CreatedAt: now, UpdatedAt: now}, Title: title + mark, Code: code}
	}
	records := []*domain.Course{
		course("c1", "Beta", "Z9"),
		course("c2", "alpha", "a1"),
		course("c3", "Gamma", ""), // no code: the key is absent
		course("c4", "Delta", "B2"),
	}
	t.Cleanup(func() {
		// t.Context() is already canceled when cleanups run.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, r := range records {
			assert.NoError(t, repo.Delete(cleanup, r.ID))
		}
	})
	for _, r := range records {
		require.NoError(t, repo.Create(ctx, r))
	}
	mine := port.FilterTerm{Field: "title", Op: port.FilterContains, Value: mark}

	list := func(params port.ListParams) ([]string, int) {
		t.Helper()
		// An OR filter cannot be narrowed by AND-ing the marker, so those
		// tests put the marker in each term's value instead.
		switch {
		case params.Filter == nil:
			params.Filter = &port.Filter{Terms: []port.FilterTerm{mine}}
		case !params.Filter.Any:
			params.Filter = &port.Filter{Terms: append([]port.FilterTerm{mine}, params.Filter.Terms...)}
		}
		page, err := repo.List(ctx, params)
		require.NoError(t, err)
		var titles []string
		for _, e := range page.Items {
			titles = append(titles, e.Title[:len(e.Title)-len(mark)])
		}
		return titles, page.Total
	}
	term := func(field string, op port.FilterOp, value string) port.FilterTerm {
		return port.FilterTerm{Field: field, Op: op, Value: value}
	}

	t.Run("default order is sourcedId", func(t *testing.T) {
		got, total := list(port.ListParams{})
		assert.Equal(t, []string{"Beta", "alpha", "Gamma", "Delta"}, got)
		assert.Equal(t, 4, total)
	})
	t.Run("sort by title compares bytes", func(t *testing.T) {
		got, _ := list(port.ListParams{Sort: "title"})
		assert.Equal(t, []string{"Beta", "Delta", "Gamma", "alpha"}, got, "uppercase sorts before lowercase")
		got, _ = list(port.ListParams{Sort: "title", Desc: true})
		assert.Equal(t, []string{"alpha", "Gamma", "Delta", "Beta"}, got)
	})
	t.Run("records without the key sort last either way", func(t *testing.T) {
		got, _ := list(port.ListParams{Sort: "code"})
		assert.Equal(t, []string{"Delta", "Beta", "alpha", "Gamma"}, got)
		got, _ = list(port.ListParams{Sort: "code", Desc: true})
		assert.Equal(t, []string{"alpha", "Beta", "Delta", "Gamma"}, got)
	})
	t.Run("sort pages consistently", func(t *testing.T) {
		got, total := list(port.ListParams{Sort: "title", Limit: 2, Offset: 1})
		assert.Equal(t, []string{"Delta", "Gamma"}, got)
		assert.Equal(t, 4, total, "total ignores the page")
	})
	t.Run("equals ignores case", func(t *testing.T) {
		got, total := list(port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{term("title", port.FilterEquals, "ALPHA"+mark)}}})
		assert.Equal(t, []string{"alpha"}, got)
		assert.Equal(t, 1, total)
	})
	t.Run("contains", func(t *testing.T) {
		got, _ := list(port.ListParams{Sort: "title", Filter: &port.Filter{Terms: []port.FilterTerm{term("title", port.FilterContains, "ta"+mark)}}})
		assert.Equal(t, []string{"Beta", "Delta"}, got)
	})
	t.Run("not equals never matches an absent key", func(t *testing.T) {
		got, total := list(port.ListParams{Sort: "title", Filter: &port.Filter{Terms: []port.FilterTerm{term("code", port.FilterNotEquals, "z9")}}})
		assert.Equal(t, []string{"Delta", "alpha"}, got, "Gamma has no code")
		assert.Equal(t, 2, total)
	})
	t.Run("and", func(t *testing.T) {
		got, _ := list(port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{term("title", port.FilterContains, "ta"), term("code", port.FilterEquals, "b2")}}})
		assert.Equal(t, []string{"Delta"}, got)
	})
	t.Run("or", func(t *testing.T) {
		got, _ := list(port.ListParams{Sort: "title", Filter: &port.Filter{Any: true, Terms: []port.FilterTerm{
			term("title", port.FilterEquals, "Beta"+mark), term("title", port.FilterEquals, "Gamma"+mark),
		}}})
		assert.Equal(t, []string{"Beta", "Gamma"}, got)
	})
	t.Run("a value is data, not SQL", func(t *testing.T) {
		got, total := list(port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{term("title", port.FilterEquals, "x' OR '1'='1")}}})
		assert.Empty(t, got)
		assert.Zero(t, total)
	})
	t.Run("a key that could alter the SQL is refused", func(t *testing.T) {
		_, err := repo.List(ctx, port.ListParams{Sort: "title') OR 1=1 --"})
		assert.Error(t, err)
		_, err = repo.List(ctx, port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{term("title') OR 1=1 --", port.FilterEquals, "x")}}})
		assert.Error(t, err)
	})
	t.Run("filter applies to ListByRef too", func(t *testing.T) {
		page, err := repo.ListByRef(ctx, "orgId", "none", port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{mine}}})
		require.NoError(t, err)
		assert.Zero(t, page.Total)
	})
}

// ListQueryPersons checks sort and filter on a person repository. The
// database may hold other persons, so every query first narrows to this run's
// records with a family-name filter.
func ListQueryPersons(t *testing.T, repo port.PersonRepository) {
	t.Helper()
	ctx := t.Context()
	mark := fmt.Sprintf("-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	person := func(id, given, family string, roles ...domain.Role) domain.Person {
		return domain.Person{
			ID: id + mark, SourcedID: id + mark, Status: domain.StatusActive,
			GivenName: given, FamilyName: family + mark, Roles: roles, CreatedAt: now, UpdatedAt: now,
		}
	}
	records := []domain.Person{
		person("p1", "Zed", "Berg", domain.RoleStudent),
		person("p2", "amy", "Ahl", domain.RoleStudent),
		person("p3", "Bo", "Cole", domain.RoleStaff),
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, p := range records {
			assert.NoError(t, repo.Delete(cleanup, p.ID))
		}
	})
	for _, p := range records {
		require.NoError(t, repo.Create(ctx, p))
	}
	mine := port.FilterTerm{Field: "familyName", Op: port.FilterContains, Value: mark}
	given := func(items []domain.Person) []string {
		var out []string
		for _, p := range items {
			out = append(out, p.GivenName)
		}
		return out
	}

	t.Run("default order is by name", func(t *testing.T) {
		page, err := repo.List(ctx, port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{mine}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"amy", "Zed", "Bo"}, given(page.Items))
	})
	t.Run("sort by given name compares bytes, either direction", func(t *testing.T) {
		page, err := repo.List(ctx, port.ListParams{Sort: "givenName", Filter: &port.Filter{Terms: []port.FilterTerm{mine}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"Bo", "Zed", "amy"}, given(page.Items))
		page, err = repo.List(ctx, port.ListParams{Sort: "givenName", Desc: true, Filter: &port.Filter{Terms: []port.FilterTerm{mine}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"amy", "Zed", "Bo"}, given(page.Items))
	})
	t.Run("filter ignores case and totals the matches", func(t *testing.T) {
		page, err := repo.List(ctx, port.ListParams{Limit: 1, Filter: &port.Filter{Terms: []port.FilterTerm{mine, {Field: "givenName", Op: port.FilterEquals, Value: "AMY"}}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"amy"}, given(page.Items))
		assert.Equal(t, 1, page.Total)
	})
	t.Run("a filtered page past the end keeps the total", func(t *testing.T) {
		page, err := repo.List(ctx, port.ListParams{Offset: 10, Limit: 5, Filter: &port.Filter{Terms: []port.FilterTerm{mine}}})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		assert.Equal(t, 3, page.Total)
	})
	t.Run("filter and sort apply with a role filter", func(t *testing.T) {
		page, err := repo.ListByRoles(ctx, []domain.Role{domain.RoleStudent}, port.ListParams{Sort: "givenName", Filter: &port.Filter{Terms: []port.FilterTerm{mine}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"Zed", "amy"}, given(page.Items))
		assert.Equal(t, 2, page.Total)
	})
	t.Run("a key that is not a person column is refused", func(t *testing.T) {
		_, err := repo.List(ctx, port.ListParams{Sort: "roles"})
		assert.Error(t, err)
		_, err = repo.List(ctx, port.ListParams{Filter: &port.Filter{Terms: []port.FilterTerm{{Field: "givenName') OR 1=1 --", Op: port.FilterEquals, Value: "x"}}}})
		assert.Error(t, err)
	})
}
