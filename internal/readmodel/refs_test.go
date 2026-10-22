package readmodel

import (
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingPersons counts GetByIDs and single-record lookups so tests can
// assert reference resolution is batched.
type countingPersons struct {
	*memory.PersonRepository
	bulk, single int
}

func (repo *countingPersons) GetByIDs(ctx context.Context, ids []string) ([]domain.Person, error) {
	repo.bulk++
	return repo.PersonRepository.GetByIDs(ctx, ids)
}

func (repo *countingPersons) Get(ctx context.Context, id string) (domain.Person, error) {
	repo.single++
	return repo.PersonRepository.Get(ctx, id)
}

func (repo *countingPersons) GetBySourcedID(ctx context.Context, sid string) (domain.Person, error) {
	repo.single++
	return repo.PersonRepository.GetBySourcedID(ctx, sid)
}

func (repo *countingPersons) GetBySourcedIDs(ctx context.Context, sids []string) ([]domain.Person, error) {
	repo.bulk++
	return repo.PersonRepository.GetBySourcedIDs(ctx, sids)
}

type countingDocs[T any, PT domain.EntityPtr[T]] struct {
	port.Repository[T, PT]
	bulk, single int
}

func (repo *countingDocs[T, PT]) GetByIDs(ctx context.Context, ids []string) ([]PT, error) {
	repo.bulk++
	return repo.Repository.GetByIDs(ctx, ids)
}

func (repo *countingDocs[T, PT]) Get(ctx context.Context, id string) (PT, error) {
	repo.single++
	return repo.Repository.Get(ctx, id)
}

func (repo *countingDocs[T, PT]) GetBySourcedID(ctx context.Context, sid string) (PT, error) {
	repo.single++
	return repo.Repository.GetBySourcedID(ctx, sid)
}

func (repo *countingDocs[T, PT]) GetBySourcedIDs(ctx context.Context, sids []string) ([]PT, error) {
	repo.bulk++
	return repo.Repository.GetBySourcedIDs(ctx, sids)
}

type fixture struct {
	catalog  *Client
	persons  *countingPersons
	offering *countingDocs[domain.Offering, *domain.Offering]
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	persons := &countingPersons{PersonRepository: memory.NewPersonRepository()}
	offerings := &countingDocs[domain.Offering, *domain.Offering]{Repository: memory.NewDocRepo[domain.Offering]()}
	return fixture{
		catalog: &Client{
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

// TestPublishRefsUsesSourcedIDs seeds records whose canonical id differs from
// their sourcedId and checks every reference is published as the sourcedId.
func TestPublishRefsUsesSourcedIDs(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	_, err := catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-id", SourcedID: "student-1", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	require.NoError(t, err)
	_, err = catalog.Organizations.Create(ctx, &domain.Organization{ID: "o-id", SourcedID: "uni", Name: "U"})
	require.NoError(t, err)
	_, err = catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{ID: "s-id", SourcedID: "ht26", Title: "HT26"})
	require.NoError(t, err)
	_, err = catalog.Courses.Create(ctx, &domain.Course{ID: "c-id", SourcedID: "DA101", Title: "C", OrgID: "o-id"})
	require.NoError(t, err)
	_, err = catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-id", SourcedID: "DA101-ht26", CourseID: "c-id", AcademicSessionID: "s-id", OrgID: "o-id"})
	require.NoError(t, err)

	child := &domain.Organization{ParentID: "o-id"}
	course := &domain.Course{OrgID: "o-id"}
	offering := &domain.Offering{CourseID: "c-id", AcademicSessionID: "s-id", OrgID: "o-id"}
	enrollment := &domain.Enrollment{PersonID: "p-id", OfferingID: "f-id"}
	dangling := &domain.Enrollment{PersonID: "gone", OfferingID: "f-id"}
	publishable, err := catalog.PublishRefs(ctx, child, course, offering, enrollment, dangling)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, true, true, true, false}, publishable, "publishable")

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
	for _, check := range checks {
		assert.Equal(t, check.want, check.got, check.name)
	}
}

// TestPublishRefsNeverFallsBackToSourcedID is the regression test for a
// dangling reference being reinterpreted as a sourcedId: person A has id
// "p1", person B has sourcedId "p1"; once A is deleted, an enrollment that
// referenced A must publish no person, not B.
func TestPublishRefsNeverFallsBackToSourcedID(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	_, err := catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p1", SourcedID: "a", GivenName: "A", FamilyName: "A", Status: domain.StatusActive})
	require.NoError(t, err)
	_, err = catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p2", SourcedID: "p1", GivenName: "B", FamilyName: "B", Status: domain.StatusActive})
	require.NoError(t, err)
	require.NoError(t, catalog.Persons.DeletePerson(ctx, "p1"))

	enrollment := &domain.Enrollment{PersonID: "p1"}
	_, err = catalog.PublishRefs(ctx, enrollment)
	require.NoError(t, err)
	assert.Empty(t, enrollment.PersonID, "dangling reference must be cleared")
}

// TestPublishRefsBatchesLookups checks a page of many records with distinct
// references costs one bulk lookup per referenced type and no per-record
// lookups.
func TestPublishRefsBatchesLookups(t *testing.T) {
	ctx := t.Context()
	fx := newFixture(t)
	catalog := fx.catalog
	_, err := catalog.Courses.Create(ctx, &domain.Course{ID: "c", Title: "C"})
	require.NoError(t, err)
	const count = 50
	page := make([]domain.Entity, 0, count)
	for i := range count {
		pid, fid := "p"+strconv.Itoa(i), "f"+strconv.Itoa(i)
		_, err := catalog.Persons.CreatePerson(ctx, domain.Person{ID: pid, GivenName: "G", FamilyName: "F", Status: domain.StatusActive})
		require.NoError(t, err)
		_, err = catalog.Offerings.Create(ctx, &domain.Offering{ID: fid, CourseID: "c"})
		require.NoError(t, err)
		page = append(page, &domain.Enrollment{PersonID: pid, OfferingID: fid})
	}
	fx.persons.bulk, fx.persons.single, fx.offering.bulk, fx.offering.single = 0, 0, 0, 0

	_, err = catalog.PublishRefs(ctx, page...)
	require.NoError(t, err)
	assert.Equal(t, 1, fx.persons.bulk, "persons bulk lookups")
	assert.Equal(t, 0, fx.persons.single, "persons single lookups")
	assert.Equal(t, 1, fx.offering.bulk, "offerings bulk lookups")
	assert.Equal(t, 0, fx.offering.single, "offerings single lookups")
	for i, entity := range page {
		enrollment := entity.(*domain.Enrollment)
		require.NotEmpty(t, enrollment.PersonID, "enrollment %d person not resolved", i)
		require.NotEmpty(t, enrollment.OfferingID, "enrollment %d offering not resolved", i)
	}
}

// TestPublishRefsFlagsOrphans deletes the targets of required references
// after the referencing records were stored and checks PublishRefs reports
// those records as unpublishable, while a dangling optional reference is
// only cleared. An enrollment whose offering lost its course is an orphan
// too: its offering is not published, so the link could not be followed.
func TestPublishRefsFlagsOrphans(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	_, err := catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	require.NoError(t, err)
	_, err = catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-gone", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	require.NoError(t, err)
	for _, id := range []string{"c", "c-gone"} {
		_, err = catalog.Courses.Create(ctx, &domain.Course{ID: id, Title: "C"})
		require.NoError(t, err)
	}
	_, err = catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{ID: "s-gone", Title: "S"})
	require.NoError(t, err)
	for _, offering := range []*domain.Offering{
		{ID: "f", CourseID: "c", AcademicSessionID: "s-gone"},
		{ID: "f-gone", CourseID: "c"},
		{ID: "f-orphan", CourseID: "c-gone"},
	} {
		_, err = catalog.Offerings.Create(ctx, offering)
		require.NoError(t, err)
	}
	require.NoError(t, catalog.Persons.DeletePerson(ctx, "p-gone"))
	require.NoError(t, catalog.Offerings.Delete(ctx, "f-gone"))
	require.NoError(t, catalog.Courses.Delete(ctx, "c-gone"))
	require.NoError(t, catalog.AcademicSessions.Delete(ctx, "s-gone"))

	cases := []struct {
		name   string
		entity domain.Entity
		want   bool
	}{
		{"intact enrollment", &domain.Enrollment{PersonID: "p", OfferingID: "f"}, true},
		{"person deleted", &domain.Enrollment{PersonID: "p-gone", OfferingID: "f"}, false},
		{"offering deleted", &domain.Enrollment{PersonID: "p", OfferingID: "f-gone"}, false},
		{"offering orphaned", &domain.Enrollment{PersonID: "p", OfferingID: "f-orphan"}, false},
		{"offering course deleted", &domain.Offering{CourseID: "c-gone"}, false},
		{"optional session deleted", &domain.Offering{CourseID: "c", AcademicSessionID: "s-gone"}, true},
		{"optional parent deleted", &domain.Organization{ParentID: "o-gone"}, true},
	}
	entities := make([]domain.Entity, len(cases))
	for i, tc := range cases {
		entities[i] = tc.entity
	}
	publishable, err := catalog.PublishRefs(ctx, entities...)
	require.NoError(t, err)
	require.Len(t, publishable, len(cases))
	for i, tc := range cases {
		assert.Equal(t, tc.want, publishable[i], "%s: publishable", tc.name)
	}
	offering := cases[5].entity.(*domain.Offering)
	assert.Empty(t, offering.AcademicSessionID, "dangling optional session must be cleared")
}

