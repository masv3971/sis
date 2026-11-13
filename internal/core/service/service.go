// Package service is the application/use-case layer — the hub every inbound
// adapter calls. It owns validation, identity assignment, persistence and event
// emission so they are identical regardless of how a request arrived.
package service

import (
	"context"
	"io"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/trace"
)

// Clock returns the current time; injected so tests are deterministic.
type Clock func() time.Time

// startSpan begins the span of one write use case, e.g. "service person.create".
// Reads get no span of their own: the HTTP request and SQL spans cover them.
func startSpan(ctx context.Context, entityType, op string) (context.Context, func(error)) {
	return trace.Start(ctx, "service "+entityType+"."+op, attribute.String("sis.entity", entityType))
}

// publish hands event to the publisher. The write it describes is already
// committed and cannot be rolled back here, so a failure is logged with the
// event's identity instead of being returned (see port.EventPublisher).
func publish(ctx context.Context, events port.EventPublisher, log *logger.Log, event domain.Event) {
	if events == nil {
		return
	}
	if err := events.Publish(ctx, event); err != nil {
		log.ErrorContext(ctx, "event publish failed; event lost",
			"eventId", event.ID, "type", event.Type,
			"aggregateType", event.AggregateType, "aggregateId", event.AggregateID,
			"err", err)
	}
}

func serviceLog(log *logger.Log) *logger.Log {
	if log == nil {
		return logger.NewWithWriter("service", false, "error", io.Discard)
	}
	return log.New("service")
}
