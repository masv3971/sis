package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
)

// TestEduAPINestedRoutes checks /students, /staff and the routes nested under
// course offerings and academic sessions: filtering, sourcedId addressing,
// total count and 404 for an unknown or orphaned parent.
func TestEduAPINestedRoutes(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:          service.NewResource(memory.NewDocRepo[domain.Person](), nil, nil, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
	}
	must := func(_ any, err error) {
		t.Helper()
		require.NoError(t, err)
	}
	person := func(id string, roles ...domain.Role) {
		must(catalog.Persons.Create(ctx, &domain.Person{ID: "p-" + id, SourcedID: id, GivenName: id, FamilyName: id, Roles: roles, Status: domain.StatusActive}))
	}
	person("ann", domain.RoleStudent)
	person("bob", domain.RoleStudent)
	person("cat", domain.RoleFaculty)
	person("dan", domain.RoleStaff)

	must(catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{ID: "s-1", SourcedID: "ht26", Title: "HT26"}))
	must(catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{ID: "s-2", SourcedID: "vt27", Title: "VT27"}))
	must(catalog.Courses.Create(ctx, &domain.Course{ID: "c-1", SourcedID: "DA101", Title: "C"}))
	must(catalog.Courses.Create(ctx, &domain.Course{ID: "c-gone", SourcedID: "GONE", Title: "G"}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-1", SourcedID: "DA101-ht26", CourseID: "c-1", AcademicSessionID: "s-1"}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-2", SourcedID: "DA101-vt27", CourseID: "c-1", AcademicSessionID: "s-2"}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-orphan", SourcedID: "GONE-ht26", CourseID: "c-gone", AcademicSessionID: "s-1"}))
	enroll := func(id, personID, offeringID string, role domain.Role) {
		must(catalog.Enrollments.Create(ctx, &domain.Enrollment{ID: "e-" + id, SourcedID: id, PersonID: personID, OfferingID: offeringID, Role: role}))
	}
	enroll("e1", "p-ann", "f-1", domain.RoleStudent)
	enroll("e2", "p-bob", "f-1", domain.RoleStudent)
	enroll("e3", "p-cat", "f-1", domain.RoleFaculty)
	enroll("e4", "p-ann", "f-2", domain.RoleStudent)
	require.NoError(t, catalog.Courses.Delete(ctx, "c-gone"))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")

	type result struct {
		status int
		total  int
		ids    []string
	}
	get := func(path string) result {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+srv.Addr().String()+eduBase+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, path)
		defer resp.Body.Close()
		out := result{status: resp.StatusCode}
		if resp.StatusCode != http.StatusOK {
			return out
		}
		out.total, _ = strconv.Atoi(resp.Header.Get("X-Total-Count"))
		var body []struct{ SourcedID string }
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), path)
		for _, item := range body {
			out.ids = append(out.ids, item.SourcedID)
		}
		return out
	}

	tests := []struct {
		path       string
		wantStatus int
		wantTotal  int
		wantIDs    []string
	}{
		{"/students", 200, 2, []string{"ann", "bob"}},
		{"/staff", 200, 2, []string{"cat", "dan"}},
		{"/courseOfferings/DA101-ht26/enrollments", 200, 3, []string{"e1", "e2", "e3"}},
		{"/courseOfferings/DA101-ht26/students", 200, 2, []string{"ann", "bob"}},
		{"/courseOfferings/DA101-ht26/staff", 200, 1, []string{"cat"}},
		{"/courseOfferings/DA101-vt27/students?limit=1&offset=0", 200, 1, []string{"ann"}},
		{"/academicSessions/ht26/courseOfferings", 200, 2, []string{"DA101-ht26"}}, // the orphan is not published
		{"/academicSessions/ht26/enrollments", 200, 3, []string{"e1", "e2", "e3"}},
		{"/academicSessions/vt27/enrollments", 200, 1, []string{"e4"}},
		{"/courseOfferings/none/enrollments", 404, 0, nil},
		{"/courseOfferings/GONE-ht26/enrollments", 404, 0, nil},
		{"/academicSessions/none/courseOfferings", 404, 0, nil},
		{"/courseOfferings/DA101-ht26/students?sort=x", 400, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := get(tt.path)
			require.Equal(t, tt.wantStatus, got.status)
			if tt.wantStatus != http.StatusOK {
				return
			}
			assert.Equal(t, tt.wantIDs, got.ids)
			if tt.wantTotal > 0 {
				assert.Equal(t, tt.wantTotal, got.total, "X-Total-Count")
			}
		})
	}
}
