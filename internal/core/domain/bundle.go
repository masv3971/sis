package domain

// Bundle is a self-contained set of canonical records converted from an
// external document (e.g. an ELM credential), not yet stored. Unlike stored
// records, its references (ParentID, OrgID, CourseID, PersonID, ...) hold
// sourcedIds: they name records in the same bundle (or already stored ones)
// by business key, because canonical ids are only assigned on storage.
type Bundle struct {
	Persons          []Person
	Organizations    []*Organization
	AcademicSessions []*AcademicSession
	Courses          []*Course
	Programs         []*Program
	Offerings        []*Offering
	Enrollments      []*Enrollment
	Results          []*Result
}

// Len returns the number of records in the bundle.
func (b *Bundle) Len() int {
	return len(b.Persons) + len(b.Organizations) + len(b.AcademicSessions) + len(b.Courses) +
		len(b.Programs) + len(b.Offerings) + len(b.Enrollments) + len(b.Results)
}
