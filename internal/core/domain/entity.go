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
