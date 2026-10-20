package readmodel

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/logger"
)

// countingPersons counts GetByIDs and single-record lookups so tests can
// assert reference resolution is batched.
type countingPersons struct {
	*memory.PersonRepository
	bulk, single int
}

func (r *countingPersons) GetByIDs(ctx context.Context, ids []string) ([]domain.Person, error) {
	r.bulk++
	return r.PersonRepository.GetByIDs(ctx, ids)
}

func (r *countingPersons) Get(ctx context.Context, id string) (domain.Person, error) {
	r.single++
	return r.PersonRepository.Get(ctx, id)
}

func (r *countingPersons) GetBySourcedID(ctx context.Context, sid string) (domain.Person, error) {
	r.single++
	return r.PersonRepository.GetBySourcedID(ctx, sid)
}

func (r *countingPersons) GetBySourcedIDs(ctx context.Context, sids []string) ([]domain.Person, error) {
	r.bulk++
	return r.PersonRepository.GetBySourcedIDs(ctx, sids)
}

type countingDocs[T any, PT domain.EntityPtr[T]] struct {
	port.Repository[T, PT]
	bulk, single int
}

func (r *countingDocs[T, PT]) GetByIDs(ctx context.Context, ids []string) ([]PT, error) {
	r.bulk++
	return r.Repository.GetByIDs(ctx, ids)
}

func (r *countingDocs[T, PT]) Get(ctx context.Context, id string) (PT, error) {
	r.single++
	return r.Repository.Get(ctx, id)
}

func (r *countingDocs[T, PT]) GetBySourcedID(ctx context.Context, sid string) (PT, error) {
	r.single++
	return r.Repository.GetBySourcedID(ctx, sid)
}

func (r *countingDocs[T, PT]) GetBySourcedIDs(ctx context.Context, sids []string) ([]PT, error) {
	r.bulk++
	return r.Repository.GetBySourcedIDs(ctx, sids)
}

type fixture struct {
	c        *Client
	persons  *countingPersons
	offering *countingDocs[domain.Offering, *domain.Offering]
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	persons := &countingPersons{PersonRepository: memory.NewPersonRepository()}
	offerings := &countingDocs[domain.Offering, *domain.Offering]{Repository: memory.NewDocRepo[domain.Offering]()}
	return fixture{
		c: &Client{
			Persons:          service.New(persons, nil, nil, log),
			Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
			AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
			Courses:          service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
			Offerings:        service.NewResource(port.Repository[domain.Offering, *domain.Offering](offerings), nil, nil, log),
			Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
			Programs:         service.NewResource(memory.NewDocRepo[domain.Program](), nil, nil, log),
			Results:          service.NewResource(memory.NewDocRepo[domain.Result](), nil, nil, log),
		},
		persons:  persons,
		offering: offerings,
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestPublishRefsUsesSourcedIDs seeds records whose canonical id differs from
// their sourcedId and checks every reference is published as the sourcedId.
func TestPublishRefsUsesSourcedIDs(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.c
	_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p-id", SourcedID: "student-1", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	must(t, err)
	_, err = c.Organizations.Create(ctx, &domain.Organization{Base: domain.Base{ID: "o-id", SourcedID: "uni"}, Name: "U"})
	must(t, err)
	_, err = c.AcademicSessions.Create(ctx, &domain.AcademicSession{Base: domain.Base{ID: "s-id", SourcedID: "ht26"}, Title: "HT26"})
	must(t, err)
	_, err = c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c-id", SourcedID: "DA101"}, Title: "C", OrgID: "o-id"})
	must(t, err)
	_, err = c.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: "f-id", SourcedID: "DA101-ht26"}, CourseID: "c-id", AcademicSessionID: "s-id", OrgID: "o-id"})
	must(t, err)

	child := &domain.Organization{ParentID: "o-id"}
	course := &domain.Course{OrgID: "o-id"}
	offering := &domain.Offering{CourseID: "c-id", AcademicSessionID: "s-id", OrgID: "o-id"}
	enrollment := &domain.Enrollment{PersonID: "p-id", OfferingID: "f-id"}
	dangling := &domain.Enrollment{PersonID: "gone", OfferingID: "f-id"}
	ok, err := c.PublishRefs(ctx, child, course, offering, enrollment, dangling)
	must(t, err)
	if want := []bool{true, true, true, true, false}; !slices.Equal(ok, want) {
		t.Errorf("publishable = %v, want %v", ok, want)
	}

	checks := []struct{ name, got, want string }{
		{"organization.parent", child.ParentID, "uni"},
		{"course.org", course.OrgID, "uni"},
		{"offering.course", offering.CourseID, "DA101"},
		{"offering.session", offering.AcademicSessionID, "ht26"},
		{"offering.org", offering.OrgID, "uni"},
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

// TestPublishRefsNeverFallsBackToSourcedID is the regression test for a
// dangling reference being reinterpreted as a sourcedId: person A has id
// "p1", person B has sourcedId "p1"; once A is deleted, an enrollment that
// referenced A must publish no person, not B.
func TestPublishRefsNeverFallsBackToSourcedID(t *testing.T) {
	ctx := context.Background()
	c := newFixture(t).c
	_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p1", SourcedID: "a", GivenName: "A", FamilyName: "A", Status: domain.StatusActive})
	must(t, err)
	_, err = c.Persons.CreatePerson(ctx, domain.Person{ID: "p2", SourcedID: "p1", GivenName: "B", FamilyName: "B", Status: domain.StatusActive})
	must(t, err)
	must(t, c.Persons.DeletePerson(ctx, "p1"))

	e := &domain.Enrollment{PersonID: "p1"}
	_, err = c.PublishRefs(ctx, e)
	must(t, err)
	if e.PersonID != "" {
		t.Fatalf("dangling reference published as %q; want it cleared", e.PersonID)
	}
}

// TestPublishRefsBatchesLookups checks a page of many records with distinct
// references costs one bulk lookup per referenced type and no per-record
// lookups.
func TestPublishRefsBatchesLookups(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.c
	_, err := c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c"}, Title: "C"})
	must(t, err)
	const n = 50
	page := make([]domain.Entity, 0, n)
	for i := range n {
		pid, fid := "p"+strconv.Itoa(i), "f"+strconv.Itoa(i)
		_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: pid, GivenName: "G", FamilyName: "F", Status: domain.StatusActive})
		must(t, err)
		_, err = c.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: fid}, CourseID: "c"})
		must(t, err)
		page = append(page, &domain.Enrollment{PersonID: pid, OfferingID: fid})
	}
	f.persons.bulk, f.persons.single, f.offering.bulk, f.offering.single = 0, 0, 0, 0

	_, err = c.PublishRefs(ctx, page...)
	must(t, err)
	if f.persons.bulk != 1 || f.persons.single != 0 {
		t.Errorf("persons: %d bulk / %d single lookups, want 1 / 0", f.persons.bulk, f.persons.single)
	}
	if f.offering.bulk != 1 || f.offering.single != 0 {
		t.Errorf("offerings: %d bulk / %d single lookups, want 1 / 0", f.offering.bulk, f.offering.single)
	}
	for i, e := range page {
		if en := e.(*domain.Enrollment); en.PersonID == "" || en.OfferingID == "" {
			t.Fatalf("enrollment %d not resolved: %+v", i, en)
		}
	}
}

