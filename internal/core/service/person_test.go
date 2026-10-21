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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingPublisher struct{ events []domain.Event }

func (pub *countingPublisher) Publish(_ context.Context, event domain.Event) error {
	pub.events = append(pub.events, event)
	return nil
}

func newSvc() (*service.Service, *countingPublisher) {
	pub := &countingPublisher{}
	return service.New(memory.NewPersonRepository(), pub, nil, nil), pub
}

func TestCreatePerson_Validation(t *testing.T) {
	svc, _ := newSvc()
	_, err := svc.CreatePerson(t.Context(), domain.Person{})
	assert.Error(t, err, "expected validation error for empty person")
}

func TestCreatePerson_Success(t *testing.T) {
	svc, pub := newSvc()
	person, err := svc.CreatePerson(t.Context(), domain.Person{
		GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, person.ID, "expected a generated id")
	assert.Equal(t, person.ID, person.SourcedID, "expected sourcedId to default to id")
	require.Len(t, pub.events, 1)
	assert.Equal(t, "person.created", pub.events[0].Type)
}

// TestUpsert_CreatesThenUpdates proves the idempotency the hub's non-HTTP
// ingress paths rely on: the same business key converges to one person.
func TestUpsert_CreatesThenUpdates(t *testing.T) {
	svc, _ := newSvc()
	ctx := t.Context()

	first, err := svc.UpsertPersonBySourcedID(ctx, domain.Person{
		SourcedID: "stu-1", GivenName: "Grace", FamilyName: "Hopper", Status: domain.StatusActive,
	})
	require.NoError(t, err, "first upsert")
	second, err := svc.UpsertPersonBySourcedID(ctx, domain.Person{
		SourcedID: "stu-1", GivenName: "Grace B.", FamilyName: "Hopper", Status: domain.StatusActive,
	})
	require.NoError(t, err, "second upsert")
	assert.Equal(t, first.ID, second.ID, "upsert must reuse id")

	page, err := svc.ListPersons(ctx, port.ListParams{Limit: 10})
	require.NoError(t, err, "list")
	assert.Equal(t, 1, page.Total, "expected 1 person after repeated upsert")
	require.NotEmpty(t, page.Items)
	assert.Equal(t, "Grace B.", page.Items[0].GivenName, "expected updated given name")
}

// racyPersons holds the first n GetBySourcedID calls until all n have
// arrived, so every concurrent upsert misses the lookup and tries to create.
// Later calls (the losers' re-read) pass straight through.
type racyPersons struct {
	*memory.PersonRepository
	gate    *sync.WaitGroup
	pending *atomic.Int32
}

func (repo racyPersons) GetBySourcedID(ctx context.Context, sid string) (domain.Person, error) {
	person, err := repo.PersonRepository.GetBySourcedID(ctx, sid)
	if repo.pending.Add(-1) >= 0 {
		repo.gate.Done()
		repo.gate.Wait()
	}
	return person, err
}

// interleavedPersons runs before once, just ahead of the first Update, to
// simulate a concurrent writer landing between update's read and write.
type interleavedPersons struct {
	*memory.PersonRepository
	before func()
}

func (repo *interleavedPersons) Update(ctx context.Context, person domain.Person, prev *time.Time) error {
	if before := repo.before; before != nil {
		repo.before = nil
		before()
	}
	return repo.PersonRepository.Update(ctx, person, prev)
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
	_, _, err := svc.UpsertPersonWithExpiry(ctx, person("A"), &exp)
	require.NoError(t, err)
	repo.before = func() {
		_, err := svc.UpsertPersonBySourcedID(ctx, person("Permanent"))
		assert.NoError(t, err, "concurrent permanent upsert")
	}
	later := now.Add(2 * time.Hour)
	_, _, err = svc.UpsertPersonWithExpiry(ctx, person("Late"), &later)
	require.NoError(t, err)
	stored, err := svc.GetPersonBySourcedID(ctx, "s")
	require.NoError(t, err)
	assert.Nil(t, stored.ExpiresAt, "want permanent")
	assert.Equal(t, "Late", stored.GivenName, "want the late data")
}

func TestUpsert_ConcurrentDeliveriesConverge(t *testing.T) {
	const count = 8
	var gate sync.WaitGroup
	gate.Add(count)
	var pending atomic.Int32
	pending.Store(count)
	repo := memory.NewPersonRepository()
	svc := service.New(racyPersons{repo, &gate, &pending}, nil, nil, nil)

	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			person, err := svc.UpsertPersonBySourcedID(t.Context(), domain.Person{
				SourcedID: "stu-race", GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive,
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- person.ID
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		assert.NoError(t, err, "upsert failed instead of converging")
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		} else {
			require.Equal(t, first, id, "deliveries diverged")
		}
	}
	page, err := repo.List(t.Context(), port.ListParams{})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total, "expected exactly one person")
}
