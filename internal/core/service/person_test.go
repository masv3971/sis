package service_test

import (
	"context"
	"testing"

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
	return service.New(memory.NewPersonRepository(), pub, nil), pub
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
