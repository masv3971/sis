package eduapi

import (
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
)

func TestFromPerson(t *testing.T) {
	p := FromPerson(domain.Person{
		SourcedID: "stu", Status: domain.StatusActive, GivenName: "Ada", FamilyName: "Lovelace",
		Roles:       []domain.Role{domain.RoleStudent},
		Identifiers: []domain.Identifier{{Type: "nin", Value: "1"}},
		Emails:      []domain.Contact{{Value: "a@x"}, {Value: "b@x"}},
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600)),
	})
	if p.SourcedID != "stu" || p.Email != "a@x" || p.DateLastModified != "2026-01-02T02:04:05Z" {
		t.Fatalf("person = %+v", p)
	}
	if len(p.Roles) != 1 || p.Roles[0].Role != string(domain.RoleStudent) || len(p.UserIDs) != 1 || p.UserIDs[0].Identifier != "1" {
		t.Fatalf("roles/ids = %+v %+v", p.Roles, p.UserIDs)
	}
	if empty := FromPerson(domain.Person{}); empty.DateLastModified != "" || empty.Roles == nil {
		t.Fatalf("zero person = %+v", empty)
	}
}

// TestOptionalRefsAreOmitted checks an empty reference becomes no GUIDRef at
// all rather than one with an empty sourcedId.
func TestOptionalRefsAreOmitted(t *testing.T) {
	c := FromClass(&domain.Offering{Base: domain.Base{SourcedID: "o"}, CourseID: "c"})
	if c.Course == nil || c.Course.SourcedID != "c" || c.Course.Type != "course" || c.School != nil || c.Terms != nil {
		t.Fatalf("class = %+v", c)
	}
	c = FromClass(&domain.Offering{Base: domain.Base{SourcedID: "o"}, CourseID: "c", AcademicSessionID: "ht26", OrgID: "uni"})
	if len(c.Terms) != 1 || c.Terms[0].SourcedID != "ht26" || c.School == nil || c.School.SourcedID != "uni" {
		t.Fatalf("class with terms = %+v", c)
	}
	if o := FromOrg(&domain.Organization{Base: domain.Base{SourcedID: "uni"}}); o.Parent != nil {
		t.Fatalf("org parent = %+v", o.Parent)
	}
	r := FromResult(&domain.Result{Base: domain.Base{SourcedID: "r"}, PersonID: "stu", ProgramID: "BSc"})
	if r.Student.SourcedID != "stu" || r.Course != nil || r.Class != nil || r.Program == nil || r.Program.SourcedID != "BSc" {
		t.Fatalf("result = %+v", r)
	}
}

func TestFromBundle(t *testing.T) {
	out := FromBundle(&domain.Bundle{
		Persons:     []domain.Person{{SourcedID: "stu"}},
		Enrollments: []*domain.Enrollment{{Base: domain.Base{SourcedID: "e"}, PersonID: "stu", OfferingID: "o"}},
	})
	if len(out.Persons) != 1 || len(out.Enrollments) != 1 || out.Enrollments[0].User.SourcedID != "stu" || out.Orgs == nil {
		t.Fatalf("bundle = %+v", out)
	}
}
