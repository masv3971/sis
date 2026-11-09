package domain

import (
	"math"
	"slices"
)

// validateIDs rejects an empty entry in a list of references.
func validateIDs(field string, ids []string) error {
	if slices.Contains(ids, "") {
		return &ValidationError{Field: field, Msg: "must not hold an empty id"}
	}
	return nil
}

// Organization is an institution or an organizational unit within one.
type Organization struct {
	Base
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"` // e.g. institution, school, department
	ParentID string `json:"parentId,omitempty"`
}

// EntityType returns the stable type tag.
func (o *Organization) EntityType() string { return "organization" }

// Validate checks required invariants.
func (o *Organization) Validate() error {
	if err := o.validateStatus(); err != nil {
		return err
	}
	if o.Name == "" {
		return Required("name")
	}
	return nil
}

// AcademicSession is a term, semester, or school year.
type AcademicSession struct {
	Base
	Title     string `json:"title"`
	Type      string `json:"type,omitempty"` // e.g. term, semester, schoolYear
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

// EntityType returns the stable type tag.
func (a *AcademicSession) EntityType() string { return "academicSession" }

// Validate checks required invariants.
func (a *AcademicSession) Validate() error {
	if err := a.validateStatus(); err != nil {
		return err
	}
	if a.Title == "" {
		return Required("title")
	}
	return nil
}

// Course is a unit of study offered by an organization.
type Course struct {
	Base
	Code  string `json:"code,omitempty"`
	Title string `json:"title"`
	OrgID string `json:"orgId,omitempty"`
	// ProgramIDs are the programs the course is part of.
	ProgramIDs []string `json:"programIds,omitempty"`
}

// EntityType returns the stable type tag.
func (c *Course) EntityType() string { return "course" }

// Validate checks required invariants.
func (c *Course) Validate() error {
	if err := c.validateStatus(); err != nil {
		return err
	}
	if c.Title == "" {
		return Required("title")
	}
	return validateIDs("programIds", c.ProgramIDs)
}

// Offering is a course scheduled in an academic session (a class).
type Offering struct {
	Base
	Title             string `json:"title,omitempty"`
	CourseID          string `json:"courseId"`
	AcademicSessionID string `json:"academicSessionId,omitempty"`
	OrgID             string `json:"orgId,omitempty"`
	// CollectionOfferingIDs are the collection offerings (programs delivered
	// in a session) the offering is part of.
	CollectionOfferingIDs []string `json:"collectionOfferingIds,omitempty"`
}

// EntityType returns the stable type tag.
func (o *Offering) EntityType() string { return "offering" }

// Validate checks required invariants.
func (o *Offering) Validate() error {
	if err := o.validateStatus(); err != nil {
		return err
	}
	if o.CourseID == "" {
		return Required("courseId")
	}
	return validateIDs("collectionOfferingIds", o.CollectionOfferingIDs)
}

// Enrollment associates a person with an offering, in a role.
type Enrollment struct {
	Base
	PersonID   string `json:"personId"`
	OfferingID string `json:"offeringId"`
	Role       Role   `json:"role,omitempty"` // e.g. student, faculty
}

// EntityType returns the stable type tag.
func (e *Enrollment) EntityType() string { return "enrollment" }

// Validate checks required invariants.
func (e *Enrollment) Validate() error {
	if err := e.validateStatus(); err != nil {
		return err
	}
	if e.PersonID == "" {
		return Required("personId")
	}
	if e.OfferingID == "" {
		return Required("offeringId")
	}
	return nil
}

// Program is a structured collection of courses leading to a qualification
// (degree, diploma, certificate). OEAPI calls it a programme; ELM a
// Qualification.
type Program struct {
	Base
	Code  string `json:"code,omitempty"`
	Title string `json:"title"`
	// Level is the qualification level, e.g. an EQF level ("6") or a local
	// level code.
	Level string `json:"level,omitempty"`
	OrgID string `json:"orgId,omitempty"`
}

// EntityType returns the stable type tag.
func (p *Program) EntityType() string { return "program" }

// Validate checks required invariants.
func (p *Program) Validate() error {
	if err := p.validateStatus(); err != nil {
		return err
	}
	if p.Title == "" {
		return Required("title")
	}
	return nil
}

// Result is a person's outcome for a course or a program: the grade (or
// score) and the credits awarded.
type Result struct {
	Base
	PersonID string `json:"personId"`
	// Exactly one of CourseID and ProgramID is set: what the result is for.
	CourseID  string `json:"courseId,omitempty"`
	ProgramID string `json:"programId,omitempty"`
	// OfferingID optionally names the offering (class) the result was
	// obtained in.
	OfferingID string `json:"offeringId,omitempty"`
	// Grade is the grade as awarded, e.g. "A", "VG", "Pass".
	Grade string `json:"grade,omitempty"`
	// Credits awarded, in ECTS credits. Credits in other systems (e.g.
	// local points) are not stored here.
	Credits float64 `json:"credits,omitempty"`
	// ResultDate is the ISO 8601 date the result was awarded.
	ResultDate string `json:"resultDate,omitempty"`
}

// Entitlement is a right a person holds, e.g. access to further studies or
// to practise a profession (ELM LearningEntitlement). Not published through
// OEAPI or Edu-API, which have no such resource.
type Entitlement struct {
	Base
	PersonID string `json:"personId"`
	Title    string `json:"title"`
	OrgID    string `json:"orgId,omitempty"`
	// ResultID optionally names the result that confers the entitlement.
	ResultID string `json:"resultId,omitempty"`
	// IssuedDate and ExpiryDate are ISO 8601 dates.
	IssuedDate string `json:"issuedDate,omitempty"`
	ExpiryDate string `json:"expiryDate,omitempty"`
}

// EntityType returns the stable type tag.
func (e *Entitlement) EntityType() string { return "entitlement" }

// Validate checks required invariants.
func (e *Entitlement) Validate() error {
	if err := e.validateStatus(); err != nil {
		return err
	}
	if e.PersonID == "" {
		return Required("personId")
	}
	if e.Title == "" {
		return Required("title")
	}
	return nil
}

// EntityType returns the stable type tag.
func (r *Result) EntityType() string { return "result" }

// Affiliation relates a person to an organization in a role, for a period
// (Edu-API affiliation).
type Affiliation struct {
	Base
	PersonID string `json:"personId"`
	OrgID    string `json:"orgId"`
	Role     Role   `json:"role,omitempty"`
	// StartDate and EndDate are ISO 8601 dates or date-times.
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

// EntityType returns the stable type tag.
func (a *Affiliation) EntityType() string { return "affiliation" }

// Validate checks required invariants.
func (a *Affiliation) Validate() error {
	if err := a.validateStatus(); err != nil {
		return err
	}
	if a.PersonID == "" {
		return Required("personId")
	}
	if a.OrgID == "" {
		return Required("orgId")
	}
	return nil
}

// CollectionOffering is a program delivered in an academic session.
type CollectionOffering struct {
	Base
	Title             string `json:"title,omitempty"`
	ProgramID         string `json:"programId"`
	AcademicSessionID string `json:"academicSessionId,omitempty"`
	OrgID             string `json:"orgId,omitempty"`
}

// EntityType returns the stable type tag.
func (c *CollectionOffering) EntityType() string { return "collectionOffering" }

// Validate checks required invariants.
func (c *CollectionOffering) Validate() error {
	if err := c.validateStatus(); err != nil {
		return err
	}
	if c.ProgramID == "" {
		return Required("programId")
	}
	return nil
}

// ComponentTemplate is a part of a course, such as a lecture or a laboratory,
// as the catalog describes it.
type ComponentTemplate struct {
	Base
	Code  string `json:"code,omitempty"`
	Title string `json:"title"`
	// Type is the kind of component, e.g. lecture, laboratory, seminar.
	Type  string `json:"type,omitempty"`
	OrgID string `json:"orgId,omitempty"`
	// CourseIDs are the courses the component belongs to.
	CourseIDs []string `json:"courseIds,omitempty"`
}

// EntityType returns the stable type tag.
func (c *ComponentTemplate) EntityType() string { return "componentTemplate" }

// Validate checks required invariants.
func (c *ComponentTemplate) Validate() error {
	if err := c.validateStatus(); err != nil {
		return err
	}
	if c.Title == "" {
		return Required("title")
	}
	return validateIDs("courseIds", c.CourseIDs)
}

// ComponentOffering is a component delivered in an academic session, e.g.
// the lecture of one course offering.
type ComponentOffering struct {
	Base
	Title             string `json:"title,omitempty"`
	ComponentID       string `json:"componentId"`
	AcademicSessionID string `json:"academicSessionId,omitempty"`
	OrgID             string `json:"orgId,omitempty"`
	// CourseOfferingIDs are the course offerings the component is delivered
	// with.
	CourseOfferingIDs []string `json:"courseOfferingIds,omitempty"`
}

// EntityType returns the stable type tag.
func (c *ComponentOffering) EntityType() string { return "componentOffering" }

// Validate checks required invariants.
func (c *ComponentOffering) Validate() error {
	if err := c.validateStatus(); err != nil {
		return err
	}
	if c.ComponentID == "" {
		return Required("componentId")
	}
	return validateIDs("courseOfferingIds", c.CourseOfferingIDs)
}

// Validate checks required invariants.
func (r *Result) Validate() error {
	if err := r.validateStatus(); err != nil {
		return err
	}
	if r.PersonID == "" {
		return Required("personId")
	}
	if (r.CourseID == "") == (r.ProgramID == "") {
		return &ValidationError{Field: "courseId", Msg: "exactly one of courseId and programId is required"}
	}
	if math.IsNaN(r.Credits) || math.IsInf(r.Credits, 0) {
		return &ValidationError{Field: "credits", Msg: "must be a finite number"}
	}
	if r.Credits < 0 {
		return &ValidationError{Field: "credits", Msg: "must not be negative"}
	}
	return nil
}
