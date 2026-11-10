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
		Results:          service.NewResource(memory.NewDocRepo[domain.Result](), nil, nil, log),
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
	must(catalog.Results.Create(ctx, &domain.Result{ID: "r-internal", SourcedID: "res-1", PersonID: "p-internal", CourseID: "c-internal", OfferingID: "f-internal", Grade: "A"}))

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
		type nested struct{ ResultID, PersonID, CourseID, OfferingID, Score string }
		var assoc struct {
			PersonID, OfferingID string
			Result               *nested
		}
		get(t, "/oeapi/associations/enr-1", &assoc)
		var list struct {
			Items []struct {
				PersonID, OfferingID string
				Result               *nested
			}
		}
		get(t, "/oeapi/associations", &list)
		require.Len(t, list.Items, 1)
		require.NotNil(t, assoc.Result, "association must nest its result")
		require.NotNil(t, list.Items[0].Result, "listed association must nest its result")

		checks := []struct{ name, got, want string }{
			{"organization.parentOrganization", org.ParentOrganization, "uni"},
			{"course.organizationId", course.OrganizationID, "dept"},
			{"offering.courseId", off.CourseID, "DA101"},
			{"offering.academicSessionId", off.AcademicSessionID, "ht26"},
			{"association.personId", assoc.PersonID, "p-external"},
			{"association.offeringId", assoc.OfferingID, "DA101-ht26"},
			{"list association.personId", list.Items[0].PersonID, "p-external"},
			{"association.result.resultId", assoc.Result.ResultID, "res-1"},
			{"association.result.personId", assoc.Result.PersonID, "p-external"},
			{"association.result.courseId", assoc.Result.CourseID, "DA101"},
			{"association.result.offeringId", assoc.Result.OfferingID, "DA101-ht26"},
			{"association.result.score", assoc.Result.Score, "A"},
			{"list association.result.resultId", list.Items[0].Result.ResultID, "res-1"},
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
		var org struct{ Parent string }
		get(t, eduBase+"/organizations/dept", &org)
		var course struct{ Organization string }
		get(t, eduBase+"/courseTemplates/DA101", &course)
		var offering struct{ Course, AcademicSession, Organization, StartDate string }
		get(t, eduBase+"/courseOfferings/DA101-ht26", &offering)
		var enr struct{ Person, EducationOffering string }
		get(t, eduBase+"/enrollments/enr-1", &enr)

		checks := []struct{ name, got, want string }{
			{"organization.parent", org.Parent, "uni"},
			{"courseTemplate.organization", course.Organization, "dept"},
			{"courseOffering.course", offering.Course, "DA101"},
			{"courseOffering.academicSession", offering.AcademicSession, "ht26"},
			{"courseOffering.organization", offering.Organization, "dept"},
			{"enrollment.person", enr.Person, "p-external"},
			{"enrollment.educationOffering", enr.EducationOffering, "DA101-ht26"},
		}
		for _, check := range checks {
			assert.Equal(t, check.want, check.got, check.name)
		}
		get(t, eduBase+"/persons/"+enr.Person, nil)
		get(t, eduBase+"/courseOfferings/"+enr.EducationOffering, nil)
		get(t, eduBase+"/academicSessions/"+offering.AcademicSession, nil)
		get(t, eduBase+"/courseTemplates/"+offering.Course, nil)
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
			eduBase + "/enrollments/enr-person-gone",
			eduBase + "/enrollments/enr-offering-gone",
		} {
			assert.Equal(t, http.StatusNotFound, status(t, path), "GET %s", path)
		}

		var oe struct {
			Items []struct{ AssociationID, PersonID, OfferingID string }
		}
		get(t, "/oeapi/associations", &oe)
		var edu []struct{ SourcedID string }
		get(t, eduBase+"/enrollments", &edu)
		if assert.Len(t, oe.Items, 1, "oeapi associations, want only enr-1") {
			assert.Equal(t, "enr-1", oe.Items[0].AssociationID)
		}
		if assert.Len(t, edu, 1, "eduapi enrollments, want only enr-1") {
			assert.Equal(t, "enr-1", edu[0].SourcedID)
		}
	})
}
