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
	// DateOfBirth is an ISO 8601 calendar date (YYYY-MM-DD), or empty.
	DateOfBirth string    `json:"dateOfBirth,omitempty" format:"date" example:"2001-03-14"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// ExpiresAt: see Base.ExpiresAt.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// EntityType returns the stable type tag.
func (*Person) EntityType() string { return "person" }

// QuerySpec makes persons searchable by name, id, sourcedId and identifier
// and email values, and lists them by name.
func (*Person) QuerySpec() QuerySpec {
	return QuerySpec{
		SearchFields:      []string{"givenName", "middleName", "familyName", "id", "sourcedId"},
		SearchValueArrays: []string{"identifiers", "emails"},
		DefaultOrder:      []string{"familyName", "givenName"},
	}
}

// Person does not embed Base: its status is required, not optional.

func (p *Person) GetID() string            { return p.ID }
func (p *Person) SetID(s string)           { p.ID = s }
func (p *Person) GetSourcedID() string     { return p.SourcedID }
func (p *Person) SetSourcedID(s string)    { p.SourcedID = s }
func (p *Person) GetCreatedAt() time.Time  { return p.CreatedAt }
func (p *Person) SetCreatedAt(t time.Time) { p.CreatedAt = t }
func (p *Person) GetUpdatedAt() time.Time  { return p.UpdatedAt }
func (p *Person) SetUpdatedAt(t time.Time) { p.UpdatedAt = t }

func (p *Person) GetExpiresAt() *time.Time  { return p.ExpiresAt }
func (p *Person) SetExpiresAt(t *time.Time) { p.ExpiresAt = t }

// Validate checks required invariants for a Person.
func (p *Person) Validate() error {
	if p.GivenName == "" {
		return Required("givenName")
	}
	if p.FamilyName == "" {
		return Required("familyName")
	}
	if p.Status == "" {
		return Required("status")
	}
	if !p.Status.Valid() {
		return &ValidationError{Field: "status", Msg: statusMsg}
	}
	if p.DateOfBirth != "" && !ValidDate(p.DateOfBirth) {
		return &ValidationError{Field: "dateOfBirth", Msg: MsgISODate}
	}
	return nil
}

// ValidDate reports whether s is an ISO 8601 calendar date in the extended
// format, YYYY-MM-DD, that exists (2001-02-29 does not).
func ValidDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}
