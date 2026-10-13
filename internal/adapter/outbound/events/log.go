// Package events provides outbound event-publisher adapters. LogPublisher
// stands in until the NATS bus adapter lands.
package events

import (
	"context"
	"log/slog"

	"github.com/SUNET/sis/internal/core/domain"
)

// LogPublisher is a port.EventPublisher that logs events.
type LogPublisher struct {
	log *slog.Logger
}

// NewLogPublisher returns a LogPublisher writing to log.
func NewLogPublisher(log *slog.Logger) *LogPublisher {
	return &LogPublisher{log: log}
}

// Publish logs the event.
func (p *LogPublisher) Publish(ctx context.Context, e domain.Event) error {
	p.log.InfoContext(ctx, "domain event",
		"type", e.Type,
		"aggregateType", e.AggregateType,
		"aggregateId", e.AggregateID,
	)
	return nil
}
