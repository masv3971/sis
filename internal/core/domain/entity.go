package domain

import "time"

// Entity is the behaviour the generic repository/service layer needs from any
// canonical aggregate: identity, business key, timestamps, type tag, validation.
type Entity interface {
	GetID() string
	SetID(string)
	GetSourcedID() string
	SetSourcedID(string)
	GetCreatedAt() time.Time
	SetCreatedAt(time.Time)
	GetUpdatedAt() time.Time
	SetUpdatedAt(time.Time)
	GetExpiresAt() *time.Time
	SetExpiresAt(*time.Time)
	EntityType() string
	Validate() error
}

// EntityPtr constrains a pointer type *T that implements Entity, so the generic
// store and service can allocate and mutate values of T.
type EntityPtr[T any] interface {
	*T
	Entity
}

// Base provides the common identity/timestamp fields and accessors for entities
// to embed. EntityType and Validate are supplied by each concrete entity.
type Base struct {
	ID        string    `json:"id"`
	SourcedID string    `json:"sourcedId"`
	Status    Status    `json:"status,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// ExpiresAt is when the record stops being served and becomes eligible
	// for deletion; nil means it never expires. Server-managed: only
	// POST /api/v1/populate with a ttl sets it, and /api/v1 writes ignore it.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Expired reports whether a record with the given expiry is expired at now.
// A nil expiry never expires. The boundary is inclusive (expired at exactly
// expiresAt), matching the repositories' `expires_at > now()` read filter.
func Expired(expiresAt *time.Time, now time.Time) bool {
	return expiresAt != nil && !now.Before(*expiresAt)
}

// SameExpiry reports whether a and b are the same expiry: both nil
// (permanent) or both set to the same instant.
func SameExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// validateStatus rejects a status outside the declared states. Status is
// optional on entities, so empty is allowed.
func (b *Base) validateStatus() error {
	if b.Status != "" && !b.Status.Valid() {
		return &ValidationError{Field: "status", Msg: statusMsg}
	}
	return nil
}

func (b *Base) GetID() string            { return b.ID }
func (b *Base) SetID(s string)           { b.ID = s }
func (b *Base) GetSourcedID() string     { return b.SourcedID }
func (b *Base) SetSourcedID(s string)    { b.SourcedID = s }
func (b *Base) GetCreatedAt() time.Time  { return b.CreatedAt }
func (b *Base) SetCreatedAt(t time.Time) { b.CreatedAt = t }
func (b *Base) GetUpdatedAt() time.Time  { return b.UpdatedAt }
func (b *Base) SetUpdatedAt(t time.Time) { b.UpdatedAt = t }

func (b *Base) GetExpiresAt() *time.Time  { return b.ExpiresAt }
func (b *Base) SetExpiresAt(t *time.Time) { b.ExpiresAt = t }
