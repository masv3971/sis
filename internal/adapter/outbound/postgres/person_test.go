package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"log/slog"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// newStore connects to the database named by SIS_TEST_DATABASE_URL, skipping
// the test when it is unset so `go test ./...` stays hermetic.
func newStore(t *testing.T) *postgres.Store {
	t.Helper()
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	store, err := postgres.New(ctx, url, slog.New(slog.NewTextHandler(os.Stderr, nil)))
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
