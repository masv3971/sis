package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
)

// TestProvidersPublishReferencesAsSourcedIDs seeds records whose canonical id
// differs from their sourcedId and checks that every relationship the OEAPI
// and Edu-API providers publish carries the sourcedId, i.e. can be followed
// through the same provider.
func TestProvidersPublishReferencesAsSourcedIDs(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:          service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
	}
	must := func(_ any, err error) {
		t.Helper()
		require.NoError(t, err)
	}
	must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-internal", SourcedID: "p-external", GivenName: "A", FamilyName: "B", Status: domain.StatusActive}))
	must(catalog.Organizations.Create(ctx, &domain.Organization{ID: "o-root", SourcedID: "uni", Name: "U"}))
	must(catalog.Organizations.Create(ctx, &domain.Organization{ID: "o-dept", SourcedID: "dept", Name: "D", ParentID: "o-root"}))
	must(catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{ID: "s-internal", SourcedID: "ht26", Title: "HT26"}))
	must(catalog.Courses.Create(ctx, &domain.Course{ID: "c-internal", SourcedID: "DA101", Title: "C", OrgID: "o-dept"}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-internal", SourcedID: "DA101-ht26", CourseID: "c-internal", AcademicSessionID: "s-internal", OrgID: "o-dept"}))
	must(catalog.Enrollments.Create(ctx, &domain.Enrollment{ID: "e-internal", SourcedID: "enr-1", PersonID: "p-internal", OfferingID: "f-internal", Role: domain.RoleStudent}))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")
	base := "http://" + srv.Addr().String()

	get := func(t *testing.T, path string, out any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "GET %s", path)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s", path)
		if out != nil {
			require.NoError(t, json.NewDecoder(resp.Body).Decode(out), "GET %s: decode", path)
		}
	}

	t.Run("oeapi", func(t *testing.T) {
		var org struct{ ParentOrganization string }
		get(t, "/oeapi/organizations/dept", &org)
		var course struct{ OrganizationID string }
		get(t, "/oeapi/courses/DA101", &course)
		var off struct{ CourseID, AcademicSessionID string }
		get(t, "/oeapi/offerings/DA101-ht26", &off)
		var assoc struct{ PersonID, OfferingID string }
		get(t, "/oeapi/associations/enr-1", &assoc)
		var list struct {
			Items []struct{ PersonID, OfferingID string }
		}
		get(t, "/oeapi/associations", &list)
		require.Len(t, list.Items, 1)

		checks := []struct{ name, got, want string }{
			{"organization.parentOrganization", org.ParentOrganization, "uni"},
			{"course.organizationId", course.OrganizationID, "dept"},
			{"offering.courseId", off.CourseID, "DA101"},
			{"offering.academicSessionId", off.AcademicSessionID, "ht26"},
			{"association.personId", assoc.PersonID, "p-external"},
			{"association.offeringId", assoc.OfferingID, "DA101-ht26"},
			{"list association.personId", list.Items[0].PersonID, "p-external"},
		}
		for _, check := range checks {
			assert.Equal(t, check.want, check.got, check.name)
		}
		// Each published reference resolves through the provider.
		get(t, "/oeapi/organizations/"+org.ParentOrganization, nil)
		get(t, "/oeapi/persons/"+assoc.PersonID, nil)
		get(t, "/oeapi/offerings/"+assoc.OfferingID, nil)
		get(t, "/oeapi/courses/"+off.CourseID, nil)
		get(t, "/oeapi/academic-sessions/"+off.AcademicSessionID, nil)
	})

	t.Run("eduapi", func(t *testing.T) {
		type ref struct{ SourcedID string }
		var org struct{ Org struct{ Parent *ref } }
		get(t, "/eduapi/orgs/dept", &org)
		var course struct{ Course struct{ Org *ref } }
		get(t, "/eduapi/courses/DA101", &course)
		var class struct {
			Class struct {
				Course *ref
				Terms  []ref
				School *ref
			}
		}
		get(t, "/eduapi/classes/DA101-ht26", &class)
		var enr struct{ Enrollment struct{ User, Class ref } }
		get(t, "/eduapi/enrollments/enr-1", &enr)

		sid := func(reference *ref) string {
			if reference == nil {
				return ""
			}
			return reference.SourcedID
		}
		var term string
		if len(class.Class.Terms) == 1 {
			term = class.Class.Terms[0].SourcedID
		}
		checks := []struct{ name, got, want string }{
			{"org.parent", sid(org.Org.Parent), "uni"},
			{"course.org", sid(course.Course.Org), "dept"},
			{"class.course", sid(class.Class.Course), "DA101"},
			{"class.terms", term, "ht26"},
			{"class.school", sid(class.Class.School), "dept"},
			{"enrollment.user", enr.Enrollment.User.SourcedID, "p-external"},
			{"enrollment.class", enr.Enrollment.Class.SourcedID, "DA101-ht26"},
		}
		for _, check := range checks {
			assert.Equal(t, check.want, check.got, check.name)
		}
		get(t, "/eduapi/persons/"+enr.Enrollment.User.SourcedID, nil)
		get(t, "/eduapi/classes/"+enr.Enrollment.Class.SourcedID, nil)
		get(t, "/eduapi/academicSessions/"+term, nil)
	})

	// Runs last: it deletes records. Enrollments whose person or offering is
	// deleted after they were stored must not be published with an empty
	// required relationship: Get answers 404 and List omits them.
	t.Run("orphaned enrollments", func(t *testing.T) {
		must := func(_ any, err error) {
			t.Helper()
			require.NoError(t, err)
		}
		must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-gone", SourcedID: "p-gone", GivenName: "A", FamilyName: "B", Status: domain.StatusActive}))
		must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-gone", SourcedID: "f-gone", CourseID: "c-internal"}))
		must(catalog.Enrollments.Create(ctx, &domain.Enrollment{ID: "e-person", SourcedID: "enr-person-gone", PersonID: "p-gone", OfferingID: "f-internal"}))
		must(catalog.Enrollments.Create(ctx, &domain.Enrollment{ID: "e-offering", SourcedID: "enr-offering-gone", PersonID: "p-internal", OfferingID: "f-gone"}))
		require.NoError(t, catalog.Persons.DeletePerson(ctx, "p-gone"))
		require.NoError(t, catalog.Offerings.Delete(ctx, "f-gone"))

		status := func(t *testing.T, path string) int {
			t.Helper()
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err, "GET %s", path)
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		for _, path := range []string{
			"/oeapi/associations/enr-person-gone",
			"/oeapi/associations/enr-offering-gone",
			"/eduapi/enrollments/enr-person-gone",
			"/eduapi/enrollments/enr-offering-gone",
		} {
			assert.Equal(t, http.StatusNotFound, status(t, path), "GET %s", path)
		}

		var oe struct {
			Items []struct{ AssociationID, PersonID, OfferingID string }
		}
		get(t, "/oeapi/associations", &oe)
		var edu struct {
			Enrollments []struct {
				SourcedID   string
				User, Class struct{ SourcedID string }
			}
		}
		get(t, "/eduapi/enrollments", &edu)
		if assert.Len(t, oe.Items, 1, "oeapi associations, want only enr-1") {
			assert.Equal(t, "enr-1", oe.Items[0].AssociationID)
		}
		if assert.Len(t, edu.Enrollments, 1, "eduapi enrollments, want only enr-1") {
			assert.Equal(t, "enr-1", edu.Enrollments[0].SourcedID)
		}
	})
}
