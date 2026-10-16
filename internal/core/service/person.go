package service

import (
	"context"
	"errors"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// CreatePerson validates, assigns identity/timestamps, persists, and emits an event.
func (s *Service) CreatePerson(ctx context.Context, p domain.Person) (domain.Person, error) {
	if err := p.Validate(); err != nil {
		return domain.Person{}, err
	}
	now := s.now().UTC()
	if p.ID == "" {
		p.ID = s.newID()
	}
	if p.SourcedID == "" {
		p.SourcedID = p.ID
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	if err := s.persons.Create(ctx, p); err != nil {
		return domain.Person{}, err
	}
	s.emit(ctx, "person.created", "person", p.ID, p)
	return p, nil
}

// UpdatePerson validates and replaces an existing person, preserving its
// creation time, and emits an event.
func (s *Service) UpdatePerson(ctx context.Context, p domain.Person) (domain.Person, error) {
	if p.ID == "" {
		return domain.Person{}, &domain.ValidationError{Field: "id", Msg: "required"}
	}
	if err := p.Validate(); err != nil {
		return domain.Person{}, err
	}
	existing, err := s.persons.Get(ctx, p.ID)
	if err != nil {
		return domain.Person{}, err
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = s.now().UTC()
	if p.SourcedID == "" {
		p.SourcedID = existing.SourcedID
	}
	if err := s.persons.Update(ctx, p); err != nil {
		return domain.Person{}, err
	}
	s.emit(ctx, "person.updated", "person", p.ID, p)
	return p, nil
}

// UpsertPersonBySourcedID creates or updates by business key, so repeated
// deliveries of the same record (bus redelivery, bulk re-import) converge to
// one person — the idempotency the hub's non-HTTP ingress paths depend on.
//
// Two concurrent deliveries can both miss the lookup and both try to create.
// The repository enforces sourcedId uniqueness, so one Create wins and the
// other gets domain.ErrConflict; the loser then updates the winner's record,
// and both calls succeed with the same id.
func (s *Service) UpsertPersonBySourcedID(ctx context.Context, p domain.Person) (domain.Person, error) {
	if p.SourcedID == "" {
		return domain.Person{}, &domain.ValidationError{Field: "sourcedId", Msg: "required for upsert"}
	}
	existing, err := s.persons.GetBySourcedID(ctx, p.SourcedID)
	switch {
	case err == nil:
		p.ID = existing.ID
		return s.UpdatePerson(ctx, p)
	case !errors.Is(err, domain.ErrNotFound):
		return domain.Person{}, err
	}

	created, err := s.CreatePerson(ctx, p)
	if !errors.Is(err, domain.ErrConflict) {
		return created, err
	}
	// Lost the race. If the conflict was not on sourcedId (e.g. a
	// caller-supplied id already in use), there is no winner to update.
	winner, getErr := s.persons.GetBySourcedID(ctx, p.SourcedID)
	if getErr != nil {
		return domain.Person{}, err
	}
	p.ID = winner.ID
	return s.UpdatePerson(ctx, p)
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
