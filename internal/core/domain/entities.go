package domain

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
		return &ValidationError{Field: "name", Msg: "required"}
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
		return &ValidationError{Field: "title", Msg: "required"}
	}
	return nil
}

// Course is a unit of study offered by an organization.
type Course struct {
	Base
	Code  string `json:"code,omitempty"`
	Title string `json:"title"`
	OrgID string `json:"orgId,omitempty"`
}

// EntityType returns the stable type tag.
func (c *Course) EntityType() string { return "course" }

// Validate checks required invariants.
func (c *Course) Validate() error {
	if err := c.validateStatus(); err != nil {
		return err
	}
	if c.Title == "" {
		return &ValidationError{Field: "title", Msg: "required"}
	}
	return nil
}

// Offering is a course scheduled in an academic session (a class).
type Offering struct {
	Base
	Title             string `json:"title,omitempty"`
	CourseID          string `json:"courseId"`
	AcademicSessionID string `json:"academicSessionId,omitempty"`
	OrgID             string `json:"orgId,omitempty"`
}

// EntityType returns the stable type tag.
func (o *Offering) EntityType() string { return "offering" }

// Validate checks required invariants.
func (o *Offering) Validate() error {
	if err := o.validateStatus(); err != nil {
		return err
	}
	if o.CourseID == "" {
		return &ValidationError{Field: "courseId", Msg: "required"}
	}
	return nil
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
		return &ValidationError{Field: "personId", Msg: "required"}
	}
	if e.OfferingID == "" {
		return &ValidationError{Field: "offeringId", Msg: "required"}
	}
	return nil
}
