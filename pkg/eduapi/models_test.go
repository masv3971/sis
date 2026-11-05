package eduapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
)

var mapper = NewMapper("en")

func toMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestPerson(t *testing.T) {
	person := mapper.Person(domain.Person{
		SourcedID: "stu", Status: domain.StatusActive, GivenName: "Ada", MiddleName: "King", FamilyName: "Lovelace",
		Roles:       []domain.Role{domain.RoleStudent},
		Identifiers: []domain.Identifier{{Type: "nin", Value: "1"}, {Type: "systemId", Value: "2"}},
		Emails:      []domain.Contact{{Type: "work", Value: "a@x"}, {Value: "b@x"}},
		Phones:      []domain.Contact{{Type: "phone", Value: "123"}},
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600)),
	})
	assert.Equal(t, "stu", person.SourcedID)
	assert.Equal(t, "en", person.RecordLanguage)
	assert.Equal(t, "active", person.RecordStatus)
	assert.Equal(t, "Ada King Lovelace", person.FormattedName)
	assert.Equal(t, "2026-01-02T02:04:05Z", person.DateLastModified)
	require.NotNil(t, person.LegalName)
	assert.Equal(t, "King", person.LegalName.AdditionalName)
	require.NotNil(t, person.PrimaryEmail)
	assert.Equal(t, "a@x", person.PrimaryEmail.Email)
	assert.Equal(t, "workEmail", person.PrimaryEmail.EmailType)
	require.Len(t, person.OtherEmails, 1)
	assert.Equal(t, "ext:other", person.OtherEmails[0].EmailType, "typed list entries need a type")
	require.NotNil(t, person.PrimaryPhone)
	assert.Empty(t, person.PrimaryPhone.PhoneType)
	assert.Equal(t, []IdentifierEntry{
		{Identifier: "1", IdentifierType: "ext:nin"},
		{Identifier: "2", IdentifierType: "systemId"},
	}, person.OtherIdentifiers)
	assert.NotContains(t, toMap(t, person), "roles", "role belongs to enrollments")
}

func TestEmptyValuesAreOmitted(t *testing.T) {
	got := toMap(t, mapper.Person(domain.Person{SourcedID: "x"}))
	for _, key := range []string{"otherIdentifiers", "primaryEmail", "otherEmails", "primaryPhone", "dateOfBirth", "dateLastModified"} {
		assert.NotContains(t, got, key)
	}
	assert.Equal(t, "active", got["recordStatus"], "an unset status is an active record")
}

func TestRecordStatus(t *testing.T) {
	for in, want := range map[domain.Status]string{
		"": "active", domain.StatusActive: "active", domain.StatusInactive: "inactive", domain.StatusToBeDeleted: "deleted",
	} {
		assert.Equal(t, want, recordStatus(in), "status %q", in)
	}
}

func TestOrganization(t *testing.T) {
	org := mapper.Organization(&domain.Organization{SourcedID: "uni", Name: "University", ParentID: "root"})
	assert.Equal(t, []LanguageTypedString{{RecordLanguage: "en", Value: "University"}}, org.Name)
	assert.Equal(t, "organization", org.OrganizationType, "organizationType is required")
	assert.Equal(t, "root", org.Parent)
	assert.NotContains(t, toMap(t, mapper.Organization(&domain.Organization{SourcedID: "u", Name: "U"})), "parent")
}

func TestAcademicSessionDates(t *testing.T) {
	session := mapper.AcademicSession(&domain.AcademicSession{
		SourcedID: "ht26", Title: "Autumn 2026", Type: "term", StartDate: "2026-08-31", EndDate: "2027-01-15T10:00:00Z",
	})
	assert.Equal(t, "term", session.SessionType)
	assert.Equal(t, "2026-08-31T00:00:00Z", session.StartDate)
	assert.Equal(t, "2027-01-15T10:00:00Z", session.EndDate)
	assert.Empty(t, mapper.AcademicSession(&domain.AcademicSession{StartDate: "soon"}).StartDate, "unreadable dates are left out")
	assert.Equal(t, "ext:Trimester", mapper.AcademicSession(&domain.AcademicSession{Type: "Trimester"}).SessionType)
}

