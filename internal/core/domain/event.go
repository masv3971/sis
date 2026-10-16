package domain

import "time"

// Event is a domain event emitted on every successful write. The outbound
// events adapter publishes it to the bus so downstream systems can subscribe.
type Event struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	AggregateType string    `json:"aggregateType"`
	AggregateID   string    `json:"aggregateId"`
	OccurredAt    time.Time `json:"occurredAt"`
	Payload       any       `json:"payload,omitempty"`
}
