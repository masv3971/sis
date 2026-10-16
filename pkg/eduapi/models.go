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

// Program is an academic program (a structured set of courses leading to a
// qualification). Edu-API 1.0 has no first-class program resource; the shape
// follows the 1EdTech conventions used by the other resources here.
type Program struct {
	SourcedID        string   `json:"sourcedId"`
	Status           string   `json:"status,omitempty"`
	DateLastModified string   `json:"dateLastModified,omitempty"`
	Title            string   `json:"title"`
	ProgramCode      string   `json:"programCode,omitempty"`
	Level            string   `json:"level,omitempty"`
	Org              *GUIDRef `json:"org,omitempty"`
}

// Result is a person's outcome for a course or program (OneRoster lineage:
// score/scoreDate), extended with the program link and credits.
type Result struct {
	SourcedID        string   `json:"sourcedId"`
	Status           string   `json:"status,omitempty"`
	DateLastModified string   `json:"dateLastModified,omitempty"`
	Student          GUIDRef  `json:"student"`
	Course           *GUIDRef `json:"course,omitempty"`
	Program          *GUIDRef `json:"program,omitempty"`
	Class            *GUIDRef `json:"class,omitempty"`
	Score            string   `json:"score,omitempty"`
	Credits          float64  `json:"credits,omitempty"`
	ScoreDate        string   `json:"scoreDate,omitempty"`
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

// The relationship mappers below copy reference fields (ParentID, OrgID,
// CourseID, AcademicSessionID, PersonID, OfferingID) verbatim into GUIDRefs,
// which are sourcedIds. Callers must first rewrite those fields to the
// referenced records' sourcedIds (readmodel.Client.PublishRefs).

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

// FromProgram maps a canonical program.
func FromProgram(p *domain.Program) Program {
	return Program{
		SourcedID: p.SourcedID, Status: string(p.Status), DateLastModified: rfc3339(p.UpdatedAt),
		Title: p.Title, ProgramCode: p.Code, Level: p.Level, Org: ref(p.OrgID, "org"),
	}
}

// FromResult maps a canonical result.
func FromResult(r *domain.Result) Result {
	return Result{
		SourcedID: r.SourcedID, Status: string(r.Status), DateLastModified: rfc3339(r.UpdatedAt),
		Student: GUIDRef{SourcedID: r.PersonID, Type: "user"},
		Course:  ref(r.CourseID, "course"), Program: ref(r.ProgramID, "program"), Class: ref(r.OfferingID, "class"),
		Score: r.Grade, Credits: r.Credits, ScoreDate: r.ResultDate,
	}
}

// Bundle is a set of converted records in Edu-API shape, one array per
// resource collection, keyed like the provider's list responses.
type Bundle struct {
	Persons          []Person          `json:"persons"`
	Orgs             []Org             `json:"orgs"`
	AcademicSessions []AcademicSession `json:"academicSessions"`
	Courses          []Course          `json:"courses"`
	Programs         []Program         `json:"programs"`
	Classes          []Class           `json:"classes"`
	Enrollments      []Enrollment      `json:"enrollments"`
	Results          []Result          `json:"results"`
}

// FromBundle maps a converted domain.Bundle, whose references are already
// sourcedIds, to Edu-API shape.
func FromBundle(b *domain.Bundle) Bundle {
	out := Bundle{
		Persons:          make([]Person, 0, len(b.Persons)),
		Orgs:             mapAll(b.Organizations, FromOrg),
		AcademicSessions: mapAll(b.AcademicSessions, FromAcademicSession),
		Courses:          mapAll(b.Courses, FromCourse),
		Programs:         mapAll(b.Programs, FromProgram),
		Classes:          mapAll(b.Offerings, FromClass),
		Enrollments:      mapAll(b.Enrollments, FromEnrollment),
		Results:          mapAll(b.Results, FromResult),
	}
	for _, p := range b.Persons {
		out.Persons = append(out.Persons, FromPerson(p))
	}
	return out
}

func mapAll[E, D any](in []E, fn func(E) D) []D {
	out := make([]D, 0, len(in))
	for _, e := range in {
		out = append(out, fn(e))
	}
	return out
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