func TestCourseTemplate(t *testing.T) {
	course := mapper.CourseTemplate(&domain.Course{Base: domain.Base{SourcedID: "c"}, Code: "BI101", Title: "Biology", OrgID: "uni"})
	assert.Equal(t, "standard", course.CourseType)
	assert.Equal(t, IdentifierEntry{Identifier: "BI101", IdentifierType: "identifier"}, course.PrimaryCode)
	assert.Equal(t, "uni", course.Organization)
	assert.Equal(t, "ext:unspecified", course.Level)
	assert.Equal(t, "ext:unspecified", course.CreditType)
	assert.NotEmpty(t, course.GradingScheme, "gradingScheme is required")

	noCode := mapper.CourseTemplate(&domain.Course{Base: domain.Base{SourcedID: "c"}, Title: "Biology"})
	assert.Equal(t, IdentifierEntry{Identifier: "c", IdentifierType: "systemId"}, noCode.PrimaryCode, "primaryCode is required")
	assert.NotContains(t, toMap(t, noCode), "organization")
}

func TestCollectionTemplate(t *testing.T) {
	for level, want := range map[string]string{"": "ext:unspecified", "6": "undergraduate", "7": "graduate", "8": "doctoral", "9": "ext:9", "graduate": "graduate"} {
		program := mapper.CollectionTemplate(&domain.Program{Base: domain.Base{SourcedID: "p"}, Title: "BSc", Level: level})
		assert.Equal(t, "program", program.CollectionType)
		assert.Equal(t, want, program.Level, "level %q", level)
	}
}

func TestCourseOffering(t *testing.T) {
	offering := &domain.Offering{Base: domain.Base{SourcedID: "o"}, CourseID: "c", AcademicSessionID: "ht26", Title: "Biology HT26"}
	session := &domain.AcademicSession{StartDate: "2026-08-31", EndDate: "2027-01-15"}

	out := mapper.CourseOffering(offering, session, "Biology")
	assert.Equal(t, "c", out.Course)
	assert.Equal(t, "ht26", out.AcademicSession)
	assert.Equal(t, "2026-08-31T00:00:00Z", out.StartDate)
	assert.Equal(t, "2027-01-15T00:00:00Z", out.EndDate)
	assert.Equal(t, "closed", out.RegistrationStatus)
	assert.Equal(t, "o", out.PrimaryCode.Identifier)

	bare := toMap(t, mapper.CourseOffering(offering, nil, ""))
	assert.NotContains(t, bare, "startDate")
	assert.NotContains(t, bare, "endDate")
}

// TestUntitledCourseOffering checks an offering without a title still gets
// the title and description the spec requires: its course's title, else its
// sourcedId.
func TestUntitledCourseOffering(t *testing.T) {
	offering := &domain.Offering{Base: domain.Base{SourcedID: "o"}, CourseID: "c"}

	withCourse := mapper.CourseOffering(offering, nil, "Biology")
	assert.Equal(t, []LanguageTypedString{{RecordLanguage: "en", Value: "Biology"}}, withCourse.Title)
	assert.Equal(t, withCourse.Title, withCourse.Description)

	bare := toMap(t, mapper.CourseOffering(offering, nil, ""))
	assert.Contains(t, bare, "title", "title is required")
	assert.Contains(t, bare, "description", "description is required")
	assert.Equal(t, []LanguageTypedString{{RecordLanguage: "en", Value: "o"}}, mapper.CourseOffering(offering, nil, "").Title)

	titled := &domain.Offering{Base: domain.Base{SourcedID: "o"}, CourseID: "c", Title: "Own title"}
	assert.Equal(t, "Own title", mapper.CourseOffering(titled, nil, "Biology").Title[0].Value, "the offering's own title wins")
}

func TestEnrollment(t *testing.T) {
	active := mapper.Enrollment(&domain.Enrollment{Base: domain.Base{SourcedID: "e"}, PersonID: "stu", OfferingID: "o", Role: domain.RoleFaculty})
	assert.True(t, active.IsActive)
	assert.Equal(t, "enrolled", active.EnrollmentStatus)
	assert.Equal(t, "teacher", active.Role)
	assert.Equal(t, "stu", active.Person)
	assert.Equal(t, "o", active.EducationOffering)
	assert.Equal(t, "course", active.OfferingType)

	done := mapper.Enrollment(&domain.Enrollment{Base: domain.Base{Status: domain.StatusInactive}, Role: domain.RoleStudent})
	assert.False(t, done.IsActive)
	assert.Equal(t, "finished", done.EnrollmentStatus)
	assert.Equal(t, "member", mapper.Enrollment(&domain.Enrollment{}).Role, "role is required")
}

