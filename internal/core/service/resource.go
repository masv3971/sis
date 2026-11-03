package service

import (
	"cmp"
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
// ExpiresAt is server-managed: a value supplied by the caller is dropped, so
// a record created this way never expires (see UpsertWithExpiry).
func (s *Resource[T, PT]) Create(ctx context.Context, e PT) (PT, error) {
	e.SetExpiresAt(nil)
	return s.create(ctx, e)
}

func (s *Resource[T, PT]) create(ctx context.Context, e PT) (PT, error) {
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

// Update validates and replaces an existing entity, preserving its creation
// time and its expiry (ExpiresAt is server-managed; the caller's value is
// ignored).
func (s *Resource[T, PT]) Update(ctx context.Context, e PT) (PT, error) {
	return s.update(ctx, e, func(existing PT) *time.Time { return existing.GetExpiresAt() })
}

// update replaces an existing entity; expiry picks the new ExpiresAt from
// the record being replaced.
//
// The read and the write are tied together by the repository's conditional
// Update: the write applies only while the stored expiry is still the one
// expiry was computed from. If a concurrent write changed it in between
// (domain.ErrStale), the record is re-read and the merge redone, so a stale
// finite expiry can never overwrite a record that just became permanent.
func (s *Resource[T, PT]) update(ctx context.Context, e PT, expiry func(existing PT) *time.Time) (PT, error) {
	if e.GetID() == "" {
		return nil, domain.Required("id")
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	sourcedID := e.GetSourcedID()
	for attempt := 1; ; attempt++ {
		existing, err := s.repo.Get(ctx, e.GetID())
		if err != nil {
			return nil, err
		}
		e.SetExpiresAt(expiry(existing))
		e.SetCreatedAt(existing.GetCreatedAt())
		e.SetUpdatedAt(s.now().UTC())
		e.SetSourcedID(cmp.Or(sourcedID, existing.GetSourcedID()))
		err = s.repo.Update(ctx, e, existing.GetExpiresAt())
		if errors.Is(err, domain.ErrStale) && attempt < maxStaleRetries {
			continue
		}
		if err != nil {
			return nil, err
		}
		s.emit(ctx, s.typeName+".updated", e.GetID(), e)
		return e, nil
	}
}

// UpsertBySourcedID creates or updates by business key (idempotent ingress).
// The result never expires. Concurrent deliveries of the same sourcedId
// converge on one entity: see Service.UpsertPersonBySourcedID.
func (s *Resource[T, PT]) UpsertBySourcedID(ctx context.Context, e PT) (PT, error) {
	out, _, err := s.UpsertWithExpiry(ctx, e, nil)
	return out, err
}

// UpsertWithExpiry is UpsertBySourcedID for time-limited data: the record
// expires at expiresAt (nil: never). Re-upserting refreshes the expiry, but a
// record that is already permanent stays permanent (see MergeExpiry), so
// loading time-limited data never schedules deletion of a record someone
// else maintains. created reports whether a new record was stored.
func (s *Resource[T, PT]) UpsertWithExpiry(ctx context.Context, e PT, expiresAt *time.Time) (out PT, created bool, err error) {
	if e.GetSourcedID() == "" {
		return nil, false, &domain.ValidationError{Field: "sourcedId", Msg: domain.MsgRequiredForUpsert}
	}
	merge := func(existing PT) *time.Time { return MergeExpiry(existing.GetExpiresAt(), expiresAt) }
	existing, err := s.repo.GetBySourcedID(ctx, e.GetSourcedID())
	switch {
	case err == nil:
		e.SetID(existing.GetID())
		out, err = s.update(ctx, e, merge)
		return out, false, err
	case !errors.Is(err, domain.ErrNotFound):
		return nil, false, err
	}

	e.SetExpiresAt(expiresAt)
	out, err = s.create(ctx, e)
	if !errors.Is(err, domain.ErrConflict) {
		return out, err == nil, err
	}
	winner, getErr := s.repo.GetBySourcedID(ctx, e.GetSourcedID())
	if getErr != nil {
		return nil, false, err
	}
	e.SetID(winner.GetID())
	out, err = s.update(ctx, e, merge)
	return out, false, err
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

// GetBySourcedIDs is GetByIDs by sourcedId.
func (s *Resource[T, PT]) GetBySourcedIDs(ctx context.Context, sourcedIDs []string) ([]PT, error) {
	return s.repo.GetBySourcedIDs(ctx, sourcedIDs)
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

// DeleteExpired removes every record expired now, emits <type>.expired for
// each, and returns their ids.
func (s *Resource[T, PT]) DeleteExpired(ctx context.Context) ([]string, error) {
	ids, err := s.repo.DeleteExpired(ctx, s.now().UTC())
	for _, id := range ids {
		s.emit(ctx, s.typeName+".expired", id, nil)
	}
	return ids, err
}

// TypeName returns the entity type tag (domain.Entity.EntityType).
func (s *Resource[T, PT]) TypeName() string { return s.typeName }

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