// TestPublishRefsFlagsOrphans deletes the targets of required references
// after the referencing records were stored and checks PublishRefs reports
// those records as unpublishable, while a dangling optional reference is
// only cleared. An enrollment whose offering lost its course is an orphan
// too: its offering is not published, so the link could not be followed.
func TestPublishRefsFlagsOrphans(t *testing.T) {
	ctx := context.Background()
	c := newFixture(t).c
	_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	must(t, err)
	_, err = c.Persons.CreatePerson(ctx, domain.Person{ID: "p-gone", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	must(t, err)
	for _, id := range []string{"c", "c-gone"} {
		_, err = c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: id}, Title: "C"})
		must(t, err)
	}
	_, err = c.AcademicSessions.Create(ctx, &domain.AcademicSession{Base: domain.Base{ID: "s-gone"}, Title: "S"})
	must(t, err)
	for _, o := range []*domain.Offering{
		{Base: domain.Base{ID: "f"}, CourseID: "c", AcademicSessionID: "s-gone"},
		{Base: domain.Base{ID: "f-gone"}, CourseID: "c"},
		{Base: domain.Base{ID: "f-orphan"}, CourseID: "c-gone"},
	} {
		_, err = c.Offerings.Create(ctx, o)
		must(t, err)
	}
	must(t, c.Persons.DeletePerson(ctx, "p-gone"))
	must(t, c.Offerings.Delete(ctx, "f-gone"))
	must(t, c.Courses.Delete(ctx, "c-gone"))
	must(t, c.AcademicSessions.Delete(ctx, "s-gone"))

	cases := []struct {
		name string
		e    domain.Entity
		want bool
	}{
		{"intact enrollment", &domain.Enrollment{PersonID: "p", OfferingID: "f"}, true},
		{"person deleted", &domain.Enrollment{PersonID: "p-gone", OfferingID: "f"}, false},
		{"offering deleted", &domain.Enrollment{PersonID: "p", OfferingID: "f-gone"}, false},
		{"offering orphaned", &domain.Enrollment{PersonID: "p", OfferingID: "f-orphan"}, false},
		{"offering course deleted", &domain.Offering{CourseID: "c-gone"}, false},
		{"optional session deleted", &domain.Offering{CourseID: "c", AcademicSessionID: "s-gone"}, true},
		{"optional parent deleted", &domain.Organization{ParentID: "o-gone"}, true},
	}
	ents := make([]domain.Entity, len(cases))
	for i, tc := range cases {
		ents[i] = tc.e
	}
	ok, err := c.PublishRefs(ctx, ents...)
	must(t, err)
	for i, tc := range cases {
		if ok[i] != tc.want {
			t.Errorf("%s: publishable = %v, want %v", tc.name, ok[i], tc.want)
		}
	}
	if o := cases[5].e.(*domain.Offering); o.AcademicSessionID != "" {
		t.Errorf("dangling optional session published as %q; want it cleared", o.AcademicSessionID)
	}
}

