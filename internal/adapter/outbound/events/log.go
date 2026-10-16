// Package events provides outbound event-publisher adapters. The current
// Client is a logging stand-in; a NATS-backed implementation will slot into
// the same New(ctx, cfg, log) constructor when phase-5 lands.
package events

import (
	"context"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Client is a port.EventPublisher. It is a "Client" rather than a "Service"
// because the current log-based implementation holds no runtime resources and
// has nothing to shut down; when the NATS adapter replaces it the subsystem
// will be promoted to a Service (with a Close that drains and disconnects).
type Client struct {
	log *logger.Log
}

// New returns an event-publisher Client. cfg is accepted for signature
// uniformity (and to let a future NATS implementation read
// cfg.SIS.Adapter.Outbound.Events)
// without changing call sites.
func New(_ context.Context, _ *model.Cfg, log *logger.Log) (*Client, error) {
	return &Client{log: log.New("events")}, nil
}

// Publish logs the event.
func (c *Client) Publish(ctx context.Context, e domain.Event) error {
	c.log.InfoContext(ctx, "domain event",
		"type", e.Type,
		"aggregateType", e.AggregateType,
		"aggregateId", e.AggregateID,
	)
	return nil
}
