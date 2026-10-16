// Package oeapi holds Open Education API v6.0 read DTOs and mappers from the
// canonical domain, plus the OEAPI page envelope. Written from the spec shapes;
// no external dependency. Simplified subset (plain-string names rather than the
// language-typed arrays) sufficient for the read provider.
package oeapi

import "github.com/SUNET/sis/internal/core/domain"

// Page is the OEAPI pagination envelope.
type Page[T any] struct {
	PageSize        int  `json:"pageSize"`
	PageNumber      int  `json:"pageNumber"`
	TotalPages      int  `json:"totalPages"`
	HasPreviousPage bool `json:"hasPreviousPage"`
	HasNextPage     bool `json:"hasNextPage"`
	Items           []T  `json:"items"`
}

// NewPage builds an OEAPI page envelope from items and paging state.
func NewPage[T any](items []T, pageNumber, pageSize, total int) Page[T] {
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return Page[T]{
		PageSize:        pageSize,
		PageNumber:      pageNumber,
		TotalPages:      totalPages,
		HasPreviousPage: pageNumber > 1,
		HasNextPage:     pageNumber < totalPages,
		Items:           items,
	}
}

// Person is an OEAPI person.
type Person struct {
	PersonID    string `json:"personId"`
	GivenName   string `json:"givenName,omitempty"`
	Surname     string `json:"surname,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Mail        string `json:"mail,omitempty"`
}

// Organization is an OEAPI organization.
type Organization struct {
	OrganizationID     string `json:"organizationId"`
	Name               string `json:"name,omitempty"`
	Type               string `json:"type,omitempty"`
	ParentOrganization string `json:"parentOrganization,omitempty"`
}

// AcademicSession is an OEAPI academic session.
type AcademicSession struct {
	AcademicSessionID string `json:"academicSessionId"`
	Name              string `json:"name,omitempty"`
	Type              string `json:"type,omitempty"`
	StartDate         string `json:"startDate,omitempty"`
	EndDate           string `json:"endDate,omitempty"`
}

// Course is an OEAPI course.
type Course struct {
	CourseID       string `json:"courseId"`
	Name           string `json:"name,omitempty"`
	Abbreviation   string `json:"abbreviation,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
}

// Offering is an OEAPI offering.
type Offering struct {
	OfferingID        string `json:"offeringId"`
	Name              string `json:"name,omitempty"`
	CourseID          string `json:"courseId,omitempty"`
	AcademicSessionID string `json:"academicSessionId,omitempty"`
}

// Association is an OEAPI association (our Enrollment).
type Association struct {
	AssociationID string `json:"associationId"`
	PersonID      string `json:"personId"`
	OfferingID    string `json:"offeringId"`
	Role          string `json:"role,omitempty"`
	State         string `json:"state,omitempty"`
}

// Program is an OEAPI program.
type Program struct {
	ProgramID            string `json:"programId"`
	Name                 string `json:"name,omitempty"`
	Abbreviation         string `json:"abbreviation,omitempty"`
	LevelOfQualification string `json:"levelOfQualification,omitempty"`
	OrganizationID       string `json:"organizationId,omitempty"`
}

// Result is an OEAPI result. In OEAPI a result hangs off an association;
// here it is published as its own resource carrying the person and the
// course or program it is for.
type Result struct {
	ResultID   string  `json:"resultId"`
	PersonID   string  `json:"personId"`
	CourseID   string  `json:"courseId,omitempty"`
	ProgramID  string  `json:"programId,omitempty"`
	OfferingID string  `json:"offeringId,omitempty"`
	State      string  `json:"state,omitempty"`
	Score      string  `json:"score,omitempty"`
	ECTS       float64 `json:"ects,omitempty"`
	ResultDate string  `json:"resultDate,omitempty"`
}

