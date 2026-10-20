package readmodel

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

func sampleBundle() *domain.Bundle {
	b := func(sid string) domain.Base { return domain.Base{SourcedID: sid, Status: domain.StatusActive} }
	return &domain.Bundle{
		Persons: []domain.Person{{SourcedID: "stu", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		// Child listed before its parent: Populate must reorder.
		Organizations: []*domain.Organization{
			{Base: b("dept"), Name: "D", ParentID: "uni"},
			{Base: b("uni"), Name: "U"},
		},
		AcademicSessions: []*domain.AcademicSession{{Base: b("ht25"), Title: "HT25"}},
		Courses:          []*domain.Course{{Base: b("BI101"), Title: "Bio", OrgID: "dept"}},
		Programs:         []*domain.Program{{Base: b("BSc"), Title: "BSc", OrgID: "uni"}},
		Offerings:        []*domain.Offering{{Base: b("BI101-ht25"), CourseID: "BI101", AcademicSessionID: "ht25"}},
		Enrollments:      []*domain.Enrollment{{Base: b("enr"), PersonID: "stu", OfferingID: "BI101-ht25", Role: domain.RoleStudent}},
		Results: []*domain.Result{
			{Base: b("r1"), PersonID: "stu", CourseID: "BI101", OfferingID: "BI101-ht25", Grade: "A"},
			{Base: b("r2"), PersonID: "stu", ProgramID: "BSc"},
		},
	}
}

// TestPopulateStoresWithCanonicalRefs checks every record is stored and its
// references point at canonical ids, so CheckRefs-style invariants hold and
// PublishRefs publishes the original sourcedIds.
func TestPopulateStoresWithCanonicalRefs(t *testing.T) {
	ctx := t.Context()
	c := newFixture(t).c
	exp := time.Now().Add(time.Hour).UTC()
	items, err := c.Populate(ctx, sampleBundle(), &exp)
	must(t, err)
	if len(items) != 10 {
		t.Fatalf("items = %d: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Action != ActionCreated || it.ID == "" {
			t.Fatalf("item %+v", it)
		}
	}
	if items[0].SourcedID != "uni" {
		t.Fatalf("parent must be stored first: %+v", items[:2])
	}

	r, err := c.Results.GetBySourcedID(ctx, "r1")
	must(t, err)
	must(t, c.CheckRefs(ctx, r))
	if r.ExpiresAt == nil || !r.ExpiresAt.Equal(exp) {
		t.Fatalf("expiry not set: %+v", r.ExpiresAt)
	}
	ok, err := c.PublishRefs(ctx, r)
	must(t, err)
	if !ok[0] || r.PersonID != "stu" || r.CourseID != "BI101" || r.OfferingID != "BI101-ht25" {
		t.Fatalf("published refs: %+v", r)
	}
	dept, err := c.Organizations.GetBySourcedID(ctx, "dept")
	must(t, err)
	must(t, c.CheckRefs(ctx, dept))

	// Re-populating converges: same ids, all updated.
	again, err := c.Populate(ctx, sampleBundle(), &exp)
	must(t, err)
	for i, it := range again {
		if it.Action != ActionUpdated || it.ID != items[i].ID {
			t.Fatalf("re-populate item %d = %+v, first %+v", i, it, items[i])
		}
	}
	page, err := c.Results.List(ctx, port.ListParams{})
	must(t, err)
	if page.Total != 2 {
		t.Fatalf("results = %d", page.Total)
	}
}

// TestPopulateResolvesStoredRefs checks a reference may name an already
// stored record by sourcedId.
func TestPopulateResolvesStoredRefs(t *testing.T) {
	ctx := t.Context()
	c := newFixture(t).c
	_, err := c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c-id", SourcedID: "DA101"}, Title: "C"})
	must(t, err)
	b := &domain.Bundle{
		Persons: []domain.Person{{SourcedID: "stu", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Results: []*domain.Result{{Base: domain.Base{SourcedID: "r"}, PersonID: "stu", CourseID: "DA101"}},
	}
	_, err = c.Populate(ctx, b, nil)
	must(t, err)
	r, err := c.Results.GetBySourcedID(ctx, "r")
	must(t, err)
	if r.CourseID != "c-id" || r.ExpiresAt != nil {
		t.Fatalf("result = %+v", r)
	}
}

// TestPopulateBatchesStoredRefs checks references to stored records cost one
// lookup per type, however many records hold them.
func TestPopulateBatchesStoredRefs(t *testing.T) {
	ctx := t.Context()
	f := newFixture(t)
	c := f.c
	_, err := c.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: "f-id", SourcedID: "DA101-ht26"}, CourseID: "c-id"})
	must(t, err)
	b := &domain.Bundle{}
	for i := range 5 {
		sid := "stu" + strconv.Itoa(i)
		_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p" + sid, SourcedID: sid, GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
		must(t, err)
		b.Enrollments = append(b.Enrollments, &domain.Enrollment{
			Base:     domain.Base{SourcedID: "enr" + strconv.Itoa(i), Status: domain.StatusActive},
			PersonID: sid, OfferingID: "DA101-ht26", Role: domain.RoleStudent,
		})
	}
	f.persons.bulk, f.persons.single, f.offering.bulk, f.offering.single = 0, 0, 0, 0

	_, err = c.Populate(ctx, b, nil)
	must(t, err)
	if f.persons.bulk != 1 || f.persons.single != 0 || f.offering.bulk != 1 || f.offering.single != 0 {
		t.Fatalf("lookups: persons bulk=%d single=%d, offerings bulk=%d single=%d",
			f.persons.bulk, f.persons.single, f.offering.bulk, f.offering.single)
	}
	e, err := c.Enrollments.GetBySourcedID(ctx, "enr3")
	must(t, err)
	if e.PersonID != "pstu3" || e.OfferingID != "f-id" {
		t.Fatalf("enrollment = %+v", e)
	}
}

// TestPopulateRejectsBeforeWriting checks a bad bundle stores nothing.
func TestPopulateRejectsBeforeWriting(t *testing.T) {
	cases := map[string]func(b *domain.Bundle){
		"unknown reference": func(b *domain.Bundle) { b.Results[0].CourseID = "nope" },
		"invalid record":    func(b *domain.Bundle) { b.Courses[0].Title = "" },
		"duplicate":         func(b *domain.Bundle) { b.Courses = append(b.Courses, b.Courses[0]) },
		"missing sourcedId": func(b *domain.Bundle) { b.AcademicSessions[0].SourcedID = "" },
		"parent cycle":      func(b *domain.Bundle) { b.Organizations[1].ParentID = "dept" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			c := newFixture(t).c
			b := sampleBundle()
			mutate(b)
			_, err := c.Populate(ctx, b, nil)
			if _, ok := errors.AsType[*domain.ValidationError](err); !ok {
				t.Fatalf("want ValidationError, got %v", err)
			}
			if page, _ := c.Organizations.List(ctx, port.ListParams{}); page.Total != 0 {
				t.Fatalf("nothing must be written, got %d organizations", page.Total)
			}
		})
	}
}
