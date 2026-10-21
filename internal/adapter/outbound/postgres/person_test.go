package postgres_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// pgCfg is a config with only the postgres settings, as the defaults would
// set them.
func pgCfg(url string) *model.Cfg {
	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Outbound.Postgres = model.Postgres{ConnectionString: url, ConnectTimeout: 10 * time.Second, StatementTimeout: 30 * time.Second}
	return cfg
}

// newStore connects to the database named by SIS_TEST_DATABASE_URL, skipping
// the test when it is unset so `go test ./...` stays hermetic.
func newStore(t *testing.T) *postgres.Service {
	t.Helper()
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	store, err := postgres.New(ctx, pgCfg(url), logger.NewWithWriter("test", false, "debug", os.Stderr))
	require.NoError(t, err, "connect")
	t.Cleanup(func() { _ = store.Close(context.Background()) })
	return store
}

func TestPersonRepository_CRUD(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	repo := svc.Persons()

	person := domain.Person{
		ID: "test-" + time.Now().Format("150405.000000"), SourcedID: "src-" + time.Now().Format("150405.000000"),
		Status: domain.StatusActive, GivenName: "Ada", FamilyName: "Lovelace",
		Roles: []domain.Role{domain.RoleStudent}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	t.Cleanup(func() { _ = repo.Delete(context.Background(), person.ID) })

	require.NoError(t, repo.Create(ctx, person), "create")
	assert.ErrorIs(t, repo.Create(ctx, person), domain.ErrConflict, "duplicate create")

	got, err := repo.Get(ctx, person.ID)
	require.NoError(t, err, "get")
	assert.Equal(t, "Ada", got.GivenName, "round-trip")
	assert.Equal(t, []domain.Role{domain.RoleStudent}, got.Roles, "round-trip")

	bySrc, err := repo.GetBySourcedID(ctx, person.SourcedID)
	require.NoError(t, err, "get by sourcedId")
	assert.Equal(t, person.ID, bySrc.ID, "get by sourcedId")

	person.GivenName = "Augusta"
	person.UpdatedAt = time.Now().UTC()
	require.NoError(t, repo.Update(ctx, person, nil), "update")
	got, _ = repo.Get(ctx, person.ID)
	assert.Equal(t, "Augusta", got.GivenName, "updated name")

	page, err := repo.List(ctx, port.ListParams{Limit: 10})
	require.NoError(t, err, "list")
	assert.Positive(t, page.Total, "expected at least one person")
	// Past the last page: no rows carry the window count, the total must remain.
	past, err := repo.List(ctx, port.ListParams{Offset: page.Total, Limit: 10})
	require.NoError(t, err, "list past the end")
	assert.Empty(t, past.Items, "list past the end")
	assert.Equal(t, page.Total, past.Total, "list past the end")
	courses := postgres.NewDocRepo[domain.Course](svc)
	coursePage, err := courses.List(ctx, port.ListParams{Limit: 10})
	require.NoError(t, err, "courses list")
	coursePast, err := courses.List(ctx, port.ListParams{Offset: coursePage.Total + 1, Limit: 10})
	require.NoError(t, err, "courses list past the end")
	assert.Equal(t, coursePage.Total, coursePast.Total, "courses list past the end")

	require.NoError(t, repo.Delete(ctx, person.ID), "delete")
	_, err = repo.Get(ctx, person.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound, "get after delete")
}

func TestUpdateSourcedIDCollisionIsConflict(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()

	persons := svc.Persons()
	a := domain.Person{ID: "pa-" + suffix, SourcedID: "sa-" + suffix, Status: domain.StatusActive, GivenName: "A", FamilyName: "A", CreatedAt: now, UpdatedAt: now}
	b := domain.Person{ID: "pb-" + suffix, SourcedID: "sb-" + suffix, Status: domain.StatusActive, GivenName: "B", FamilyName: "B", CreatedAt: now, UpdatedAt: now}
	t.Cleanup(func() {
		_ = persons.Delete(context.Background(), a.ID)
		_ = persons.Delete(context.Background(), b.ID)
	})
	require.NoError(t, persons.Create(ctx, a), "create a")
	require.NoError(t, persons.Create(ctx, b), "create b")
	b.SourcedID = a.SourcedID
	assert.ErrorIs(t, persons.Update(ctx, b, nil), domain.ErrConflict, "person update")

	courses := postgres.NewDocRepo[domain.Course](svc)
	ca := &domain.Course{ID: "ca-" + suffix, SourcedID: "csa-" + suffix, CreatedAt: now, UpdatedAt: now, Title: "A"}
	cb := &domain.Course{ID: "cb-" + suffix, SourcedID: "csb-" + suffix, CreatedAt: now, UpdatedAt: now, Title: "B"}
	t.Cleanup(func() {
		_ = courses.Delete(context.Background(), ca.ID)
		_ = courses.Delete(context.Background(), cb.ID)
	})
	require.NoError(t, courses.Create(ctx, ca), "create ca")
	require.NoError(t, courses.Create(ctx, cb), "create cb")
	cb.SourcedID = ca.SourcedID
	assert.ErrorIs(t, courses.Update(ctx, cb, nil), domain.ErrConflict, "doc update")
}

// TestGetByIDs checks the bulk lookups used for reference resolution return
// exactly the matching rows, skip unknown keys, and match by id or sourcedId
// only as asked.
func TestGetByIDs(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()

	persons := svc.Persons()
	a := domain.Person{ID: "ga-" + suffix, SourcedID: "gsa-" + suffix, Status: domain.StatusActive, GivenName: "A", FamilyName: "A", CreatedAt: now, UpdatedAt: now}
	b := domain.Person{ID: "gb-" + suffix, SourcedID: "gsb-" + suffix, Status: domain.StatusActive, GivenName: "B", FamilyName: "B", CreatedAt: now, UpdatedAt: now}
	t.Cleanup(func() {
		_ = persons.Delete(context.Background(), a.ID)
		_ = persons.Delete(context.Background(), b.ID)
	})
	for _, person := range []domain.Person{a, b} {
		require.NoError(t, persons.Create(ctx, person), "create %s", person.ID)
	}
	got, err := persons.GetByIDs(ctx, []string{a.ID, b.ID, "missing", b.SourcedID})
	require.NoError(t, err, "persons GetByIDs")
	assert.Len(t, got, 2, "persons GetByIDs")
	empty, err := persons.GetByIDs(ctx, nil)
	require.NoError(t, err, "persons GetByIDs(nil)")
	assert.Empty(t, empty, "persons GetByIDs(nil)")
	bySID, err := persons.GetBySourcedIDs(ctx, []string{a.SourcedID, "missing", b.ID})
	require.NoError(t, err, "persons GetBySourcedIDs")
	require.Len(t, bySID, 1, "persons GetBySourcedIDs")
	assert.Equal(t, a.ID, bySID[0].ID, "persons GetBySourcedIDs")

	courses := postgres.NewDocRepo[domain.Course](svc)
	ca := &domain.Course{ID: "gca-" + suffix, SourcedID: "gcsa-" + suffix, CreatedAt: now, UpdatedAt: now, Title: "A"}
	t.Cleanup(func() { _ = courses.Delete(context.Background(), ca.ID) })
	require.NoError(t, courses.Create(ctx, ca), "create course")
	gotCourses, err := courses.GetByIDs(ctx, []string{ca.ID, ca.SourcedID, "missing"})
	require.NoError(t, err, "courses GetByIDs")
	require.Len(t, gotCourses, 1, "courses GetByIDs")
	assert.Equal(t, ca.ID, gotCourses[0].ID, "courses GetByIDs")
	gotCourses, err = courses.GetBySourcedIDs(ctx, []string{ca.SourcedID, ca.ID, "missing"})
	require.NoError(t, err, "courses GetBySourcedIDs")
	require.Len(t, gotCourses, 1, "courses GetBySourcedIDs")
	assert.Equal(t, ca.ID, gotCourses[0].ID, "courses GetBySourcedIDs")
}

// TestListTotalMatchesPageUnderInserts lists just past the end while rows
// are inserted: an empty page must never report a total beyond its offset,
// and a non-empty one never a total at or below it.
func TestListTotalMatchesPageUnderInserts(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	persons := svc.Persons()
	courses := postgres.NewDocRepo[domain.Course](svc)

	const inserts = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range inserts {
			now := time.Now().UTC()
			id := fmt.Sprintf("lt-%s-%03d", suffix, i)
			person := domain.Person{ID: id, SourcedID: "s" + id, Status: domain.StatusActive, GivenName: "L", FamilyName: "L", CreatedAt: now, UpdatedAt: now}
			course := &domain.Course{ID: id, SourcedID: "s" + id, CreatedAt: now, UpdatedAt: now, Title: "L"}
			_ = persons.Create(ctx, person)
			_ = courses.Create(ctx, course)
		}
	}()
	t.Cleanup(func() {
		for i := range inserts {
			id := fmt.Sprintf("lt-%s-%03d", suffix, i)
			_ = persons.Delete(context.Background(), id)
			_ = courses.Delete(context.Background(), id)
		}
	})

	check := func(name string, offset, items, total int) {
		t.Helper()
		require.Equal(t, total <= offset, items == 0, "%s at offset %d: %d items with total %d", name, offset, items, total)
	}
	var personOffset, courseOffset int
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		personPage, err := persons.List(ctx, port.ListParams{Offset: personOffset, Limit: 1})
		require.NoError(t, err, "persons list")
		check("persons", personOffset, len(personPage.Items), personPage.Total)
		personOffset = personPage.Total
		coursePage, err := courses.List(ctx, port.ListParams{Offset: courseOffset, Limit: 1})
		require.NoError(t, err, "courses list")
		check("courses", courseOffset, len(coursePage.Items), coursePage.Total)
		courseOffset = coursePage.Total
	}
}

// TestConcurrentStartupMigrates simulates several replicas booting at once;
// the advisory lock must let every one of them succeed.
func TestConcurrentStartupMigrates(t *testing.T) {
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	const replicas = 4
	var wg sync.WaitGroup
	errs := make(chan error, replicas)
	for range replicas {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			svc, err := postgres.New(ctx, pgCfg(url), logger.NewWithWriter("test", false, "info", io.Discard))
			if err != nil {
				errs <- err
				return
			}
			_ = svc.Close(ctx)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err, "replica startup failed")
	}
}
