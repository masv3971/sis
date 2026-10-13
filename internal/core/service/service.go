// Package service is the application/use-case layer — the hub every inbound
// adapter calls. It owns validation, identity assignment, persistence and event
// emission so they are identical regardless of how a request arrived.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// Clock returns the current time; injected so tests are deterministic.
type Clock func() time.Time

// Service implements the use cases over the injected ports.
type Service struct {
	persons port.PersonRepository
	events  port.EventPublisher
	now     Clock
	newID   func() string
}

// New builds a Service. If clock is nil, time.Now is used.
func New(persons port.PersonRepository, events port.EventPublisher, clock Clock) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{persons: persons, events: events, now: clock, newID: uuid.NewString}
}

// emit publishes a domain event; a nil publisher is a no-op.
func (s *Service) emit(ctx context.Context, eventType, aggType, aggID string, payload any) {
	if s.events == nil {
		return
	}
	_ = s.events.Publish(ctx, domain.Event{
		ID:            s.newID(),
		Type:          eventType,
		AggregateType: aggType,
		AggregateID:   aggID,
		OccurredAt:    s.now().UTC(),
		Payload:       payload,
	})
}
