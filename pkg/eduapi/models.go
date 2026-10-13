// Package eduapi holds Edu-API v1.0 (OneRoster-lineage) read DTOs and mappers
// from the canonical domain. Written from the spec shapes; no external
// dependency. Field names follow the standard (sourcedId, dateLastModified,
// orgs/classes/enrollments, GUIDRef).
package eduapi

import (
	"time"

	"github.com/SUNET/sis/internal/core/domain"
)

// GUIDRef is a typed reference to another resource by sourcedId.
type GUIDRef struct {
	SourcedID string `json:"sourcedId"`
	Type      string `json:"type"`
}

// Person is an Edu-API person.
type Person struct {
	SourcedID        string   `json:"sourcedId"`
	Status           string   `json:"status,omitempty"`
	DateLastModified string   `json:"dateLastModified,omitempty"`
	GivenName        string   `json:"givenName"`
	FamilyName       string   `json:"familyName"`
	MiddleName       string   `json:"middleName,omitempty"`
	Email            string   `json:"email,omitempty"`
	Roles            []Role   `json:"roles,omitempty"`
	UserIDs          []UserID `json:"userIds,omitempty"`
}

// Role is a person's role entry.
type Role struct {
	Role string `json:"role"`
}

// UserID is an external identifier.
type UserID struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

// Org is an Edu-API organization.
type Org struct {
	SourcedID        string   `json:"sourcedId"`
	Status           string   `json:"status,omitempty"`
	DateLastModified string   `json:"dateLastModified,omitempty"`
	Name             string   `json:"name"`
	Type             string   `json:"type,omitempty"`
	Parent           *GUIDRef `json:"parent,omitempty"`
}

// AcademicSession is an Edu-API academic session (term/year).
type AcademicSession struct {
	SourcedID        string `json:"sourcedId"`
	Status           string `json:"status,omitempty"`
	DateLastModified string `json:"dateLastModified,omitempty"`
	Title            string `json:"title"`
	Type             string `json:"type,omitempty"`
	StartDate        string `json:"startDate,omitempty"`
	EndDate          string `json:"endDate,omitempty"`
}

// Course is an Edu-API course.
type Course struct {
	SourcedID        string   `json:"sourcedId"`
	Status           string   `json:"status,omitempty"`
	DateLastModified string   `json:"dateLastModified,omitempty"`
	Title            string   `json:"title"`
	CourseCode       string   `json:"courseCode,omitempty"`
	Org              *GUIDRef `json:"org,omitempty"`
}

// Class is an Edu-API class (a course scheduled in a session — our Offering).
type Class struct {
	SourcedID        string    `json:"sourcedId"`
	Status           string    `json:"status,omitempty"`
	DateLastModified string    `json:"dateLastModified,omitempty"`
	Title            string    `json:"title,omitempty"`
	Course           *GUIDRef  `json:"course,omitempty"`
	Terms            []GUIDRef `json:"terms,omitempty"`
	School           *GUIDRef  `json:"school,omitempty"`
}

// Enrollment is an Edu-API enrollment (person ↔ class).
type Enrollment struct {
	SourcedID        string  `json:"sourcedId"`
	Status           string  `json:"status,omitempty"`
	DateLastModified string  `json:"dateLastModified,omitempty"`
	User             GUIDRef `json:"user"`
	Class            GUIDRef `json:"class"`
	Role             string  `json:"role,omitempty"`
}

// FromPerson maps a canonical person to the Edu-API shape.
func FromPerson(p domain.Person) Person {
	roles := make([]Role, 0, len(p.Roles))
	for _, r := range p.Roles {
		roles = append(roles, Role{Role: string(r)})
	}
	ids := make([]UserID, 0, len(p.Identifiers))
	for _, id := range p.Identifiers {
		ids = append(ids, UserID{Type: id.Type, Identifier: id.Value})
	}
	var email string
	if len(p.Emails) > 0 {
		email = p.Emails[0].Value
	}
	return Person{
		SourcedID: p.SourcedID, Status: string(p.Status), DateLastModified: rfc3339(p.UpdatedAt),
		GivenName: p.GivenName, FamilyName: p.FamilyName, MiddleName: p.MiddleName,
		Email: email, Roles: roles, UserIDs: ids,
	}
}

// FromOrg maps a canonical organization.
func FromOrg(o *domain.Organization) Org {
	return Org{
		SourcedID: o.SourcedID, Status: string(o.Status), DateLastModified: rfc3339(o.UpdatedAt),
		Name: o.Name, Type: o.Type, Parent: ref(o.ParentID, "org"),
	}
}

// FromAcademicSession maps a canonical academic session.
func FromAcademicSession(a *domain.AcademicSession) AcademicSession {
	return AcademicSession{
		SourcedID: a.SourcedID, Status: string(a.Status), DateLastModified: rfc3339(a.UpdatedAt),
		Title: a.Title, Type: a.Type, StartDate: a.StartDate, EndDate: a.EndDate,
	}
}

// FromCourse maps a canonical course.
func FromCourse(c *domain.Course) Course {
	return Course{
		SourcedID: c.SourcedID, Status: string(c.Status), DateLastModified: rfc3339(c.UpdatedAt),
		Title: c.Title, CourseCode: c.Code, Org: ref(c.OrgID, "org"),
	}
}

// FromClass maps a canonical offering to an Edu-API class.
func FromClass(o *domain.Offering) Class {
	var terms []GUIDRef
	if o.AcademicSessionID != "" {
		terms = []GUIDRef{{SourcedID: o.AcademicSessionID, Type: "academicSession"}}
	}
	return Class{
		SourcedID: o.SourcedID, Status: string(o.Status), DateLastModified: rfc3339(o.UpdatedAt),
		Title: o.Title, Course: ref(o.CourseID, "course"), Terms: terms, School: ref(o.OrgID, "org"),
	}
}

// FromEnrollment maps a canonical enrollment.
func FromEnrollment(e *domain.Enrollment) Enrollment {
	return Enrollment{
		SourcedID: e.SourcedID, Status: string(e.Status), DateLastModified: rfc3339(e.UpdatedAt),
		User:  GUIDRef{SourcedID: e.PersonID, Type: "user"},
		Class: GUIDRef{SourcedID: e.OfferingID, Type: "class"},
		Role:  string(e.Role),
	}
}

func ref(id, typ string) *GUIDRef {
	if id == "" {
		return nil
	}
	return &GUIDRef{SourcedID: id, Type: typ}
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
