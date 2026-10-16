// Package memory provides in-memory implementations of the outbound ports for
// development and tests. The Postgres adapter replaces it in production.
package memory

import (
	"context"
	"slices"
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

// clonePerson returns a copy of p that shares no slice backing arrays with
// it. Every write stores a clone and every read returns one, so neither the
// caller's argument nor a returned value can change stored data outside a
// repository write (and its mutex) — the isolation the Postgres and JSON
// document repositories get for free by serialising.
func clonePerson(p domain.Person) domain.Person {
	p.Roles = slices.Clone(p.Roles)
	p.Identifiers = slices.Clone(p.Identifiers)
	p.Emails = slices.Clone(p.Emails)
	p.Phones = slices.Clone(p.Phones)
	return p
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
	r.byID[p.ID] = clonePerson(p)
	return nil
}

// Update replaces an existing person, rejecting a sourcedId already held by a
// different person (mirrors the Postgres UNIQUE constraint).
func (r *PersonRepository) Update(_ context.Context, p domain.Person) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[p.ID]; !ok {
		return domain.ErrNotFound
	}
	if p.SourcedID != "" {
		for id, e := range r.byID {
			if id != p.ID && e.SourcedID == p.SourcedID {
				return domain.ErrConflict
			}
		}
	}
	r.byID[p.ID] = clonePerson(p)
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
	return clonePerson(p), nil
}

// GetByIDs returns the persons whose id is in ids.
func (r *PersonRepository) GetByIDs(_ context.Context, ids []string) ([]domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.Person, 0, len(ids))
	for _, id := range ids {
		if p, ok := r.byID[id]; ok {
			items = append(items, clonePerson(p))
		}
	}
	return items, nil
}

// GetBySourcedID returns a person by business key.
func (r *PersonRepository) GetBySourcedID(_ context.Context, sourcedID string) (domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.byID {
		if p.SourcedID == sourcedID {
			return clonePerson(p), nil
		}
	}
	return domain.Person{}, domain.ErrNotFound
}

// List returns a page of persons sorted by family name, given name, then id.
// The unique id tie-breaker keeps the order total, so pages are stable across
// calls even though map iteration is randomised.
func (r *PersonRepository) List(_ context.Context, params port.ListParams) (port.Page[domain.Person], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.Person, 0, len(r.byID))
	for _, p := range r.byID {
		items = append(items, clonePerson(p))
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.FamilyName != b.FamilyName {
			return a.FamilyName < b.FamilyName
		}
		if a.GivenName != b.GivenName {
			return a.GivenName < b.GivenName
		}
		return a.ID < b.ID
	})
	lo, hi := pageBounds(len(items), params.Offset, params.Limit)
	return port.Page[domain.Person]{Items: items[lo:hi], Total: len(items)}, nil
}

// pageBounds returns the [lo:hi) slice bounds for a page of a total-length
// result. It never adds offset and limit together, so attacker-chosen values
// near MaxInt cannot overflow into a negative bound. A non-positive limit
// means "no limit" (the inbound adapters are responsible for capping it).
func pageBounds(total, offset, limit int) (lo, hi int) {
	lo = min(max(offset, 0), total)
	hi = total
	if limit > 0 && limit < total-lo {
		hi = lo + limit
	}
	return lo, hi
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
