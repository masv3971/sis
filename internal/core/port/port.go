// Package port declares the interfaces (ports) the core depends on. Outbound
// adapters implement them; the service layer calls them. No adapter imports
// another — everything meets here.
package port

import (
	"context"

	"github.com/SUNET/sis/internal/core/domain"
)

// ListParams carries pagination/sort/filter options common to list queries.
// Each inbound adapter maps its own conventions (OEAPI pageNumber/pageSize,
// OneRoster limit/offset) onto this shared shape.
//
// Repositories treat Limit <= 0 as "unbounded", so inbound adapters must
// reject non-positive limits and clamp larger ones to their configured
// maximum (sis.adapter.inbound.<api>) before calling a repository.
type ListParams struct {
	Limit  int
	Offset int
	Sort   string
	Filter string
}

// Page is a paginated query result.
type Page[T any] struct {
	Items []T
	Total int
}

// PersonRepository is the persistence port for Person aggregates.
type PersonRepository interface {
	Create(ctx context.Context, p domain.Person) error
	Update(ctx context.Context, p domain.Person) error
	Get(ctx context.Context, id string) (domain.Person, error)
	GetBySourcedID(ctx context.Context, sourcedID string) (domain.Person, error)
	List(ctx context.Context, params ListParams) (Page[domain.Person], error)
	Delete(ctx context.Context, id string) error
}

// EventPublisher is the port for emitting domain events to the outside world.
type EventPublisher interface {
	Publish(ctx context.Context, e domain.Event) error
}