// TestPublishRefsProgramsAndResults checks the program/result references:
// a result is an orphan when its person or its course/program is gone, but
// not when only its optional offering is.
func TestPublishRefsProgramsAndResults(t *testing.T) {
	ctx := t.Context()
	c := newFixture(t).c
	_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p", SourcedID: "stu", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	must(t, err)
	_, err = c.Organizations.Create(ctx, &domain.Organization{Base: domain.Base{ID: "o", SourcedID: "uni"}, Name: "U"})
	must(t, err)
	_, err = c.Programs.Create(ctx, &domain.Program{Base: domain.Base{ID: "g", SourcedID: "BSc"}, Title: "BSc", OrgID: "o"})
	must(t, err)
	_, err = c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c", SourcedID: "DA101"}, Title: "C"})
	must(t, err)

	program := &domain.Program{OrgID: "o"}
	forProgram := &domain.Result{PersonID: "p", ProgramID: "g"}
	forCourse := &domain.Result{PersonID: "p", CourseID: "c", OfferingID: "f-gone"}
	courseGone := &domain.Result{PersonID: "p", CourseID: "c-gone"}
	programGone := &domain.Result{PersonID: "p", ProgramID: "g-gone"}
	ok, err := c.PublishRefs(ctx, program, forProgram, forCourse, courseGone, programGone)
	must(t, err)
	if want := []bool{true, true, true, false, false}; !slices.Equal(ok, want) {
		t.Fatalf("publishable = %v, want %v", ok, want)
	}
	if program.OrgID != "uni" || forProgram.ProgramID != "BSc" || forCourse.CourseID != "DA101" || forCourse.PersonID != "stu" || forCourse.OfferingID != "" {
		t.Fatalf("refs not published as sourcedIds: %+v %+v %+v", program, forProgram, forCourse)
	}
	if err := c.CheckRefs(ctx, &domain.Result{PersonID: "p", ProgramID: "BSc"}); err == nil {
		t.Fatal("CheckRefs must refuse a program sourcedId")
	}
}

// TestUnknownRefTypeIsAnError checks a reference type missing from the
// lookup switches fails the request instead of panicking.
func TestUnknownRefTypeIsAnError(t *testing.T) {
	c := newFixture(t).c
	if _, err := c.sourcedIDs(t.Context(), "nope", []string{"x"}); err == nil {
		t.Fatal("sourcedIDs: expected an error")
	}
	if _, err := c.storedIDs(t.Context(), "nope", []string{"x"}); err == nil {
		t.Fatal("storedIDs: expected an error")
	}
}

// TestCheckRefsRequiresCanonicalIDs checks ingress accepts canonical ids of
// existing records and refuses a sourcedId or an unknown id, so stored
// references are never ambiguous.
func TestCheckRefsRequiresCanonicalIDs(t *testing.T) {
	ctx := context.Background()
	c := newFixture(t).c
	_, err := c.Persons.CreatePerson(ctx, domain.Person{ID: "p-id", SourcedID: "student-1", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	must(t, err)
	_, err = c.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c-id", SourcedID: "DA101"}, Title: "C"})
	must(t, err)
	_, err = c.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: "f-id", SourcedID: "DA101-ht26"}, CourseID: "c-id"})
	must(t, err)

	cases := []struct {
		name      string
		e         domain.Entity
		wantField string
	}{
		{"canonical ids", &domain.Enrollment{PersonID: "p-id", OfferingID: "f-id"}, ""},
		{"empty optional ref", &domain.Course{Title: "T"}, ""},
		{"sourcedId refused", &domain.Enrollment{PersonID: "student-1", OfferingID: "f-id"}, "personId"},
		{"unknown id refused", &domain.Enrollment{PersonID: "p-id", OfferingID: "nope"}, "offeringId"},
		{"offering sourcedId refused", &domain.Offering{CourseID: "DA101"}, "courseId"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.CheckRefs(ctx, tc.e)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			verr, ok := errors.AsType[*domain.ValidationError](err)
			if !ok || verr.Field != tc.wantField {
				t.Fatalf("want ValidationError on %s, got %v", tc.wantField, err)
			}
		})
	}
}
