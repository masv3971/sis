package service

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
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

// CreatePerson validates, assigns identity/timestamps, persists, and emits an
// event. ExpiresAt is server-managed: the caller's value is dropped.
func (s *Service) CreatePerson(ctx context.Context, person domain.Person) (domain.Person, error) {
	person.ExpiresAt = nil
	return s.createPerson(ctx, person)
}

func (s *Service) createPerson(ctx context.Context, person domain.Person) (domain.Person, error) {
	if err := person.Validate(); err != nil {
		return domain.Person{}, err
	}
	now := s.now().UTC()
	if person.ID == "" {
		person.ID = s.newID()
	}
	if person.SourcedID == "" {
		person.SourcedID = person.ID
	}
	person.CreatedAt = now
	person.UpdatedAt = now
	if err := s.persons.Create(ctx, person); err != nil {
		return domain.Person{}, err
	}
	s.emit(ctx, "person.created", "person", person.ID, person)
	return person, nil
}

// UpdatePerson validates and replaces an existing person, preserving its
// creation time and expiry (server-managed), and emits an event.
func (s *Service) UpdatePerson(ctx context.Context, person domain.Person) (domain.Person, error) {
	return s.updatePerson(ctx, person, func(existing domain.Person) *time.Time { return existing.ExpiresAt })
}

func (s *Service) updatePerson(ctx context.Context, person domain.Person, expiry func(existing domain.Person) *time.Time) (domain.Person, error) {
	if person.ID == "" {
		return domain.Person{}, domain.Required("id")
	}
	if err := person.Validate(); err != nil {
		return domain.Person{}, err
	}
	// Conditional write with re-read on domain.ErrStale; see Resource.update.
	sourcedID := person.SourcedID
	for attempt := 1; ; attempt++ {
		existing, err := s.persons.Get(ctx, person.ID)
		if err != nil {
			return domain.Person{}, err
		}
		person.ExpiresAt = expiry(existing)
		person.CreatedAt = existing.CreatedAt
		person.UpdatedAt = s.now().UTC()
		person.SourcedID = cmp.Or(sourcedID, existing.SourcedID)
		err = s.persons.Update(ctx, person, existing.ExpiresAt)
		if errors.Is(err, domain.ErrStale) && attempt < maxStaleRetries {
			continue
		}
		if err != nil {
			return domain.Person{}, err
		}
		s.emit(ctx, "person.updated", "person", person.ID, person)
		return person, nil
	}
}

// maxStaleRetries bounds how often an update re-reads and retries after its
// conditional write lost to a concurrent expiry change. Each retry needs a
// fresh concurrent writer to fail again, so a handful is plenty; past it the
// caller gets domain.ErrStale (a 409 conflict).
const maxStaleRetries = 5

// UpsertPersonBySourcedID creates or updates by business key, so repeated
// deliveries of the same record (bus redelivery, bulk re-import) converge to
// one person — the idempotency the hub's non-HTTP ingress paths depend on.
//
// Two concurrent deliveries can both miss the lookup and both try to create.
// The repository enforces sourcedId uniqueness, so one Create wins and the
// other gets domain.ErrConflict; the loser then updates the winner's record,
// and both calls succeed with the same id.
//
// The result never expires; see UpsertPersonWithExpiry.
func (s *Service) UpsertPersonBySourcedID(ctx context.Context, person domain.Person) (domain.Person, error) {
	out, _, err := s.UpsertPersonWithExpiry(ctx, person, nil)
	return out, err
}

// UpsertPersonWithExpiry is UpsertPersonBySourcedID for time-limited data,
// with the expiry rules of Resource.UpsertWithExpiry. created reports
// whether a new person was stored.
func (s *Service) UpsertPersonWithExpiry(ctx context.Context, person domain.Person, expiresAt *time.Time) (out domain.Person, created bool, err error) {
	if person.SourcedID == "" {
		return domain.Person{}, false, &domain.ValidationError{Field: "sourcedId", Msg: domain.MsgRequiredForUpsert}
	}
	merge := func(existing domain.Person) *time.Time { return MergeExpiry(existing.ExpiresAt, expiresAt) }
	existing, err := s.persons.GetBySourcedID(ctx, person.SourcedID)
	switch {
	case err == nil:
		person.ID = existing.ID
		out, err = s.updatePerson(ctx, person, merge)
		return out, false, err
	case !errors.Is(err, domain.ErrNotFound):
		return domain.Person{}, false, err
	}

	person.ExpiresAt = expiresAt
	out, err = s.createPerson(ctx, person)
	if !errors.Is(err, domain.ErrConflict) {
		return out, err == nil, err
	}
	// Lost the race. If the conflict was not on sourcedId (e.g. a
	// caller-supplied id already in use), there is no winner to update.
	winner, getErr := s.persons.GetBySourcedID(ctx, person.SourcedID)
	if getErr != nil {
		return domain.Person{}, false, err
	}
	person.ID = winner.ID
	out, err = s.updatePerson(ctx, person, merge)
	return out, false, err
}

// GetPerson returns a person by id.
func (s *Service) GetPerson(ctx context.Context, id string) (domain.Person, error) {
	return s.persons.Get(ctx, id)
}

// GetPersons returns the persons whose id is in ids in one repository call;
// ids that match nothing are skipped.
func (s *Service) GetPersons(ctx context.Context, ids []string) ([]domain.Person, error) {
	return s.persons.GetByIDs(ctx, ids)
}

// GetPersonBySourcedID returns a person by business key (used by standards providers).
func (s *Service) GetPersonBySourcedID(ctx context.Context, sourcedID string) (domain.Person, error) {
	return s.persons.GetBySourcedID(ctx, sourcedID)
}

// GetPersonsBySourcedIDs is GetPersons by sourcedId.
func (s *Service) GetPersonsBySourcedIDs(ctx context.Context, sourcedIDs []string) ([]domain.Person, error) {
	return s.persons.GetBySourcedIDs(ctx, sourcedIDs)
}

// ListPersons returns a page of persons.
func (s *Service) ListPersons(ctx context.Context, params port.ListParams) (port.Page[domain.Person], error) {
	return s.persons.List(ctx, params)
}

// DeletePerson removes a person and emits an event.
func (s *Service) DeletePerson(ctx context.Context, id string) error {
	if err := s.persons.Delete(ctx, id); err != nil {
		return err
	}
	s.emit(ctx, "person.deleted", "person", id, nil)
	return nil
}

// DeleteExpiredPersons removes every person expired now, emits
// person.expired for each, and returns their ids.
func (s *Service) DeleteExpiredPersons(ctx context.Context) ([]string, error) {
	ids, err := s.persons.DeleteExpired(ctx, s.now().UTC())
	for _, id := range ids {
		s.emit(ctx, "person.expired", "person", id, nil)
	}
	return ids, err
}
