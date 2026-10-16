package domain

import "time"

// Status is the lifecycle state of an entity. The values are the ones OEAPI
// and Edu-API (OneRoster) use, so outbound adapters map status without a table.
type Status string

const (
	StatusActive      Status = "active"
	StatusInactive    Status = "inactive"
	StatusToBeDeleted Status = "tobedeleted"
)

// Valid reports whether s is one of the declared lifecycle states.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusInactive, StatusToBeDeleted:
		return true
	}
	return false
}

const statusMsg = "must be one of active, inactive, tobedeleted"

// Role is a person's role within the institution.
type Role string

const (
	RoleStudent Role = "student"
	RoleStaff   Role = "staff"
	RoleFaculty Role = "faculty"
	RoleGuest   Role = "guest"
)

// Identifier is an external/business identifier (e.g. student number, national
// id), kept as a typed pair so each standard selects the kind it needs.
type Identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Contact is a typed contact value (email or phone).
type Contact struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Person is the canonical person/student entity: storage- and
// protocol-independent. Inbound adapters build it; outbound adapters map it to
// OEAPI/Edu-API DTOs.
type Person struct {
	ID          string       `json:"id"`
	SourcedID   string       `json:"sourcedId"`
	Status      Status       `json:"status"`
	GivenName   string       `json:"givenName"`
	FamilyName  string       `json:"familyName"`
	MiddleName  string       `json:"middleName,omitempty"`
	Roles       []Role       `json:"roles,omitempty"`
	Identifiers []Identifier `json:"identifiers,omitempty"`
	Emails      []Contact    `json:"emails,omitempty"`
	Phones      []Contact    `json:"phones,omitempty"`
	DateOfBirth string       `json:"dateOfBirth,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
	// ExpiresAt: see Base.ExpiresAt.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Validate checks required invariants for a Person.
func (p Person) Validate() error {
	if p.GivenName == "" {
		return &ValidationError{Field: "givenName", Msg: "required"}
	}
	if p.FamilyName == "" {
		return &ValidationError{Field: "familyName", Msg: "required"}
	}
	if p.Status == "" {
		return &ValidationError{Field: "status", Msg: "required"}
	}
	if !p.Status.Valid() {
		return &ValidationError{Field: "status", Msg: statusMsg}
	}
	return nil
}
