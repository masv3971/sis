// Package service is the application/use-case layer — the hub every inbound
// adapter calls. It owns validation, identity assignment, persistence and event
// emission so they are identical regardless of how a request arrived.
package service

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
)

// Clock returns the current time; injected so tests are deterministic.
type Clock func() time.Time

// Service implements the use cases over the injected ports.
type Service struct {
	persons port.PersonRepository
	events  port.EventPublisher
	now     Clock
	newID   func() string
	log     *logger.Log
}

// New builds a Service. If clock is nil, time.Now is used; if log is nil,
// log output is discarded.
func New(persons port.PersonRepository, events port.EventPublisher, clock Clock, log *logger.Log) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{persons: persons, events: events, now: clock, newID: uuid.NewString, log: serviceLog(log)}
}

// emit publishes a domain event; a nil publisher is a no-op.
func (s *Service) emit(ctx context.Context, eventType, aggType, aggID string, payload any) {
	publish(ctx, s.events, s.log, domain.Event{
		ID:            s.newID(),
		Type:          eventType,
		AggregateType: aggType,
		AggregateID:   aggID,
		OccurredAt:    s.now().UTC(),
		Payload:       payload,
	})
}

// publish hands e to the publisher. The write it describes is already
// committed and cannot be rolled back here, so a failure is logged with the
// event's identity instead of being returned (see port.EventPublisher).
func publish(ctx context.Context, events port.EventPublisher, log *logger.Log, e domain.Event) {
	if events == nil {
		return
	}
	if err := events.Publish(ctx, e); err != nil {
		log.ErrorContext(ctx, "event publish failed; event lost",
			"eventId", e.ID, "type", e.Type,
			"aggregateType", e.AggregateType, "aggregateId", e.AggregateID,
			"err", err)
	}
}

func serviceLog(log *logger.Log) *logger.Log {
	if log == nil {
		return logger.NewWithWriter("service", false, "error", io.Discard)
	}
	return log.New("service")
}
