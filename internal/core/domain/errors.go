package domain

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write violates a uniqueness constraint.
var ErrConflict = errors.New("conflict")

// ErrStale is returned by a conditional write when the stored record changed
// after the caller read it, so the write was not applied. It wraps
// ErrConflict, so callers that do not retry report it as a conflict (409).
var ErrStale = fmt.Errorf("%w: record changed since it was read", ErrConflict)

// ValidationError describes a single invalid field on an entity.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Msg)
}

// Messages for ValidationError.Msg that recur across entities.
const (
	MsgRequired          = "required"
	MsgRequiredForUpsert = "required for upsert"
)

// Required reports a missing mandatory field.
func Required(field string) *ValidationError {
	return &ValidationError{Field: field, Msg: MsgRequired}
}