// FromPerson maps a canonical person to the OEAPI shape.
func FromPerson(p domain.Person) Person {
	var mail string
	if len(p.Emails) > 0 {
		mail = p.Emails[0].Value
	}
	display := p.GivenName
	if p.FamilyName != "" {
		if display != "" {
			display += " "
		}
		display += p.FamilyName
	}
	return Person{
		PersonID: p.SourcedID, GivenName: p.GivenName, Surname: p.FamilyName,
		DisplayName: display, Mail: mail,
	}
}

// The relationship mappers below copy reference fields (ParentID, OrgID,
// CourseID, AcademicSessionID, PersonID, OfferingID) verbatim. OEAPI
// resources are addressed by sourcedId, so callers must first rewrite those
// fields to the referenced records' sourcedIds (readmodel.Client.PublishRefs).

// FromOrganization maps a canonical organization.
func FromOrganization(o *domain.Organization) Organization {
	return Organization{
		OrganizationID: o.SourcedID, Name: o.Name, Type: o.Type, ParentOrganization: o.ParentID,
	}
}

// FromAcademicSession maps a canonical academic session.
func FromAcademicSession(a *domain.AcademicSession) AcademicSession {
	return AcademicSession{
		AcademicSessionID: a.SourcedID, Name: a.Title, Type: a.Type,
		StartDate: a.StartDate, EndDate: a.EndDate,
	}
}

// FromCourse maps a canonical course.
func FromCourse(c *domain.Course) Course {
	return Course{CourseID: c.SourcedID, Name: c.Title, Abbreviation: c.Code, OrganizationID: c.OrgID}
}

// FromOffering maps a canonical offering.
func FromOffering(o *domain.Offering) Offering {
	return Offering{
		OfferingID: o.SourcedID, Name: o.Title, CourseID: o.CourseID, AcademicSessionID: o.AcademicSessionID,
	}
}

// FromProgram maps a canonical program.
func FromProgram(p *domain.Program) Program {
	return Program{
		ProgramID: p.SourcedID, Name: p.Title, Abbreviation: p.Code,
		LevelOfQualification: p.Level, OrganizationID: p.OrgID,
	}
}

// FromResult maps a canonical result.
func FromResult(r *domain.Result) Result {
	return Result{
		ResultID: r.SourcedID, PersonID: r.PersonID, CourseID: r.CourseID, ProgramID: r.ProgramID,
		OfferingID: r.OfferingID, State: string(r.Status), Score: r.Grade, ECTS: r.Credits, ResultDate: r.ResultDate,
	}
}

// Bundle is a set of converted records in OEAPI shape, one array per
// resource collection, keyed like the provider's paths.
type Bundle struct {
	Persons          []Person          `json:"persons"`
	Organizations    []Organization    `json:"organizations"`
	AcademicSessions []AcademicSession `json:"academicSessions"`
	Courses          []Course          `json:"courses"`
	Programs         []Program         `json:"programs"`
	Offerings        []Offering        `json:"offerings"`
	Associations     []Association     `json:"associations"`
	Results          []Result          `json:"results"`
}

// FromBundle maps a converted domain.Bundle, whose references are already
// sourcedIds, to OEAPI shape.
func FromBundle(b *domain.Bundle) Bundle {
	out := Bundle{
		Persons:          make([]Person, 0, len(b.Persons)),
		Organizations:    mapAll(b.Organizations, FromOrganization),
		AcademicSessions: mapAll(b.AcademicSessions, FromAcademicSession),
		Courses:          mapAll(b.Courses, FromCourse),
		Programs:         mapAll(b.Programs, FromProgram),
		Offerings:        mapAll(b.Offerings, FromOffering),
		Associations:     mapAll(b.Enrollments, FromAssociation),
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

// FromAssociation maps a canonical enrollment to an OEAPI association.
func FromAssociation(e *domain.Enrollment) Association {
	return Association{
		AssociationID: e.SourcedID, PersonID: e.PersonID, OfferingID: e.OfferingID,
		Role: string(e.Role), State: string(e.Status),
	}
}
