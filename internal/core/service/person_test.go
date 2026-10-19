package service_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
)

type countingPublisher struct{ events []domain.Event }

func (c *countingPublisher) Publish(_ context.Context, e domain.Event) error {
	c.events = append(c.events, e)
	return nil
}

func newSvc() (*service.Service, *countingPublisher) {
	pub := &countingPublisher{}
	return service.New(memory.NewPersonRepository(), pub, nil, nil), pub
}

func TestCreatePerson_Validation(t *testing.T) {
	svc, _ := newSvc()
	if _, err := svc.CreatePerson(context.Background(), domain.Person{}); err == nil {
		t.Fatal("expected validation error for empty person")
	}
}

func TestCreatePerson_Success(t *testing.T) {
	svc, pub := newSvc()
	p, err := svc.CreatePerson(context.Background(), domain.Person{
		GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected a generated id")
	}
	if p.SourcedID != p.ID {
		t.Fatalf("expected sourcedId to default to id, got %q", p.SourcedID)
	}
	if len(pub.events) != 1 || pub.events[0].Type != "person.created" {
		t.Fatalf("expected a person.created event, got %+v", pub.events)
	}
}

// TestUpsert_CreatesThenUpdates proves the idempotency the hub's non-HTTP
// ingress paths rely on: the same business key converges to one person.
func TestUpsert_CreatesThenUpdates(t *testing.T) {
	svc, _ := newSvc()
	ctx := context.Background()

	first, err := svc.UpsertPersonBySourcedID(ctx, domain.Person{
		SourcedID: "stu-1", GivenName: "Grace", FamilyName: "Hopper", Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := svc.UpsertPersonBySourcedID(ctx, domain.Person{
		SourcedID: "stu-1", GivenName: "Grace B.", FamilyName: "Hopper", Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("upsert must reuse id: %s != %s", first.ID, second.ID)
	}

	page, err := svc.ListPersons(ctx, port.ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected 1 person after repeated upsert, got %d", page.Total)
	}
	if page.Items[0].GivenName != "Grace B." {
		t.Fatalf("expected updated given name, got %q", page.Items[0].GivenName)
	}
}

// racyPersons holds the first n GetBySourcedID calls until all n have
// arrived, so every concurrent upsert misses the lookup and tries to create.
// Later calls (the losers' re-read) pass straight through.
type racyPersons struct {
	*memory.PersonRepository
	gate    *sync.WaitGroup
	pending *atomic.Int32
}

func (r racyPersons) GetBySourcedID(ctx context.Context, sid string) (domain.Person, error) {
	p, err := r.PersonRepository.GetBySourcedID(ctx, sid)
	if r.pending.Add(-1) >= 0 {
		r.gate.Done()
		r.gate.Wait()
	}
	return p, err
}

// interleavedPersons runs before once, just ahead of the first Update, to
// simulate a concurrent writer landing between update's read and write.
type interleavedPersons struct {
	*memory.PersonRepository
	before func()
}

func (r *interleavedPersons) Update(ctx context.Context, p domain.Person, prev *time.Time) error {
	if f := r.before; f != nil {
		r.before = nil
		f()
	}
	return r.PersonRepository.Update(ctx, p, prev)
}

// TestUpsertPersonWithExpiryRace: a TTL upsert that loses a race to a no-TTL
// upsert must not make the now-permanent person expire again.
func TestUpsertPersonWithExpiryRace(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	repo := &interleavedPersons{PersonRepository: memory.NewPersonRepository().WithClock(clock)}
	svc := service.New(repo, nil, clock, nil)
	person := func(given string) domain.Person {
		return domain.Person{SourcedID: "s", GivenName: given, FamilyName: "F", Status: domain.StatusActive}
	}

	exp := now.Add(time.Hour)
	if _, _, err := svc.UpsertPersonWithExpiry(ctx, person("A"), &exp); err != nil {
		t.Fatal(err)
	}
	repo.before = func() {
		if _, err := svc.UpsertPersonBySourcedID(ctx, person("Permanent")); err != nil {
			t.Errorf("concurrent permanent upsert: %v", err)
		}
	}
	later := now.Add(2 * time.Hour)
	if _, _, err := svc.UpsertPersonWithExpiry(ctx, person("Late"), &later); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.GetPersonBySourcedID(ctx, "s")
	if err != nil || stored.ExpiresAt != nil || stored.GivenName != "Late" {
		t.Fatalf("stored = %+v, %v; want permanent with the late data", stored, err)
	}
}

func TestUpsert_ConcurrentDeliveriesConverge(t *testing.T) {
	const n = 8
	var gate sync.WaitGroup
	gate.Add(n)
	var pending atomic.Int32
	pending.Store(n)
	repo := memory.NewPersonRepository()
	svc := service.New(racyPersons{repo, &gate, &pending}, nil, nil, nil)

	ids := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			p, err := svc.UpsertPersonBySourcedID(context.Background(), domain.Person{
				SourcedID: "stu-race", GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive,
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- p.ID
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Errorf("upsert failed instead of converging: %v", err)
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("deliveries diverged: %s vs %s", first, id)
		}
	}
	page, _ := repo.List(context.Background(), port.ListParams{})
	if page.Total != 1 {
		t.Fatalf("expected exactly one person, got %d", page.Total)
	}
}