// TestPublishRefsProgramsAndResults checks the program/result references:
// a result is an orphan when its person or its course/program is gone, but
// not when only its optional offering is.
func TestPublishRefsProgramsAndResults(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	_, err := catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p", SourcedID: "stu", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	require.NoError(t, err)
	_, err = catalog.Organizations.Create(ctx, &domain.Organization{ID: "o", SourcedID: "uni", Name: "U"})
	require.NoError(t, err)
	_, err = catalog.Programs.Create(ctx, &domain.Program{ID: "g", SourcedID: "BSc", Title: "BSc", OrgID: "o"})
	require.NoError(t, err)
	_, err = catalog.Courses.Create(ctx, &domain.Course{ID: "c", SourcedID: "DA101", Title: "C"})
	require.NoError(t, err)

	program := &domain.Program{OrgID: "o"}
	forProgram := &domain.Result{PersonID: "p", ProgramID: "g"}
	forCourse := &domain.Result{PersonID: "p", CourseID: "c", OfferingID: "f-gone"}
	courseGone := &domain.Result{PersonID: "p", CourseID: "c-gone"}
	programGone := &domain.Result{PersonID: "p", ProgramID: "g-gone"}
	publishable, err := catalog.PublishRefs(ctx, program, forProgram, forCourse, courseGone, programGone)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true, true, false, false}, publishable, "publishable")
	assert.Equal(t, "uni", program.OrgID, "program.org")
	assert.Equal(t, "BSc", forProgram.ProgramID, "result.program")
	assert.Equal(t, "DA101", forCourse.CourseID, "result.course")
	assert.Equal(t, "stu", forCourse.PersonID, "result.person")
	assert.Empty(t, forCourse.OfferingID, "dangling optional offering must be cleared")
	assert.Error(t, catalog.CheckRefs(ctx, &domain.Result{PersonID: "p", ProgramID: "BSc"}), "CheckRefs must refuse a program sourcedId")
}