func TestBundle(t *testing.T) {
	out := mapper.Bundle(&domain.Bundle{
		Persons:          []domain.Person{{SourcedID: "stu"}},
		AcademicSessions: []*domain.AcademicSession{{Base: domain.Base{SourcedID: "ht26"}, StartDate: "2026-08-31"}},
		Courses:          []*domain.Course{{Base: domain.Base{SourcedID: "c"}, Title: "Biology"}},
		Offerings:        []*domain.Offering{{Base: domain.Base{SourcedID: "o"}, CourseID: "c", AcademicSessionID: "ht26"}},
		Enrollments:      []*domain.Enrollment{{Base: domain.Base{SourcedID: "e"}, PersonID: "stu", OfferingID: "o"}},
		Results:          []*domain.Result{{Base: domain.Base{SourcedID: "r"}}},
	})
	assert.Len(t, out.Persons, 1)
	require.Len(t, out.CourseOfferings, 1)
	assert.Equal(t, "2026-08-31T00:00:00Z", out.CourseOfferings[0].StartDate, "dates come from the offering's session")
	assert.Equal(t, "Biology", out.CourseOfferings[0].Title[0].Value, "an untitled offering takes its course's title")
	require.Len(t, out.Enrollments, 1)
	assert.Equal(t, "stu", out.Enrollments[0].Person)
	assert.NotNil(t, out.Organizations, "empty lists must be non-nil")
	assert.NotContains(t, toMap(t, out), "results", "Edu-API has no result resource")
}

func TestNewStatusInfo(t *testing.T) {
	info := NewStatusInfo(404, "no such person")
	assert.Equal(t, "failure", info.CodeMajor)
	assert.Equal(t, "status", info.Severity)
	require.NotNil(t, info.CodeMinor)
	assert.Equal(t, "not_found", info.CodeMinor.Fields[0].Value)
	assert.Equal(t, "error", NewStatusInfo(500, "").Severity)
	assert.Equal(t, "server_busy", NewStatusInfo(503, "").CodeMinor.Fields[0].Value, "503 must not be unknown")
}

// TestRequiredFields pins the fields the spec's JSON schemas
// (eduapi_v1p0_openapi3.json) mark required, for a record with everything SIS
// can have filled in. A record missing one is not valid Edu-API.
func TestRequiredFields(t *testing.T) {
	session := &domain.AcademicSession{Base: domain.Base{SourcedID: "ht26"}, Title: "HT26", StartDate: "2026-08-31", EndDate: "2027-01-15"}
	template := []string{
		"sourcedId", "recordLanguage", "title", "description", "primaryCode", "organization",
		"level", "creditType", "gradingScheme", "recordStatus",
	}
	offering := []string{
		"offeringType", "sourcedId", "recordLanguage", "title", "description", "primaryCode", "organization",
		"academicSession", "offeringFormat", "registrationStatus", "startDate", "endDate", "recordStatus",
	}
	cases := []struct {
		name     string
		record   any
		required []string
	}{
		{"person", mapper.Person(domain.Person{SourcedID: "p", GivenName: "A", FamilyName: "B"}), []string{"sourcedId", "recordLanguage", "recordStatus"}},
		{"organization", mapper.Organization(&domain.Organization{Base: domain.Base{SourcedID: "o"}, Name: "O"}),
			[]string{"sourcedId", "recordLanguage", "name", "organizationType", "recordStatus"}},
		{"academicSession", mapper.AcademicSession(session), []string{"sourcedId", "recordLanguage", "recordStatus"}},
		{"courseTemplate", mapper.CourseTemplate(&domain.Course{Base: domain.Base{SourcedID: "c"}, Title: "C", OrgID: "o"}), append([]string{"courseType"}, template...)},
		{"collectionTemplate", mapper.CollectionTemplate(&domain.Program{Base: domain.Base{SourcedID: "p"}, Title: "P", OrgID: "o"}), append([]string{"collectionType"}, template...)},
		{"courseOffering", mapper.CourseOffering(&domain.Offering{
			Base: domain.Base{SourcedID: "f"}, Title: "F", CourseID: "c", AcademicSessionID: "ht26", OrgID: "o",
		}, session, ""), append([]string{"course"}, offering...)},
		{"untitled courseOffering", mapper.CourseOffering(&domain.Offering{
			Base: domain.Base{SourcedID: "g"}, CourseID: "c", AcademicSessionID: "ht26", OrgID: "o",
		}, session, ""), append([]string{"course"}, offering...)},
		{"enrollment", mapper.Enrollment(&domain.Enrollment{Base: domain.Base{SourcedID: "e"}, PersonID: "p", OfferingID: "f"}),
			[]string{"sourcedId", "recordLanguage", "isActive", "enrollmentStatus", "person", "role", "educationOffering", "offeringType", "recordStatus"}},
	}
	for _, tc := range cases {
		got := toMap(t, tc.record)
		for _, field := range tc.required {
			assert.Contains(t, got, field, "%s: required field %s missing", tc.name, field)
		}
	}
}
