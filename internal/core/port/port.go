// Package port declares the interfaces (ports) the core depends on. Outbound
// adapters implement them; the service layer calls them. No adapter imports
// another — everything meets here.
package port

import (
	"context"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
)

// ListParams carries the pagination options common to list queries. Each
// inbound adapter maps its own conventions (OEAPI pageNumber/pageSize,
// OneRoster limit/offset) onto this shared shape. Sorting and filtering are
// not supported yet; inbound adapters reject them rather than ignore them.
// Add fields here only together with an implementation in every repository.
//
// Repositories treat Limit <= 0 as "unbounded", so inbound adapters must
// reject non-positive limits and clamp larger ones to their configured
// maximum (sis.adapter.inbound.<api>) before calling a repository.
type ListParams struct {
	Limit  int
	Offset int
}

// Page is a paginated query result.
type Page[T any] struct {
	Items []T
	Total int
}

// PersonRepository is the persistence port for Person aggregates. Expired
// persons behave as described on Repository.
type PersonRepository interface {
	Create(ctx context.Context, p domain.Person) error
	// Update is conditional on the stored expiry, as Repository.Update.
	Update(ctx context.Context, p domain.Person, prevExpiresAt *time.Time) error
	Get(ctx context.Context, id string) (domain.Person, error)
	// GetByIDs returns the persons whose id is in ids, in no particular
	// order. Ids that match nothing are skipped, not an error.
	GetByIDs(ctx context.Context, ids []string) ([]domain.Person, error)
	GetBySourcedID(ctx context.Context, sourcedID string) (domain.Person, error)
	List(ctx context.Context, params ListParams) (Page[domain.Person], error)
	Delete(ctx context.Context, id string) error
	// DeleteExpired removes every person expired at now and returns their
	// ids, in no particular order.
	DeleteExpired(ctx context.Context, now time.Time) ([]string, error)
}

// EventPublisher is the port for emitting domain events to the outside world.
//
// Publish is called after the write it describes has been committed, so the
// service cannot undo the write when Publish fails; it logs the lost event
// instead. A publisher that can fail (e.g. a message bus) is responsible for
// durable delivery, for example by writing an outbox in the same transaction
// and relaying from it.
type EventPublisher interface {
	Publish(ctx context.Context, e domain.Event) error
}
