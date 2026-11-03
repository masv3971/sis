package oeapi

import (
	"testing"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPage(t *testing.T) {
	cases := []struct {
		name                   string
		number, size, total    int
		wantPages              int
		wantPrevious, wantNext bool
	}{
		{"empty", 1, 10, 0, 0, false, false},
		{"one partial page", 1, 10, 3, 1, false, false},
		{"exact multiple", 2, 10, 20, 2, true, false},
		{"middle page", 2, 10, 25, 3, true, true},
		{"past the end", 5, 10, 25, 3, true, false},
		{"zero page size", 1, 0, 25, 0, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := NewPage([]int{}, tc.number, tc.size, tc.total)
			assert.Equal(t, tc.wantPages, page.TotalPages, "totalPages")
			assert.Equal(t, tc.wantPrevious, page.HasPreviousPage, "hasPreviousPage")
			assert.Equal(t, tc.wantNext, page.HasNextPage, "hasNextPage")
		})
	}
}

func TestFromPerson(t *testing.T) {
	cases := []struct {
		name          string
		in            domain.Person
		display, mail string
	}{
		{"full", domain.Person{GivenName: "Ada", FamilyName: "Lovelace", Emails: []domain.Contact{{Value: "a@x"}, {Value: "b@x"}}}, "Ada Lovelace", "a@x"},
		{"family only", domain.Person{FamilyName: "Lovelace"}, "Lovelace", ""},
		{"given only", domain.Person{GivenName: "Ada"}, "Ada", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.SourcedID = "stu"
			got := FromPerson(tc.in)
			assert.Equal(t, "stu", got.PersonID)
			assert.Equal(t, tc.display, got.DisplayName)
			assert.Equal(t, tc.mail, got.Mail)
		})
	}
}

// TestMappersUseSourcedIDs checks every resource is identified by its
// sourcedId, never its canonical id.
func TestMappersUseSourcedIDs(t *testing.T) {
	base := func(sid string) domain.Base {
		return domain.Base{ID: "id-" + sid, SourcedID: sid, Status: domain.StatusActive}
	}
	result := FromResult(&domain.Result{Base: base("r"), PersonID: "stu", CourseID: "c", Grade: "A", Credits: 7.5})
	assert.Equal(t, "r", result.ResultID)
	assert.Equal(t, "active", result.State)
	assert.Equal(t, "A", result.Score)
	assert.Equal(t, 7.5, result.ECTS)
	association := FromAssociation(&domain.Enrollment{Base: base("e"), PersonID: "stu", OfferingID: "o", Role: domain.RoleStudent})
	assert.Equal(t, "e", association.AssociationID)
	assert.Equal(t, string(domain.RoleStudent), association.Role)
	assert.Equal(t, "active", association.State)
	out := FromBundle(&domain.Bundle{
		Persons:       []domain.Person{{SourcedID: "stu"}},
		Organizations: []*domain.Organization{{Base: base("uni"), Name: "U"}},
	})
	assert.Len(t, out.Persons, 1)
	require.Len(t, out.Organizations, 1)
	assert.Equal(t, "uni", out.Organizations[0].OrganizationID)
	assert.NotNil(t, out.Courses, "empty lists must be non-nil")
}
