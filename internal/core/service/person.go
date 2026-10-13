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
func (s *Service) UpsertPersonBySourcedID(ctx context.Context, p domain.Person) (domain.Person, error) {
	if p.SourcedID == "" {
		return domain.Person{}, &domain.ValidationError{Field: "sourcedId", Msg: "required for upsert"}
	}
	existing, err := s.persons.GetBySourcedID(ctx, p.SourcedID)
	switch {
	case err == nil:
		p.ID = existing.ID
		return s.UpdatePerson(ctx, p)
	case errors.Is(err, domain.ErrNotFound):
		return s.CreatePerson(ctx, p)
	default:
		return domain.Person{}, err
	}
}

// GetPerson returns a person by id.
func (s *Service) GetPerson(ctx context.Context, id string) (domain.Person, error) {
	return s.persons.Get(ctx, id)
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
