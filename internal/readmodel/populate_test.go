package readmodel

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleBundle() *domain.Bundle {
	base := func(sid string) domain.Base { return domain.Base{SourcedID: sid, Status: domain.StatusActive} }
	return &domain.Bundle{
		Persons: []*domain.Person{{SourcedID: "stu", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		// Child listed before its parent: Populate must reorder.
		Organizations: []*domain.Organization{
			{Base: base("dept"), Name: "D", ParentID: "uni"},
			{Base: base("uni"), Name: "U"},
		},
		AcademicSessions: []*domain.AcademicSession{{Base: base("ht25"), Title: "HT25"}},
		Courses:          []*domain.Course{{Base: base("BI101"), Title: "Bio", OrgID: "dept"}},
		Programs:         []*domain.Program{{Base: base("BSc"), Title: "BSc", OrgID: "uni"}},
		Offerings:        []*domain.Offering{{Base: base("BI101-ht25"), CourseID: "BI101", AcademicSessionID: "ht25"}},
		Enrollments:      []*domain.Enrollment{{Base: base("enr"), PersonID: "stu", OfferingID: "BI101-ht25", Role: domain.RoleStudent}},
		Results: []*domain.Result{
			{Base: base("r1"), PersonID: "stu", CourseID: "BI101", OfferingID: "BI101-ht25", Grade: "A"},
			{Base: base("r2"), PersonID: "stu", ProgramID: "BSc"},
		},
	}
}

// TestPopulateStoresWithCanonicalRefs checks every record is stored and its
// references point at canonical ids, so CheckRefs-style invariants hold and
// PublishRefs publishes the original sourcedIds.
func TestPopulateStoresWithCanonicalRefs(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	exp := time.Now().Add(time.Hour).UTC()
	items, err := catalog.Populate(ctx, sampleBundle(), &exp)
	require.NoError(t, err)
	require.Len(t, items, 10)
	for _, item := range items {
		require.Equal(t, ActionCreated, item.Action, "item %+v", item)
		require.NotEmpty(t, item.ID, "item %+v", item)
	}
	require.Equal(t, "uni", items[0].SourcedID, "parent must be stored first")

	result, err := catalog.Results.GetBySourcedID(ctx, "r1")
	require.NoError(t, err)
	require.NoError(t, catalog.CheckRefs(ctx, result))
	require.NotNil(t, result.ExpiresAt, "expiry not set")
	require.True(t, result.ExpiresAt.Equal(exp), "expiry = %v, want %v", result.ExpiresAt, exp)
	publishable, err := catalog.PublishRefs(ctx, result)
	require.NoError(t, err)
	require.Equal(t, []bool{true}, publishable, "publishable")
	require.Equal(t, "stu", result.PersonID, "published personId")
	require.Equal(t, "BI101", result.CourseID, "published courseId")
	require.Equal(t, "BI101-ht25", result.OfferingID, "published offeringId")
	dept, err := catalog.Organizations.GetBySourcedID(ctx, "dept")
	require.NoError(t, err)
	require.NoError(t, catalog.CheckRefs(ctx, dept))

	// Re-populating converges: same ids, all updated.
	again, err := catalog.Populate(ctx, sampleBundle(), &exp)
	require.NoError(t, err)
	require.Len(t, again, len(items))
	for i, item := range again {
		require.Equal(t, ActionUpdated, item.Action, "re-populate item %d", i)
		require.Equal(t, items[i].ID, item.ID, "re-populate item %d", i)
	}
	page, err := catalog.Results.List(ctx, port.ListParams{})
	require.NoError(t, err)
	assert.Equal(t, 2, page.Total, "results")
}

// TestPopulateNewEntities checks the new entities store with canonical
// references whatever order the bundle lists them in, including list
// references, and publish back as sourcedIds.
func TestPopulateNewEntities(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	base := func(sid string) domain.Base { return domain.Base{SourcedID: sid, Status: domain.StatusActive} }
	bundle := &domain.Bundle{
		Persons:          []*domain.Person{{SourcedID: "stu", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Organizations:    []*domain.Organization{{Base: base("uni"), Name: "U"}},
		AcademicSessions: []*domain.AcademicSession{{Base: base("ht26"), Title: "HT26"}},
		Programs:         []*domain.Program{{Base: base("BSc"), Title: "BSc"}},
		Courses:          []*domain.Course{{Base: base("DA101"), Title: "C", ProgramIDs: []string{"BSc"}}},
		// Dependants listed before what they reference: Populate must order the writes.
		ComponentOfferings:  []*domain.ComponentOffering{{Base: base("lab-ht26"), ComponentID: "lab", AcademicSessionID: "ht26", CourseOfferingIDs: []string{"DA101-ht26"}}},
		CollectionOfferings: []*domain.CollectionOffering{{Base: base("BSc-ht26"), ProgramID: "BSc", AcademicSessionID: "ht26", OrgID: "uni"}},
		ComponentTemplates:  []*domain.ComponentTemplate{{Base: base("lab"), Title: "Lab", Type: "laboratory", CourseIDs: []string{"DA101"}}},
		Offerings:           []*domain.Offering{{Base: base("DA101-ht26"), CourseID: "DA101", AcademicSessionID: "ht26", CollectionOfferingIDs: []string{"BSc-ht26"}}},
		Affiliations:        []*domain.Affiliation{{Base: base("stu-uni"), PersonID: "stu", OrgID: "uni", Role: domain.RoleStudent}},
	}
	items, err := catalog.Populate(ctx, bundle, nil)
	require.NoError(t, err)
	require.Len(t, items, bundle.Len())

	course, err := catalog.Courses.GetBySourcedID(ctx, "DA101")
	require.NoError(t, err)
	program, err := catalog.Programs.GetBySourcedID(ctx, "BSc")
	require.NoError(t, err)
	assert.Equal(t, []string{program.ID}, course.ProgramIDs, "stored as the program's canonical id")
	require.NoError(t, catalog.CheckRefs(ctx, course))

	offering, err := catalog.Offerings.GetBySourcedID(ctx, "DA101-ht26")
	require.NoError(t, err)
	collection, err := catalog.CollectionOfferings.GetBySourcedID(ctx, "BSc-ht26")
	require.NoError(t, err)
	assert.Equal(t, []string{collection.ID}, offering.CollectionOfferingIDs)
	assert.Equal(t, program.ID, collection.ProgramID)

	component, err := catalog.ComponentOfferings.GetBySourcedID(ctx, "lab-ht26")
	require.NoError(t, err)
	require.NoError(t, catalog.CheckRefs(ctx, component))
	published, err := catalog.PublishRefs(ctx, component)
	require.NoError(t, err)
	require.Equal(t, []bool{true}, published)
	assert.Equal(t, "lab", component.ComponentID)
	assert.Equal(t, []string{"DA101-ht26"}, component.CourseOfferingIDs)
	affiliation, err := catalog.Affiliations.GetBySourcedID(ctx, "stu-uni")
	require.NoError(t, err)
	require.NoError(t, catalog.CheckRefs(ctx, affiliation))

	again, err := catalog.Populate(ctx, sampleNewEntitiesBundle(bundle), nil)
	require.NoError(t, err)
	for i, item := range again {
		assert.Equal(t, ActionUpdated, item.Action, "re-populate item %d (%s %s)", i, item.Type, item.SourcedID)
	}
}

// sampleNewEntitiesBundle returns a fresh copy of the bundle's records:
// Populate consumes its argument by rewriting references in place.
func sampleNewEntitiesBundle(b *domain.Bundle) *domain.Bundle {
	base := func(sid string) domain.Base { return domain.Base{SourcedID: sid, Status: domain.StatusActive} }
	return &domain.Bundle{
		Persons:             []*domain.Person{{SourcedID: "stu", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Organizations:       []*domain.Organization{{Base: base("uni"), Name: "U"}},
		AcademicSessions:    []*domain.AcademicSession{{Base: base("ht26"), Title: "HT26"}},
		Programs:            []*domain.Program{{Base: base("BSc"), Title: "BSc"}},
		Courses:             []*domain.Course{{Base: base("DA101"), Title: "C", ProgramIDs: []string{"BSc"}}},
		ComponentOfferings:  []*domain.ComponentOffering{{Base: base("lab-ht26"), ComponentID: "lab", AcademicSessionID: "ht26", CourseOfferingIDs: []string{"DA101-ht26"}}},
		CollectionOfferings: []*domain.CollectionOffering{{Base: base("BSc-ht26"), ProgramID: "BSc", AcademicSessionID: "ht26", OrgID: "uni"}},
		ComponentTemplates:  []*domain.ComponentTemplate{{Base: base("lab"), Title: "Lab", Type: "laboratory", CourseIDs: []string{"DA101"}}},
		Offerings:           []*domain.Offering{{Base: base("DA101-ht26"), CourseID: "DA101", AcademicSessionID: "ht26", CollectionOfferingIDs: []string{"BSc-ht26"}}},
		Affiliations:        []*domain.Affiliation{{Base: base("stu-uni"), PersonID: "stu", OrgID: "uni", Role: domain.RoleStudent}},
	}
}

// TestPopulateRejectsBadListRefs checks an unknown entry in a list reference
// fails the whole bundle before anything is stored.
func TestPopulateRejectsBadListRefs(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	base := func(sid string) domain.Base { return domain.Base{SourcedID: sid, Status: domain.StatusActive} }
	_, err := catalog.Populate(ctx, &domain.Bundle{
		Programs: []*domain.Program{{Base: base("BSc"), Title: "BSc"}},
		Courses:  []*domain.Course{{Base: base("DA101"), Title: "C", ProgramIDs: []string{"BSc", "nowhere"}}},
	}, nil)
	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "course[DA101].programIds[1]", verr.Field)
	assert.Contains(t, verr.Msg, "nowhere")
	page, err := catalog.Programs.List(ctx, port.ListParams{})
	require.NoError(t, err)
	assert.Zero(t, page.Total, "nothing is stored when the bundle is rejected")
}

// TestPopulateResolvesStoredRefs checks a reference may name an already
// stored record by sourcedId.
func TestPopulateResolvesStoredRefs(t *testing.T) {
	ctx := t.Context()
	catalog := newFixture(t).catalog
	_, err := catalog.Courses.Create(ctx, &domain.Course{ID: "c-id", SourcedID: "DA101", Title: "C"})
	require.NoError(t, err)
	bundle := &domain.Bundle{
		Persons: []*domain.Person{{SourcedID: "stu", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Results: []*domain.Result{{SourcedID: "r", PersonID: "stu", CourseID: "DA101"}},
	}
	_, err = catalog.Populate(ctx, bundle, nil)
	require.NoError(t, err)
	result, err := catalog.Results.GetBySourcedID(ctx, "r")
	require.NoError(t, err)
	assert.Equal(t, "c-id", result.CourseID)
	assert.Nil(t, result.ExpiresAt)
}

// TestPopulateBatchesStoredRefs checks references to stored records cost one
// lookup per type, however many records hold them.
func TestPopulateBatchesStoredRefs(t *testing.T) {
	ctx := t.Context()
	fx := newFixture(t)
	catalog := fx.catalog
	_, err := catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-id", SourcedID: "DA101-ht26", CourseID: "c-id"})
	require.NoError(t, err)
	bundle := &domain.Bundle{}
	for i := range 5 {
		sid := "stu" + strconv.Itoa(i)
		_, err := catalog.Persons.Create(ctx, &domain.Person{ID: "p" + sid, SourcedID: sid, GivenName: "A", FamilyName: "B", Status: domain.StatusActive})
		require.NoError(t, err)
		bundle.Enrollments = append(bundle.Enrollments, &domain.Enrollment{
			SourcedID: "enr" + strconv.Itoa(i), Status: domain.StatusActive,
			PersonID: sid, OfferingID: "DA101-ht26", Role: domain.RoleStudent,
		})
	}
	fx.persons.bulk, fx.persons.single, fx.offering.bulk, fx.offering.single = 0, 0, 0, 0

	_, err = catalog.Populate(ctx, bundle, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, fx.persons.bulk, "persons bulk lookups")
	assert.Equal(t, 0, fx.persons.single, "persons single lookups")
	assert.Equal(t, 1, fx.offering.bulk, "offerings bulk lookups")
	assert.Equal(t, 0, fx.offering.single, "offerings single lookups")
	enrollment, err := catalog.Enrollments.GetBySourcedID(ctx, "enr3")
	require.NoError(t, err)
	assert.Equal(t, "pstu3", enrollment.PersonID)
	assert.Equal(t, "f-id", enrollment.OfferingID)
}

// TestPopulateStopsOnCancel checks a cancelled ctx stops Populate before any
// write, even on the memory store, which ignores ctx.
func TestPopulateStopsOnCancel(t *testing.T) {
	catalog := newFixture(t).catalog
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items, err := catalog.Populate(ctx, sampleBundle(), nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, items)
	page, err := catalog.Organizations.List(t.Context(), port.ListParams{})
	require.NoError(t, err)
	assert.Zero(t, page.Total, "nothing must be written")
}

// TestPopulateRejectsBeforeWriting checks a bad bundle stores nothing.
func TestPopulateRejectsBeforeWriting(t *testing.T) {
	cases := map[string]func(bundle *domain.Bundle){
		"unknown reference": func(bundle *domain.Bundle) { bundle.Results[0].CourseID = "nope" },
		"invalid record":    func(bundle *domain.Bundle) { bundle.Courses[0].Title = "" },
		"duplicate":         func(bundle *domain.Bundle) { bundle.Courses = append(bundle.Courses, bundle.Courses[0]) },
		"missing sourcedId": func(bundle *domain.Bundle) { bundle.AcademicSessions[0].SourcedID = "" },
		"parent cycle":      func(bundle *domain.Bundle) { bundle.Organizations[1].ParentID = "dept" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			catalog := newFixture(t).catalog
			bundle := sampleBundle()
			mutate(bundle)
			_, err := catalog.Populate(ctx, bundle, nil)
			var verr *domain.ValidationError
			require.ErrorAs(t, err, &verr)
			page, _ := catalog.Organizations.List(ctx, port.ListParams{})
			assert.Zero(t, page.Total, "nothing must be written")
		})
	}
}
