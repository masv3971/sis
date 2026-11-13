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

// MergeExpiry returns the expiry a record gets when data with expiry
// incoming is upserted onto a stored record whose expiry is stored: a
// permanent record (stored == nil) stays permanent, otherwise the incoming
// expiry wins (refreshing it, or making the record permanent when nil).
func MergeExpiry(stored, incoming *time.Time) *time.Time {
	if stored == nil {
		return nil
	}
	return incoming
}

// maxStaleRetries bounds how often an update re-reads and retries after its
// conditional write lost to a concurrent expiry change. Each retry needs a
// fresh concurrent writer to fail again, so a handful is plenty; past it the
// caller gets domain.ErrStale (a 409 conflict).
const maxStaleRetries = 5

// Resource is the generic application/use-case layer for any entity type
// (validation, identity, timestamps, event emission) over the generic
// repository, so every entity behaves identically.
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
func (s *Resource[T, PT]) Create(ctx context.Context, e PT) (out PT, err error) {
	ctx, end := startSpan(ctx, s.typeName, "create")
	defer func() { end(err) }()
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
func (s *Resource[T, PT]) Update(ctx context.Context, e PT) (out PT, err error) {
	ctx, end := startSpan(ctx, s.typeName, "update")
	defer func() { end(err) }()
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
// converge on one entity: the loser of a create race updates the winner.
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
	ctx, end := startSpan(ctx, s.typeName, "upsert")
	defer func() { end(err) }()
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

// ListByRef returns a page of the entities whose reference field (its JSON
// name, a constant such as "personId") equals id.
func (s *Resource[T, PT]) ListByRef(ctx context.Context, field, id string, params port.ListParams) (port.Page[PT], error) {
	return s.repo.ListByRef(ctx, field, id, params)
}

// ListByRefs returns the entities whose reference field (a constant such as
// "offeringId") equals any of ids.
func (s *Resource[T, PT]) ListByRefs(ctx context.Context, field string, ids []string) ([]PT, error) {
	return s.repo.ListByRefs(ctx, field, ids)
}

// Delete removes an entity and emits an event.
func (s *Resource[T, PT]) Delete(ctx context.Context, id string) (err error) {
	ctx, end := startSpan(ctx, s.typeName, "delete")
	defer func() { end(err) }()
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.emit(ctx, s.typeName+".deleted", id, nil)
	return nil
}

// DeleteByRef is Delete, but only while the entity's reference field (a
// constant such as "personId") still equals value; otherwise ErrNotFound.
func (s *Resource[T, PT]) DeleteByRef(ctx context.Context, id, field, value string) (err error) {
	ctx, end := startSpan(ctx, s.typeName, "delete")
	defer func() { end(err) }()
	if err := s.repo.DeleteByRef(ctx, id, field, value); err != nil {
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
