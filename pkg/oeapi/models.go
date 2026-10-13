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

// FromAssociation maps a canonical enrollment to an OEAPI association.
func FromAssociation(e *domain.Enrollment) Association {
	return Association{
		AssociationID: e.SourcedID, PersonID: e.PersonID, OfferingID: e.OfferingID,
		Role: string(e.Role), State: string(e.Status),
	}
}
