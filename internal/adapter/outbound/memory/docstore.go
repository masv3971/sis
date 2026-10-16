package memory

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// doc is one stored entity: its JSON plus the promoted columns the Postgres
// doc store keeps outside the JSONB (sourcedId and expiry), so lookups and
// the expiry filter never have to decode.
type doc struct {
	data      []byte
	sourcedID string
	expiresAt *time.Time
}

// DocRepo is an in-memory generic port.Repository storing each entity as JSON,
// mirroring the Postgres doc store so behaviour matches across backends,
// including expiry (see port.Repository).
type DocRepo[T any, PT domain.EntityPtr[T]] struct {
	mu          sync.RWMutex
	byID        map[string]doc
	sourcedToID map[string]string
	now         func() time.Time
}

// NewDocRepo returns an empty in-memory doc repository.
func NewDocRepo[T any, PT domain.EntityPtr[T]]() *DocRepo[T, PT] {
	return &DocRepo[T, PT]{byID: map[string]doc{}, sourcedToID: map[string]string{}, now: time.Now}
}

// WithClock replaces the clock the expiry filter reads (tests only) and
// returns r.
func (r *DocRepo[T, PT]) WithClock(now func() time.Time) *DocRepo[T, PT] {
	r.now = now
	return r
}

// live returns the stored doc for id if it exists and has not expired.
func (r *DocRepo[T, PT]) live(id string, now time.Time) (doc, bool) {
	d, ok := r.byID[id]
	if !ok || domain.Expired(d.expiresAt, now) {
		return doc{}, false
	}
	return d, true
}

// remove drops id and its sourcedId index entry. Caller holds the write lock.
func (r *DocRepo[T, PT]) remove(id string) {
	d, ok := r.byID[id]
	if !ok {
		return
	}
	if r.sourcedToID[d.sourcedID] == id {
		delete(r.sourcedToID, d.sourcedID)
	}
	delete(r.byID, id)
}

// Create stores a new entity, rejecting an id or sourcedId held by a live
// entity. An expired holder of either key is replaced.
func (r *DocRepo[T, PT]) Create(_ context.Context, e PT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if _, ok := r.live(e.GetID(), now); ok {
		return domain.ErrConflict
	}
	sid := e.GetSourcedID()
	owner, owned := r.sourcedToID[sid]
	if sid != "" && owned {
		if _, ok := r.live(owner, now); ok {
			return domain.ErrConflict
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	// Point of no return: drop expired key holders, then insert.
	r.remove(e.GetID())
	if sid != "" && owned {
		r.remove(owner)
	}
	r.byID[e.GetID()] = doc{data: data, sourcedID: sid, expiresAt: e.GetExpiresAt()}
	if sid != "" {
		r.sourcedToID[sid] = e.GetID()
	}
	return nil
}

// Update replaces an existing live entity, maintaining the sourcedId index. A
// sourcedId already owned by a different live entity is rejected with
// domain.ErrConflict, matching Create and the Postgres UNIQUE constraint; an
// expired owner is replaced.
//
// Every check that can fail (lookup, conflict, marshal) runs before either
// map is touched, so a failed Update leaves the stored record and its
// sourcedId index exactly as they were.
func (r *DocRepo[T, PT]) Update(_ context.Context, e PT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	old, ok := r.live(e.GetID(), now)
	if !ok {
		return domain.ErrNotFound
	}
	sid := e.GetSourcedID()
	owner, owned := r.sourcedToID[sid]
	if sid != "" && owned && owner != e.GetID() {
		if _, ok := r.live(owner, now); ok {
			return domain.ErrConflict
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	// Point of no return: mutate the index and the record together.
	if sid != "" && owned && owner != e.GetID() {
		r.remove(owner)
	}
	if old.sourcedID != sid && r.sourcedToID[old.sourcedID] == e.GetID() {
		delete(r.sourcedToID, old.sourcedID)
	}
	r.byID[e.GetID()] = doc{data: data, sourcedID: sid, expiresAt: e.GetExpiresAt()}
	if sid != "" {
		r.sourcedToID[sid] = e.GetID()
	}
	return nil
}

// Get returns a live entity by id.
func (r *DocRepo[T, PT]) Get(_ context.Context, id string) (PT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.live(id, r.now())
	if !ok {
		return nil, domain.ErrNotFound
	}
	return unmarshalDoc[T, PT](d.data)
}

// GetByIDs returns the live entities whose id is in ids.
func (r *DocRepo[T, PT]) GetByIDs(_ context.Context, ids []string) ([]PT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now()
	items := make([]PT, 0, len(ids))
	for _, id := range ids {
		d, ok := r.live(id, now)
		if !ok {
			continue
		}
		e, err := unmarshalDoc[T, PT](d.data)
		if err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, nil
}

// GetBySourcedID returns a live entity by business key.
func (r *DocRepo[T, PT]) GetBySourcedID(_ context.Context, sourcedID string) (PT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.sourcedToID[sourcedID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	d, ok := r.live(id, r.now())
	if !ok {
		return nil, domain.ErrNotFound
	}
	return unmarshalDoc[T, PT](d.data)
}

// List returns a page of live entities ordered by sourcedId.
func (r *DocRepo[T, PT]) List(_ context.Context, params port.ListParams) (port.Page[PT], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now()
	items := make([]PT, 0, len(r.byID))
	for _, d := range r.byID {
		if domain.Expired(d.expiresAt, now) {
			continue
		}
		e, err := unmarshalDoc[T, PT](d.data)
		if err != nil {
			return port.Page[PT]{}, err
		}
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GetSourcedID() < items[j].GetSourcedID() })

	lo, hi := pageBounds(len(items), params.Offset, params.Limit)
	return port.Page[PT]{Items: items[lo:hi], Total: len(items)}, nil
}

// Delete removes a live entity by id.
func (r *DocRepo[T, PT]) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.live(id, r.now()); !ok {
		return domain.ErrNotFound
	}
	r.remove(id)
	return nil
}

// DeleteExpired removes every entity expired at now and returns their ids.
func (r *DocRepo[T, PT]) DeleteExpired(_ context.Context, now time.Time) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for id, d := range r.byID {
		if domain.Expired(d.expiresAt, now) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		r.remove(id)
	}
	return ids, nil
}

func unmarshalDoc[T any, PT domain.EntityPtr[T]](data []byte) (PT, error) {
	e := PT(new(T))
	if err := json.Unmarshal(data, e); err != nil {
		return nil, err
	}
	return e, nil
}
