package oeapi

import (
	"testing"

	"github.com/SUNET/sis/internal/core/domain"
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPage([]int{}, c.number, c.size, c.total)
			if p.TotalPages != c.wantPages || p.HasPreviousPage != c.wantPrevious || p.HasNextPage != c.wantNext {
				t.Fatalf("got pages=%d prev=%v next=%v", p.TotalPages, p.HasPreviousPage, p.HasNextPage)
			}
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.SourcedID = "stu"
			got := FromPerson(c.in)
			if got.PersonID != "stu" || got.DisplayName != c.display || got.Mail != c.mail {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// TestMappersUseSourcedIDs checks every resource is identified by its
// sourcedId, never its canonical id.
func TestMappersUseSourcedIDs(t *testing.T) {
	b := func(sid string) domain.Base {
		return domain.Base{ID: "id-" + sid, SourcedID: sid, Status: domain.StatusActive}
	}
	r := FromResult(&domain.Result{Base: b("r"), PersonID: "stu", CourseID: "c", Grade: "A", Credits: 7.5})
	if r.ResultID != "r" || r.State != "active" || r.Score != "A" || r.ECTS != 7.5 {
		t.Fatalf("result = %+v", r)
	}
	a := FromAssociation(&domain.Enrollment{Base: b("e"), PersonID: "stu", OfferingID: "o", Role: domain.RoleStudent})
	if a.AssociationID != "e" || a.Role != string(domain.RoleStudent) || a.State != "active" {
		t.Fatalf("association = %+v", a)
	}
	out := FromBundle(&domain.Bundle{
		Persons:       []domain.Person{{SourcedID: "stu"}},
		Organizations: []*domain.Organization{{Base: b("uni"), Name: "U"}},
	})
	if len(out.Persons) != 1 || out.Organizations[0].OrganizationID != "uni" || out.Courses == nil {
		t.Fatalf("bundle = %+v", out)
	}
}
