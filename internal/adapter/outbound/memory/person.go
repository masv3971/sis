// Package memory provides in-memory implementations of the outbound ports for
// development and tests. The Postgres adapter replaces it in production.
package memory

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// PersonRepository is an in-memory port.PersonRepository. Expired persons
// are invisible except to DeleteExpired (see port.Repository).
type PersonRepository struct {
	mu          sync.RWMutex
	byID        map[string]domain.Person
	sourcedToID map[string]string
	now         func() time.Time
}

// NewPersonRepository returns an empty in-memory person repository.
func NewPersonRepository() *PersonRepository {
	return &PersonRepository{byID: map[string]domain.Person{}, sourcedToID: map[string]string{}, now: time.Now}
}

// WithClock replaces the clock the expiry filter reads (tests only) and
// returns r.
func (r *PersonRepository) WithClock(now func() time.Time) *PersonRepository {
	r.now = now
	return r
}

// live returns the stored person for id if it exists and has not expired.
func (r *PersonRepository) live(id string, now time.Time) (domain.Person, bool) {
	p, ok := r.byID[id]
	if !ok || domain.Expired(p.ExpiresAt, now) {
		return domain.Person{}, false
	}
	return p, true
}

// sourcedOwner returns the id of the stored person (live or expired) holding
// sourcedID.
func (r *PersonRepository) sourcedOwner(sourcedID string) (string, bool) {
	if sourcedID == "" {
		return "", false
	}
	id, ok := r.sourcedToID[sourcedID]
	return id, ok
}

// put stores p and indexes its sourcedId. Caller holds the write lock.
func (r *PersonRepository) put(p domain.Person) {
	if old, ok := r.byID[p.ID]; ok && old.SourcedID != p.SourcedID {
		r.unindex(old)
	}
	r.byID[p.ID] = clonePerson(p)
	if p.SourcedID != "" {
		r.sourcedToID[p.SourcedID] = p.ID
	}
}

// remove drops id and its sourcedId index entry. Caller holds the write lock.
func (r *PersonRepository) remove(id string) {
	if p, ok := r.byID[id]; ok {
		r.unindex(p)
		delete(r.byID, id)
	}
}

func (r *PersonRepository) unindex(p domain.Person) {
	if r.sourcedToID[p.SourcedID] == p.ID {
		delete(r.sourcedToID, p.SourcedID)
	}
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
	p.ExpiresAt = cloneTime(p.ExpiresAt)
	return p
}

// Create stores a new person, rejecting an id or sourcedId held by a live
// person. An expired holder of either key is replaced.
func (r *PersonRepository) Create(_ context.Context, p domain.Person) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if _, ok := r.live(p.ID, now); ok {
		return domain.ErrConflict
	}
	owner, owned := r.sourcedOwner(p.SourcedID)
	if owned {
		if _, ok := r.live(owner, now); ok {
			return domain.ErrConflict
		}
		r.remove(owner)
	}
	r.remove(p.ID)
	r.put(p)
	return nil
}

// Update replaces an existing live person, rejecting a sourcedId already held
// by a different live person (mirrors the Postgres UNIQUE constraint). An
// expired holder of the sourcedId is replaced. The write applies only while
// the stored expiry equals prevExpiresAt, else domain.ErrStale.
func (r *PersonRepository) Update(_ context.Context, p domain.Person, prevExpiresAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	old, ok := r.live(p.ID, now)
	if !ok {
		return domain.ErrNotFound
	}
	if !domain.SameExpiry(old.ExpiresAt, prevExpiresAt) {
		return domain.ErrStale
	}
	owner, owned := r.sourcedOwner(p.SourcedID)
	if owned && owner != p.ID {
		if _, ok := r.live(owner, now); ok {
			return domain.ErrConflict
		}
		r.remove(owner)
	}
	r.put(p)
	return nil
}

// Get returns a live person by id.
func (r *PersonRepository) Get(_ context.Context, id string) (domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.live(id, r.now())
	if !ok {
		return domain.Person{}, domain.ErrNotFound
	}
	return clonePerson(p), nil
}

// GetByIDs returns the live persons whose id is in ids.
func (r *PersonRepository) GetByIDs(_ context.Context, ids []string) ([]domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now()
	items := make([]domain.Person, 0, len(ids))
	for _, id := range ids {
		if p, ok := r.live(id, now); ok {
			items = append(items, clonePerson(p))
		}
	}
	return items, nil
}

// GetBySourcedID returns a live person by business key.
func (r *PersonRepository) GetBySourcedID(_ context.Context, sourcedID string) (domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.sourcedOwner(sourcedID)
	if !ok {
		return domain.Person{}, domain.ErrNotFound
	}
	p, ok := r.live(id, r.now())
	if !ok {
		return domain.Person{}, domain.ErrNotFound
	}
	return clonePerson(p), nil
}