// TestUnknownRefTypeIsAnError checks a reference type missing from the
// lookup switches fails the request instead of panicking.
func TestUnknownRefTypeIsAnError(t *testing.T) {
	catalog := newFixture(t).catalog
	_, err := catalog.sourcedIDs(t.Context(), "nope", []string{"x"})
	assert.Error(t, err, "sourcedIDs")
	_, err = catalog.storedIDs(t.Context(), "nope", []string{"x"})
	assert.Error(t, err, "storedIDs")
}

// TestCheckRefsRequiresCanonicalIDs checks ingress accepts canonical ids of
// existing records and refuses a sourcedId or an unknown id, so stored
// references are never ambiguous.
func TestCheckRefsRequiresCanonicalIDs(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	_, err := catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-id", SourcedID: "student-1", GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
	require.NoError(t, err)
	_, err = catalog.Courses.Create(ctx, &domain.Course{ID: "c-id", SourcedID: "DA101", Title: "C"})
	require.NoError(t, err)
	_, err = catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-id", SourcedID: "DA101-ht26", CourseID: "c-id"})
	require.NoError(t, err)

	cases := []struct {
		name      string
		entity    domain.Entity
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
			err := catalog.CheckRefs(t.Context(), tc.entity)
			if tc.wantField == "" {
				assert.NoError(t, err)
				return
			}
			var verr *domain.ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tc.wantField, verr.Field)
		})
	}
}
