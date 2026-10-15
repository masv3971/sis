package postgres_test

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
)

// newStore connects to the database named by SIS_TEST_DATABASE_URL, skipping
// the test when it is unset so `go test ./...` stays hermetic.
func newStore(t *testing.T) *postgres.Service {
	t.Helper()
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	store, err := postgres.New(ctx, url, logger.NewWithWriter("test", false, os.Stderr))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })
	return store
}

func TestPersonRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Persons()

	p := domain.Person{
		ID: "test-" + time.Now().Format("150405.000000"), SourcedID: "src-" + time.Now().Format("150405.000000"),
		Status: domain.StatusActive, GivenName: "Ada", FamilyName: "Lovelace",
		Roles: []domain.Role{domain.RoleStudent}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, p.ID) })

	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, p); err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict on duplicate, got %v", err)
	}

	got, err := repo.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.GivenName != "Ada" || len(got.Roles) != 1 || got.Roles[0] != domain.RoleStudent {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	bySrc, err := repo.GetBySourcedID(ctx, p.SourcedID)
	if err != nil || bySrc.ID != p.ID {
		t.Fatalf("get by sourcedId: %v %+v", err, bySrc)
	}

	p.GivenName = "Augusta"
	p.UpdatedAt = time.Now().UTC()
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = repo.Get(ctx, p.ID)
	if got.GivenName != "Augusta" {
		t.Fatalf("expected updated name, got %q", got.GivenName)
	}

	page, err := repo.List(ctx, port.ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total < 1 {
		t.Fatalf("expected at least one person, got total=%d", page.Total)
	}

	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, p.ID); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestUpdateSourcedIDCollisionIsConflict(t *testing.T) {
	ctx := context.Background()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()

	persons := svc.Persons()
	a := domain.Person{ID: "pa-" + suffix, SourcedID: "sa-" + suffix, Status: domain.StatusActive, GivenName: "A", FamilyName: "A", CreatedAt: now, UpdatedAt: now}
	b := domain.Person{ID: "pb-" + suffix, SourcedID: "sb-" + suffix, Status: domain.StatusActive, GivenName: "B", FamilyName: "B", CreatedAt: now, UpdatedAt: now}
	t.Cleanup(func() { _ = persons.Delete(ctx, a.ID); _ = persons.Delete(ctx, b.ID) })
	if err := persons.Create(ctx, a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := persons.Create(ctx, b); err != nil {
		t.Fatalf("create b: %v", err)
	}
	b.SourcedID = a.SourcedID
	if err := persons.Update(ctx, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("person update: expected ErrConflict, got %v", err)
	}

	courses := postgres.NewDocRepo[domain.Course](svc)
	ca := &domain.Course{Base: domain.Base{ID: "ca-" + suffix, SourcedID: "csa-" + suffix, CreatedAt: now, UpdatedAt: now}, Title: "A"}
	cb := &domain.Course{Base: domain.Base{ID: "cb-" + suffix, SourcedID: "csb-" + suffix, CreatedAt: now, UpdatedAt: now}, Title: "B"}
	t.Cleanup(func() { _ = courses.Delete(ctx, ca.ID); _ = courses.Delete(ctx, cb.ID) })
	if err := courses.Create(ctx, ca); err != nil {
		t.Fatalf("create ca: %v", err)
	}
	if err := courses.Create(ctx, cb); err != nil {
		t.Fatalf("create cb: %v", err)
	}
	cb.SourcedID = ca.SourcedID
	if err := courses.Update(ctx, cb); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("doc update: expected ErrConflict, got %v", err)
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
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			svc, err := postgres.New(ctx, url, logger.NewWithWriter("test", false, io.Discard))
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
		t.Errorf("replica startup failed: %v", err)
	}
}
