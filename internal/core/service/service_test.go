package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingPublisher struct{}

func (failingPublisher) Publish(context.Context, domain.Event) error {
	return errors.New("bus down")
}

func TestPublishFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter("sis", false, "info", &buf)
	svc := service.New(memory.NewPersonRepository(), failingPublisher{}, nil, log)

	person, err := svc.CreatePerson(t.Context(), domain.Person{
		GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive,
	})
	require.NoError(t, err, "committed write must still succeed")
	out := buf.String()
	for _, want := range []string{"level=ERROR", "event publish failed", "type=person.created", "aggregateId=" + person.ID, "bus down"} {
		assert.Contains(t, out, want, "log missing")
	}
}
