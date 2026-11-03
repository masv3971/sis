package eduapi

import (
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromPerson(t *testing.T) {
	person := FromPerson(domain.Person{
		SourcedID: "stu", Status: domain.StatusActive, GivenName: "Ada", FamilyName: "Lovelace",
		Roles:       []domain.Role{domain.RoleStudent},
		Identifiers: []domain.Identifier{{Type: "nin", Value: "1"}},
		Emails:      []domain.Contact{{Value: "a@x"}, {Value: "b@x"}},
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600)),
	})
	assert.Equal(t, "stu", person.SourcedID)
	assert.Equal(t, "a@x", person.Email)
	assert.Equal(t, "2026-01-02T02:04:05Z", person.DateLastModified)
	if assert.Len(t, person.Roles, 1) {
		assert.Equal(t, string(domain.RoleStudent), person.Roles[0].Role)
	}
	if assert.Len(t, person.UserIDs, 1) {
		assert.Equal(t, "1", person.UserIDs[0].Identifier)
	}
	empty := FromPerson(domain.Person{})
	assert.Empty(t, empty.DateLastModified, "zero person")
	assert.NotNil(t, empty.Roles, "zero person roles must be non-nil")
}

// TestOptionalRefsAreOmitted checks an empty reference becomes no GUIDRef at
// all rather than one with an empty sourcedId.
func TestOptionalRefsAreOmitted(t *testing.T) {
	class := FromClass(&domain.Offering{SourcedID: "o", CourseID: "c"})
	require.NotNil(t, class.Course)
	assert.Equal(t, "c", class.Course.SourcedID)
	assert.Equal(t, "course", class.Course.Type)
	assert.Nil(t, class.School)
	assert.Nil(t, class.Terms)

	class = FromClass(&domain.Offering{SourcedID: "o", CourseID: "c", AcademicSessionID: "ht26", OrgID: "uni"})
	if assert.Len(t, class.Terms, 1, "class with terms") {
		assert.Equal(t, "ht26", class.Terms[0].SourcedID)
	}
	if assert.NotNil(t, class.School, "class with school") {
		assert.Equal(t, "uni", class.School.SourcedID)
	}

	assert.Nil(t, FromOrg(&domain.Organization{SourcedID: "uni"}).Parent, "org parent")

	result := FromResult(&domain.Result{SourcedID: "r", PersonID: "stu", ProgramID: "BSc"})
	assert.Equal(t, "stu", result.Student.SourcedID)
	assert.Nil(t, result.Course)
	assert.Nil(t, result.Class)
	if assert.NotNil(t, result.Program) {
		assert.Equal(t, "BSc", result.Program.SourcedID)
	}
}

func TestFromBundle(t *testing.T) {
	out := FromBundle(&domain.Bundle{
		Persons:     []domain.Person{{SourcedID: "stu"}},
		Enrollments: []*domain.Enrollment{{SourcedID: "e", PersonID: "stu", OfferingID: "o"}},
	})
	assert.Len(t, out.Persons, 1)
	if assert.Len(t, out.Enrollments, 1) {
		assert.Equal(t, "stu", out.Enrollments[0].User.SourcedID)
	}
	assert.NotNil(t, out.Orgs, "empty lists must be non-nil")
}
