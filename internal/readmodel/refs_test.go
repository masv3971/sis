package readmodel

import (
	"context"
	"io"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/logger"
)

// TestPublishRefsUsesSourcedIDs seeds records whose canonical id differs from
// their sourcedId and checks every reference is published as the sourcedId.
func TestPublishRefsUsesSourcedIDs(t *testing.T) {
	ctx := context.Background()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	c := &Client{
		Persons:          service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p-id", SourcedID: "student-1", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	must(err)
	_, err = c.Organizations.Create(ctx, &domain.Organization{Base: domain.Base{ID: "o-id", SourcedID: "uni"}, Name: "U"})
	must(err)
	_, err = c.AcademicSessions.Create(ctx, &domain.AcademicSession{Base: domain.Base{ID: "s-id", SourcedID: "ht26"}, Title: "HT26"})
	must(err)
	_, err = c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c-id", SourcedID: "DA101"}, Title: "C", OrgID: "o-id"})
	must(err)
	_, err = c.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: "f-id", SourcedID: "DA101-ht26"}, CourseID: "c-id", AcademicSessionID: "s-id", OrgID: "o-id"})
	must(err)

	child := &domain.Organization{ParentID: "o-id"}
	course := &domain.Course{OrgID: "o-id"}
	offering := &domain.Offering{CourseID: "c-id", AcademicSessionID: "s-id", OrgID: "uni"} // already a sourcedId
	enrollment := &domain.Enrollment{PersonID: "p-id", OfferingID: "f-id"}
	dangling := &domain.Enrollment{PersonID: "gone", OfferingID: "f-id"}
	must(c.PublishRefs(ctx, child, course, offering, enrollment, dangling))

	checks := []struct{ name, got, want string }{
		{"organization.parent", child.ParentID, "uni"},
		{"course.org", course.OrgID, "uni"},
		{"offering.course", offering.CourseID, "DA101"},
		{"offering.session", offering.AcademicSessionID, "ht26"},
		{"offering.org (already sourcedId)", offering.OrgID, "uni"},
		{"enrollment.person", enrollment.PersonID, "student-1"},
		{"enrollment.offering", enrollment.OfferingID, "DA101-ht26"},
		{"dangling reference cleared", dangling.PersonID, ""},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %q, want %q", ch.name, ch.got, ch.want)
		}
	}
}
