package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/token"
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
	ctx := context.Background()
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
		if err != nil {
			t.Fatal(err)
		}
	}
	must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-internal", SourcedID: "p-external", GivenName: "A", FamilyName: "B", Status: domain.StatusActive}))
	must(catalog.Organizations.Create(ctx, &domain.Organization{Base: domain.Base{ID: "o-root", SourcedID: "uni"}, Name: "U"}))
	must(catalog.Organizations.Create(ctx, &domain.Organization{Base: domain.Base{ID: "o-dept", SourcedID: "dept"}, Name: "D", ParentID: "o-root"}))
	must(catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{Base: domain.Base{ID: "s-internal", SourcedID: "ht26"}, Title: "HT26"}))
	must(catalog.Courses.Create(ctx, &domain.Course{Base: domain.Base{ID: "c-internal", SourcedID: "DA101"}, Title: "C", OrgID: "o-dept"}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: "f-internal", SourcedID: "DA101-ht26"}, CourseID: "c-internal", AcademicSessionID: "s-internal", OrgID: "o-dept"}))
	must(catalog.Enrollments.Create(ctx, &domain.Enrollment{Base: domain.Base{ID: "e-internal", SourcedID: "enr-1"}, PersonID: "p-internal", OfferingID: "f-internal", Role: domain.RoleStudent}))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Auth.TokenTTL = time.Hour
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	a, err := auth.New(ctx, cfg, memory.NewAccountRepository(), memory.NewClientRepository(), log)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	s, err := New(ctx, cfg, a, catalog, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tok, _, err := a.Issuer().Issue("reader", token.KindClient, nil, []string{domain.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + s.Addr().String()

	get := func(t *testing.T, path string, out any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d", path, resp.StatusCode)
		}
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("GET %s: decode: %v", path, err)
			}
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
		if len(list.Items) != 1 {
			t.Fatalf("expected 1 association, got %d", len(list.Items))
		}

		checks := []struct{ name, got, want string }{
			{"organization.parentOrganization", org.ParentOrganization, "uni"},
			{"course.organizationId", course.OrganizationID, "dept"},
			{"offering.courseId", off.CourseID, "DA101"},
			{"offering.academicSessionId", off.AcademicSessionID, "ht26"},
			{"association.personId", assoc.PersonID, "p-external"},
			{"association.offeringId", assoc.OfferingID, "DA101-ht26"},
			{"list association.personId", list.Items[0].PersonID, "p-external"},
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
			}
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

		sid := func(r *ref) string {
			if r == nil {
				return ""
			}
			return r.SourcedID
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
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
			}
		}
		get(t, "/eduapi/persons/"+enr.Enrollment.User.SourcedID, nil)
		get(t, "/eduapi/classes/"+enr.Enrollment.Class.SourcedID, nil)
		get(t, "/eduapi/academicSessions/"+term, nil)
	})

	// Runs last: it deletes records. Enrollments whose person or offering is
	// deleted after they were stored must not be published with an empty
	// required relationship: Get answers 404 and List omits them.
	t.Run("orphaned enrollments", func(t *testing.T) {
		must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-gone", SourcedID: "p-gone", GivenName: "A", FamilyName: "B", Status: domain.StatusActive}))
		must(catalog.Offerings.Create(ctx, &domain.Offering{Base: domain.Base{ID: "f-gone", SourcedID: "f-gone"}, CourseID: "c-internal"}))
		must(catalog.Enrollments.Create(ctx, &domain.Enrollment{Base: domain.Base{ID: "e-person", SourcedID: "enr-person-gone"}, PersonID: "p-gone", OfferingID: "f-internal"}))
		must(catalog.Enrollments.Create(ctx, &domain.Enrollment{Base: domain.Base{ID: "e-offering", SourcedID: "enr-offering-gone"}, PersonID: "p-internal", OfferingID: "f-gone"}))
		if err := catalog.Persons.DeletePerson(ctx, "p-gone"); err != nil {
			t.Fatal(err)
		}
		if err := catalog.Offerings.Delete(ctx, "f-gone"); err != nil {
			t.Fatal(err)
		}

		status := func(t *testing.T, path string) int {
			t.Helper()
			req, _ := http.NewRequest(http.MethodGet, base+path, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		for _, path := range []string{
			"/oeapi/associations/enr-person-gone",
			"/oeapi/associations/enr-offering-gone",
			"/eduapi/enrollments/enr-person-gone",
			"/eduapi/enrollments/enr-offering-gone",
		} {
			if got := status(t, path); got != http.StatusNotFound {
				t.Errorf("GET %s: status %d, want 404", path, got)
			}
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
		if len(oe.Items) != 1 || oe.Items[0].AssociationID != "enr-1" {
			t.Errorf("oeapi associations = %+v, want only enr-1", oe.Items)
		}
		if len(edu.Enrollments) != 1 || edu.Enrollments[0].SourcedID != "enr-1" {
			t.Errorf("eduapi enrollments = %+v, want only enr-1", edu.Enrollments)
		}
	})
}
