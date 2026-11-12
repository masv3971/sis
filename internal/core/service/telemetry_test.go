package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
)

func TestWritesGetOneSpanEach(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	orgs := newOrgResource()
	persons := service.New(memory.NewPersonRepository(), nil, nil, nil)
	ctx := t.Context()

	org, err := orgs.Create(ctx, &domain.Organization{Name: "Uni"})
	require.NoError(t, err)
	_, err = orgs.Update(ctx, org)
	require.NoError(t, err)
	// An upsert that updates is one "upsert" span, not an extra "update".
	_, err = orgs.UpsertBySourcedID(ctx, &domain.Organization{SourcedID: org.GetSourcedID(), Name: "Uni 2"})
	require.NoError(t, err)
	require.NoError(t, orgs.Delete(ctx, org.GetID()))
	_, err = orgs.Get(ctx, org.GetID())
	require.ErrorIs(t, err, domain.ErrNotFound)

	person, err := persons.CreatePerson(ctx, domain.Person{GivenName: "A", FamilyName: "B", Status: "active"})
	require.NoError(t, err)
	require.NoError(t, persons.DeletePerson(ctx, person.ID))
	assert.ErrorIs(t, persons.DeletePerson(ctx, person.ID), domain.ErrNotFound)

	var names []string
	for _, span := range spans.Ended() {
		names = append(names, span.Name())
		assert.Equal(t, codes.Unset, span.Status().Code, "%s: not-found is the caller's error", span.Name())
	}
	assert.Equal(t, []string{
		"service organization.create", "service organization.update", "service organization.upsert", "service organization.delete",
		"service person.create", "service person.delete", "service person.delete",
	}, names, "spans, in order; reads have none")
}
