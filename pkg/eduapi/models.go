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
func FromPerson(person domain.Person) Person {
	roles := make([]Role, 0, len(person.Roles))
	for _, role := range person.Roles {
		roles = append(roles, Role{Role: string(role)})
	}
	ids := make([]UserID, 0, len(person.Identifiers))
	for _, id := range person.Identifiers {
		ids = append(ids, UserID{Type: id.Type, Identifier: id.Value})
	}
	var email string
	if len(person.Emails) > 0 {
		email = person.Emails[0].Value
	}
	return Person{
		SourcedID: person.SourcedID, Status: string(person.Status), DateLastModified: rfc3339(person.UpdatedAt),
		GivenName: person.GivenName, FamilyName: person.FamilyName, MiddleName: person.MiddleName,
		Email: email, Roles: roles, UserIDs: ids,
	}
}

// The relationship mappers below copy reference fields (ParentID, OrgID,
// CourseID, AcademicSessionID, PersonID, OfferingID) verbatim into GUIDRefs,
// which are sourcedIds. Callers must first rewrite those fields to the
// referenced records' sourcedIds (readmodel.Client.PublishRefs).

// FromOrg maps a canonical organization.
func FromOrg(org *domain.Organization) Org {
	return Org{
		SourcedID: org.SourcedID, Status: string(org.Status), DateLastModified: rfc3339(org.UpdatedAt),
		Name: org.Name, Type: org.Type, Parent: ref(org.ParentID, "org"),
	}
}

// FromAcademicSession maps a canonical academic session.
func FromAcademicSession(session *domain.AcademicSession) AcademicSession {
	return AcademicSession{
		SourcedID: session.SourcedID, Status: string(session.Status), DateLastModified: rfc3339(session.UpdatedAt),
		Title: session.Title, Type: session.Type, StartDate: session.StartDate, EndDate: session.EndDate,
	}
}

// FromCourse maps a canonical course.
func FromCourse(course *domain.Course) Course {
	return Course{
		SourcedID: course.SourcedID, Status: string(course.Status), DateLastModified: rfc3339(course.UpdatedAt),
		Title: course.Title, CourseCode: course.Code, Org: ref(course.OrgID, "org"),
	}
}

// FromClass maps a canonical offering to an Edu-API class.
func FromClass(offering *domain.Offering) Class {
	var terms []GUIDRef
	if offering.AcademicSessionID != "" {
		terms = []GUIDRef{{SourcedID: offering.AcademicSessionID, Type: "academicSession"}}
	}
	return Class{
		SourcedID: offering.SourcedID, Status: string(offering.Status), DateLastModified: rfc3339(offering.UpdatedAt),
		Title: offering.Title, Course: ref(offering.CourseID, "course"), Terms: terms, School: ref(offering.OrgID, "org"),
	}
}

// FromEnrollment maps a canonical enrollment.
func FromEnrollment(enrollment *domain.Enrollment) Enrollment {
	return Enrollment{
		SourcedID: enrollment.SourcedID, Status: string(enrollment.Status), DateLastModified: rfc3339(enrollment.UpdatedAt),
		User:  GUIDRef{SourcedID: enrollment.PersonID, Type: "user"},
		Class: GUIDRef{SourcedID: enrollment.OfferingID, Type: "class"},
		Role:  string(enrollment.Role),
	}
}

// FromProgram maps a canonical program.
func FromProgram(program *domain.Program) Program {
	return Program{
		SourcedID: program.SourcedID, Status: string(program.Status), DateLastModified: rfc3339(program.UpdatedAt),
		Title: program.Title, ProgramCode: program.Code, Level: program.Level, Org: ref(program.OrgID, "org"),
	}
}

// FromResult maps a canonical result.
func FromResult(result *domain.Result) Result {
	return Result{
		SourcedID: result.SourcedID, Status: string(result.Status), DateLastModified: rfc3339(result.UpdatedAt),
		Student: GUIDRef{SourcedID: result.PersonID, Type: "user"},
		Course:  ref(result.CourseID, "course"), Program: ref(result.ProgramID, "program"), Class: ref(result.OfferingID, "class"),
		Score: result.Grade, Credits: result.Credits, ScoreDate: result.ResultDate,
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
	for _, person := range b.Persons {
		out.Persons = append(out.Persons, FromPerson(person))
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
