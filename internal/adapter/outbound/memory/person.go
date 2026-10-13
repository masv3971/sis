// Package memory provides in-memory implementations of the outbound ports for
// development and tests. The Postgres adapter replaces it in production.
package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// PersonRepository is an in-memory port.PersonRepository.
type PersonRepository struct {
	mu   sync.RWMutex
	byID map[string]domain.Person
}

// NewPersonRepository returns an empty in-memory person repository.
func NewPersonRepository() *PersonRepository {
	return &PersonRepository{byID: make(map[string]domain.Person)}
}

// Create stores a new person, rejecting duplicate id or sourcedId.
func (r *PersonRepository) Create(_ context.Context, p domain.Person) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[p.ID]; ok {
		return domain.ErrConflict
	}
	for _, e := range r.byID {
		if p.SourcedID != "" && e.SourcedID == p.SourcedID {
			return domain.ErrConflict
		}
	}
	r.byID[p.ID] = p
	return nil
}

// Update replaces an existing person.
func (r *PersonRepository) Update(_ context.Context, p domain.Person) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[p.ID]; !ok {
		return domain.ErrNotFound
	}
	r.byID[p.ID] = p
	return nil
}

// Get returns a person by id.
func (r *PersonRepository) Get(_ context.Context, id string) (domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	if !ok {
		return domain.Person{}, domain.ErrNotFound
	}
	return p, nil
}

// GetBySourcedID returns a person by business key.
func (r *PersonRepository) GetBySourcedID(_ context.Context, sourcedID string) (domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.byID {
		if p.SourcedID == sourcedID {
			return p, nil
		}
	}
	return domain.Person{}, domain.ErrNotFound
}

// List returns a page of persons sorted by family then given name.
func (r *PersonRepository) List(_ context.Context, params port.ListParams) (port.Page[domain.Person], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.Person, 0, len(r.byID))
	for _, p := range r.byID {
		items = append(items, p)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].FamilyName == items[j].FamilyName {
			return items[i].GivenName < items[j].GivenName
		}
		return items[i].FamilyName < items[j].FamilyName
	})
	total := len(items)
	lo := params.Offset
	if lo > total {
		lo = total
	}
	hi := total
	if params.Limit > 0 && lo+params.Limit < hi {
		hi = lo + params.Limit
	}
	return port.Page[domain.Person]{Items: items[lo:hi], Total: total}, nil
}

// Delete removes a person by id.
func (r *PersonRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.byID, id)
	return nil
}
