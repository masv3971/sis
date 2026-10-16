package memory

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func at(t time.Time) *time.Time { return &t }

// TestDocRepoExpiry checks an expired entity disappears from every read,
// cannot be updated or deleted, is replaced by a Create holding its keys, and
// is removed by DeleteExpired.
func TestDocRepoExpiry(t *testing.T) {
	ctx := t.Context()
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := NewDocRepo[domain.Course]().WithClock(c.now)
	exp := c.t.Add(time.Hour)
	mustNoErr(t, r.Create(ctx, &domain.Course{Base: domain.Base{ID: "1", SourcedID: "a", ExpiresAt: &exp}, Title: "A"}))
	mustNoErr(t, r.Create(ctx, &domain.Course{Base: domain.Base{ID: "2", SourcedID: "b"}, Title: "B"}))

	if _, err := r.GetBySourcedID(ctx, "a"); err != nil {
		t.Fatalf("live record must be readable: %v", err)
	}

	c.t = exp // inclusive boundary
	assertGone(ctx, t, r, "1", "a")
	page, err := r.List(ctx, port.ListParams{})
	mustNoErr(t, err)
	if page.Total != 1 || page.Items[0].GetID() != "2" {
		t.Fatalf("List must skip expired: %+v", page)
	}
	if err := r.Update(ctx, &domain.Course{Base: domain.Base{ID: "1", SourcedID: "a"}, Title: "x"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update of expired: %v", err)
	}
	if err := r.Delete(ctx, "1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete of expired: %v", err)
	}

	// A new record may take the expired one's sourcedId.
	mustNoErr(t, r.Create(ctx, &domain.Course{Base: domain.Base{ID: "3", SourcedID: "a"}, Title: "A2"}))
	got, err := r.GetBySourcedID(ctx, "a")
	if err != nil || got.GetID() != "3" {
		t.Fatalf("replacement not found: %+v %v", got, err)
	}
	if _, ok := r.byID["1"]; ok {
		t.Fatal("expired record must be dropped when its sourcedId is reused")
	}

	// DeleteExpired reports exactly the expired ids.
	exp2 := c.t.Add(time.Minute)
	mustNoErr(t, r.Create(ctx, &domain.Course{Base: domain.Base{ID: "4", SourcedID: "d", ExpiresAt: &exp2}, Title: "D"}))
	ids, err := r.DeleteExpired(ctx, exp2)
	mustNoErr(t, err)
	if !slices.Equal(ids, []string{"4"}) {
		t.Fatalf("DeleteExpired = %v", ids)
	}
	if _, ok := r.sourcedToID["d"]; ok {
		t.Fatal("sourcedId index must be cleaned")
	}
}

func TestPersonRepoExpiry(t *testing.T) {
	ctx := t.Context()
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := NewPersonRepository().WithClock(c.now)
	mustNoErr(t, r.Create(ctx, domain.Person{ID: "1", SourcedID: "a", ExpiresAt: at(c.t.Add(time.Hour))}))
	mustNoErr(t, r.Create(ctx, domain.Person{ID: "2", SourcedID: "b"}))

	c.t = c.t.Add(2 * time.Hour)
	if _, err := r.Get(ctx, "1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get expired: %v", err)
	}
	if _, err := r.GetBySourcedID(ctx, "a"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetBySourcedID expired: %v", err)
	}
	if ps, _ := r.GetByIDs(ctx, []string{"1", "2"}); len(ps) != 1 {
		t.Fatalf("GetByIDs must skip expired: %v", ps)
	}
	if page, _ := r.List(ctx, port.ListParams{}); page.Total != 1 {
		t.Fatalf("List must skip expired: %+v", page)
	}
	mustNoErr(t, r.Create(ctx, domain.Person{ID: "3", SourcedID: "a"}))
	if got, err := r.GetBySourcedID(ctx, "a"); err != nil || got.ID != "3" {
		t.Fatalf("replacement: %+v %v", got, err)
	}
	ids, err := r.DeleteExpired(ctx, c.t)
	mustNoErr(t, err)
	if len(ids) != 0 {
		t.Fatalf("expired record was already replaced, got %v", ids)
	}
}

func assertGone(ctx context.Context, t *testing.T, r *DocRepo[domain.Course, *domain.Course], id, sid string) {
	t.Helper()
	if _, err := r.Get(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get expired: %v", err)
	}
	if _, err := r.GetBySourcedID(ctx, sid); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetBySourcedID expired: %v", err)
	}
	if es, _ := r.GetByIDs(ctx, []string{id}); len(es) != 0 {
		t.Fatalf("GetByIDs expired: %v", es)
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