// GetBySourcedIDs returns the live persons whose sourcedId is in sourcedIDs.
func (r *PersonRepository) GetBySourcedIDs(_ context.Context, sourcedIDs []string) ([]domain.Person, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now()
	items := make([]domain.Person, 0, len(sourcedIDs))
	for _, sourcedID := range sourcedIDs {
		id, ok := r.sourcedOwner(sourcedID)
		if !ok {
			continue
		}
		if p, ok := r.live(id, now); ok {
			items = append(items, clonePerson(p))
		}
	}
	return items, nil
}

// List returns a page of persons sorted by family name, given name, then id.
// The unique id tie-breaker keeps the order total, so pages are stable across
// calls even though map iteration is randomised.
func (r *PersonRepository) List(_ context.Context, params port.ListParams) (port.Page[domain.Person], error) {
	return r.list(nil, params)
}

// Search is List restricted to the persons matching every term, with the
// same matching rules as the Postgres repository (ILIKE '%term%').
func (r *PersonRepository) Search(_ context.Context, terms []string, params port.ListParams) (port.Page[domain.Person], error) {
	lowered := make([]string, len(terms))
	for i, term := range terms {
		lowered[i] = strings.ToLower(term)
	}
	return r.list(func(p domain.Person) bool {
		for _, term := range lowered {
			if !personMatches(p, term) {
				return false
			}
		}
		return true
	}, params)
}

// personMatches reports whether the lower-case term occurs in one of the
// searched fields of p.
func personMatches(p domain.Person, term string) bool {
	fields := []string{p.GivenName, p.MiddleName, p.FamilyName, p.ID, p.SourcedID}
	for _, identifier := range p.Identifiers {
		fields = append(fields, identifier.Value)
	}
	for _, email := range p.Emails {
		fields = append(fields, email.Value)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), term) {
			return true
		}
	}
	return false
}

// ListByRoles is List restricted to the persons holding one of roles.
func (r *PersonRepository) ListByRoles(_ context.Context, roles []domain.Role, params port.ListParams) (port.Page[domain.Person], error) {
	return r.list(func(p domain.Person) bool {
		return slices.ContainsFunc(p.Roles, func(role domain.Role) bool { return slices.Contains(roles, role) })
	}, params)
}

// personKeys are the keys personKey knows, those the Postgres person
// repository maps to columns (see personColumn).
var personKeys = []string{"sourcedId", "status", "givenName", "familyName", "middleName", "dateOfBirth"}

// personKey returns the scalar person property stored under key, and false
// when the person has no value for it: an empty optional property is absent.
func personKey(p domain.Person, key string) (string, bool) {
	switch key {
	case "sourcedId":
		return p.SourcedID, true
	case "status":
		return string(p.Status), true
	case "givenName":
		return p.GivenName, true
	case "familyName":
		return p.FamilyName, true
	case "middleName":
		return p.MiddleName, p.MiddleName != ""
	case "dateOfBirth":
		return p.DateOfBirth, p.DateOfBirth != ""
	}
	return "", false
}

// list pages the live persons keep accepts (all when keep is nil) and
// params.Filter matches, ordered by params.Sort, then by name.
func (r *PersonRepository) list(keep func(domain.Person) bool, params port.ListParams) (port.Page[domain.Person], error) {
	if params.Sort != "" {
		if !slices.Contains(personKeys, params.Sort) {
			return port.Page[domain.Person]{}, fmt.Errorf("memory: unknown person sort key %q", params.Sort)
		}
	}
	if params.Filter != nil {
		for _, term := range params.Filter.Terms {
			if !slices.Contains(personKeys, term.Field) {
				return port.Page[domain.Person]{}, fmt.Errorf("memory: unknown person filter key %q", term.Field)
			}
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now()
	items := make([]domain.Person, 0, len(r.byID))
	for _, p := range r.byID {
		if domain.Expired(p.ExpiresAt, now) || (keep != nil && !keep(p)) {
			continue
		}
		if !params.Filter.Match(func(field string) (string, bool) { return personKey(p, field) }) {
			continue
		}
		items = append(items, clonePerson(p))
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if params.Sort != "" {
			av, aok := personKey(a, params.Sort)
			bv, bok := personKey(b, params.Sort)
			if aok != bok {
				return aok // a person without the key goes last in both directions
			}
			if av != bv {
				if params.Desc {
					return av > bv
				}
				return av < bv
			}
		}
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

// Delete removes a live person by id.
func (r *PersonRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.live(id, r.now()); !ok {
		return domain.ErrNotFound
	}
	r.remove(id)
	return nil
}

// DeleteExpired removes every person expired at now and returns their ids.
func (r *PersonRepository) DeleteExpired(_ context.Context, now time.Time) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for id, p := range r.byID {
		if domain.Expired(p.ExpiresAt, now) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		r.remove(id)
	}
	return ids, nil
}
