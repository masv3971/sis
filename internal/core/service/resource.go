package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
)

// Resource is the generic application/use-case layer for any entity type. It
// mirrors the bespoke person service (validation, identity, timestamps, event
// emission) over the generic repository, so every entity behaves identically.
type Resource[T any, PT domain.EntityPtr[T]] struct {
	repo     port.Repository[T, PT]
	events   port.EventPublisher
	now      Clock
	newID    func() string
	typeName string
	log      *logger.Log
}

// NewResource builds a Resource. If clock is nil, time.Now is used; if log is
// nil, log output is discarded.
func NewResource[T any, PT domain.EntityPtr[T]](repo port.Repository[T, PT], events port.EventPublisher, clock Clock, log *logger.Log) *Resource[T, PT] {
	if clock == nil {
		clock = time.Now
	}
	return &Resource[T, PT]{
		repo:     repo,
		events:   events,
		now:      clock,
		newID:    uuid.NewString,
		typeName: PT(new(T)).EntityType(),
		log:      serviceLog(log),
	}
}

// Create validates, assigns identity/timestamps, persists, and emits an event.
func (s *Resource[T, PT]) Create(ctx context.Context, e PT) (PT, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if e.GetID() == "" {
		e.SetID(s.newID())
	}
	if e.GetSourcedID() == "" {
		e.SetSourcedID(e.GetID())
	}
	e.SetCreatedAt(now)
	e.SetUpdatedAt(now)
	if err := s.repo.Create(ctx, e); err != nil {
		return nil, err
	}
	s.emit(ctx, s.typeName+".created", e.GetID(), e)
	return e, nil
}

// Update validates and replaces an existing entity, preserving its creation time.
func (s *Resource[T, PT]) Update(ctx context.Context, e PT) (PT, error) {
	if e.GetID() == "" {
		return nil, &domain.ValidationError{Field: "id", Msg: "required"}
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	existing, err := s.repo.Get(ctx, e.GetID())
	if err != nil {
		return nil, err
	}
	e.SetCreatedAt(existing.GetCreatedAt())
	e.SetUpdatedAt(s.now().UTC())
	if e.GetSourcedID() == "" {
		e.SetSourcedID(existing.GetSourcedID())
	}
	if err := s.repo.Update(ctx, e); err != nil {
		return nil, err
	}
	s.emit(ctx, s.typeName+".updated", e.GetID(), e)
	return e, nil
}

// UpsertBySourcedID creates or updates by business key (idempotent ingress).
// Concurrent deliveries of the same sourcedId converge on one entity: see
// Service.UpsertPersonBySourcedID.
func (s *Resource[T, PT]) UpsertBySourcedID(ctx context.Context, e PT) (PT, error) {
	if e.GetSourcedID() == "" {
		return nil, &domain.ValidationError{Field: "sourcedId", Msg: "required for upsert"}
	}
	existing, err := s.repo.GetBySourcedID(ctx, e.GetSourcedID())
	switch {
	case err == nil:
		e.SetID(existing.GetID())
		return s.Update(ctx, e)
	case !errors.Is(err, domain.ErrNotFound):
		return nil, err
	}

	created, err := s.Create(ctx, e)
	if !errors.Is(err, domain.ErrConflict) {
		return created, err
	}
	winner, getErr := s.repo.GetBySourcedID(ctx, e.GetSourcedID())
	if getErr != nil {
		return nil, err
	}
	e.SetID(winner.GetID())
	return s.Update(ctx, e)
}

// Get returns an entity by id.
func (s *Resource[T, PT]) Get(ctx context.Context, id string) (PT, error) {
	return s.repo.Get(ctx, id)
}

// GetByIDs returns the entities whose id is in ids in one repository call;
// ids that match nothing are skipped.
func (s *Resource[T, PT]) GetByIDs(ctx context.Context, ids []string) ([]PT, error) {
	return s.repo.GetByIDs(ctx, ids)
}

// GetBySourcedID returns an entity by business key (used by standards providers,
// which address resources by sourcedId).
func (s *Resource[T, PT]) GetBySourcedID(ctx context.Context, sourcedID string) (PT, error) {
	return s.repo.GetBySourcedID(ctx, sourcedID)
}

// List returns a page of entities.
func (s *Resource[T, PT]) List(ctx context.Context, params port.ListParams) (port.Page[PT], error) {
	return s.repo.List(ctx, params)
}

// Delete removes an entity and emits an event.
func (s *Resource[T, PT]) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.emit(ctx, s.typeName+".deleted", id, nil)
	return nil
}

func (s *Resource[T, PT]) emit(ctx context.Context, eventType, aggID string, payload any) {
	publish(ctx, s.events, s.log, domain.Event{
		ID:            s.newID(),
		Type:          eventType,
		AggregateType: s.typeName,
		AggregateID:   aggID,
		OccurredAt:    s.now().UTC(),
		Payload:       payload,
	})
}
