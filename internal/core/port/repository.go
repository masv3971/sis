package port

import (
	"context"

	"github.com/SUNET/sis/internal/core/domain"
)

// Repository is the generic persistence port for any canonical entity. T is the
// entity value type and PT its pointer, which must implement domain.Entity.
type Repository[T any, PT domain.EntityPtr[T]] interface {
	Create(ctx context.Context, e PT) error
	Update(ctx context.Context, e PT) error
	Get(ctx context.Context, id string) (PT, error)
	// GetByIDs returns the entities whose canonical id is in ids, in no
	// particular order. Ids that match nothing are skipped, not an error.
	GetByIDs(ctx context.Context, ids []string) ([]PT, error)
	GetBySourcedID(ctx context.Context, sourcedID string) (PT, error)
	List(ctx context.Context, params ListParams) (Page[PT], error)
	Delete(ctx context.Context, id string) error
}
