package service_test

import (
	"context"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
)

func newOrgResource() *service.Resource[domain.Organization, *domain.Organization] {
	repo := memory.NewDocRepo[domain.Organization]()
	return service.NewResource(repo, nil, nil)
}

func TestResource_CreateValidationAndID(t *testing.T) {
	svc := newOrgResource()
	ctx := context.Background()

	if _, err := svc.Create(ctx, &domain.Organization{}); err == nil {
		t.Fatal("expected validation error for missing name")
	}

	org, err := svc.Create(ctx, &domain.Organization{Name: "Uppsala University", Type: "institution"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if org.GetID() == "" || org.GetSourcedID() != org.GetID() {
		t.Fatalf("expected generated id with sourcedId defaulting to it: %+v", org)
	}
}

func TestResource_UpsertIsIdempotent(t *testing.T) {
	svc := newOrgResource()
	ctx := context.Background()

	first, err := svc.UpsertBySourcedID(ctx, &domain.Organization{Base: domain.Base{SourcedID: "org-1"}, Name: "Dept A"})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := svc.UpsertBySourcedID(ctx, &domain.Organization{Base: domain.Base{SourcedID: "org-1"}, Name: "Dept A (renamed)"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.GetID() != second.GetID() {
		t.Fatalf("upsert must reuse id: %s != %s", first.GetID(), second.GetID())
	}

	page, err := svc.List(ctx, port.ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected 1 org after repeated upsert, got %d", page.Total)
	}
	if page.Items[0].Name != "Dept A (renamed)" {
		t.Fatalf("expected updated name, got %q", page.Items[0].Name)
	}
}
