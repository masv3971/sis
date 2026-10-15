package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/logger"
)

type failingPublisher struct{}

func (failingPublisher) Publish(context.Context, domain.Event) error {
	return errors.New("bus down")
}

func TestPublishFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter("sis", false, "info", &buf)
	svc := service.New(memory.NewPersonRepository(), failingPublisher{}, nil, log)

	p, err := svc.CreatePerson(context.Background(), domain.Person{
		GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatalf("committed write must still succeed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"level=ERROR", "event publish failed", "type=person.created", "aggregateId=" + p.ID, "bus down"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log missing %q:\n%s", want, out)
		}
	}
}
