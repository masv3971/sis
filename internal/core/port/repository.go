package port

import (
	"context"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
)

// Repository is the generic persistence port for any canonical entity. T is the
// entity value type and PT its pointer, which must implement domain.Entity.
//
// Expired records (domain.Expired) are invisible to every method except
// DeleteExpired: reads skip them, Update and Delete answer ErrNotFound, and
// Create replaces an expired record holding the same id or sourcedId instead
// of reporting ErrConflict. The sweeper removes them physically.
type Repository[T any, PT domain.EntityPtr[T]] interface {
	Create(ctx context.Context, e PT) error
	// Update replaces a live entity, but only while its stored expiry still
	// equals prevExpiresAt (nil: permanent; see domain.SameExpiry). If
	// another write changed the expiry since the caller read it, nothing is
	// written and domain.ErrStale is returned, so the caller can re-read and
	// merge again instead of overwriting the newer expiry.
	Update(ctx context.Context, e PT, prevExpiresAt *time.Time) error
	Get(ctx context.Context, id string) (PT, error)
	// GetByIDs returns the entities whose canonical id is in ids, in no
	// particular order. Ids that match nothing are skipped, not an error.
	GetByIDs(ctx context.Context, ids []string) ([]PT, error)
	GetBySourcedID(ctx context.Context, sourcedID string) (PT, error)
	List(ctx context.Context, params ListParams) (Page[PT], error)
	Delete(ctx context.Context, id string) error
	// DeleteExpired removes every record expired at now and returns their
	// ids, in no particular order.
	DeleteExpired(ctx context.Context, now time.Time) ([]string, error)
}
