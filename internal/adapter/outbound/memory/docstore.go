package memory

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// DocRepo is an in-memory generic port.Repository storing each entity as JSON,
// mirroring the Postgres doc store so behaviour matches across backends.
type DocRepo[T any, PT domain.EntityPtr[T]] struct {
	mu          sync.RWMutex
	byID        map[string][]byte
	sourcedToID map[string]string
}

// NewDocRepo returns an empty in-memory doc repository.
func NewDocRepo[T any, PT domain.EntityPtr[T]]() *DocRepo[T, PT] {
	return &DocRepo[T, PT]{byID: map[string][]byte{}, sourcedToID: map[string]string{}}
}

// Create stores a new entity, rejecting duplicate id or sourcedId.
func (r *DocRepo[T, PT]) Create(_ context.Context, e PT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[e.GetID()]; ok {
		return domain.ErrConflict
	}
	if sid := e.GetSourcedID(); sid != "" {
		if _, ok := r.sourcedToID[sid]; ok {
			return domain.ErrConflict
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	r.byID[e.GetID()] = data
	if sid := e.GetSourcedID(); sid != "" {
		r.sourcedToID[sid] = e.GetID()
	}
	return nil
}

// Update replaces an existing entity, maintaining the sourcedId index.
func (r *DocRepo[T, PT]) Update(_ context.Context, e PT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.byID[e.GetID()]
	if !ok {
		return domain.ErrNotFound
	}
	if prev, err := unmarshalDoc[T, PT](old); err == nil {
		if oldSID := prev.GetSourcedID(); oldSID != e.GetSourcedID() {
			delete(r.sourcedToID, oldSID)
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	r.byID[e.GetID()] = data
	if sid := e.GetSourcedID(); sid != "" {
		r.sourcedToID[sid] = e.GetID()
	}
	return nil
}

// Get returns an entity by id.
func (r *DocRepo[T, PT]) Get(_ context.Context, id string) (PT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	data, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return unmarshalDoc[T, PT](data)
}

// GetBySourcedID returns an entity by business key.
func (r *DocRepo[T, PT]) GetBySourcedID(_ context.Context, sourcedID string) (PT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.sourcedToID[sourcedID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return unmarshalDoc[T, PT](r.byID[id])
}

// List returns a page of entities ordered by sourcedId.
func (r *DocRepo[T, PT]) List(_ context.Context, params port.ListParams) (port.Page[PT], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]PT, 0, len(r.byID))
	for _, data := range r.byID {
		e, err := unmarshalDoc[T, PT](data)
		if err != nil {
			return port.Page[PT]{}, err
		}
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GetSourcedID() < items[j].GetSourcedID() })

	total := len(items)
	lo := params.Offset
	if lo > total {
		lo = total
	}
	hi := total
	if params.Limit > 0 && lo+params.Limit < hi {
		hi = lo + params.Limit
	}
	return port.Page[PT]{Items: items[lo:hi], Total: total}, nil
}

// Delete removes an entity by id.
func (r *DocRepo[T, PT]) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if e, err := unmarshalDoc[T, PT](data); err == nil {
		delete(r.sourcedToID, e.GetSourcedID())
	}
	delete(r.byID, id)
	return nil
}

func unmarshalDoc[T any, PT domain.EntityPtr[T]](data []byte) (PT, error) {
	e := PT(new(T))
	if err := json.Unmarshal(data, e); err != nil {
		return nil, err
	}
	return e, nil
}
