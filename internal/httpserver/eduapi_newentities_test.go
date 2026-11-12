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

// TestEduAPINewEntities reads affiliations, component templates, collection
// and component offerings, and the type-neutral /educationTemplates and
// /educationOfferings, through the Edu-API.
func TestEduAPINewEntities(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:             service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations:       service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		AcademicSessions:    service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:             service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:           service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Programs:            service.NewResource(memory.NewDocRepo[domain.Program](), nil, nil, log),
		Affiliations:        service.NewResource(memory.NewDocRepo[domain.Affiliation](), nil, nil, log),
		CollectionOfferings: service.NewResource(memory.NewDocRepo[domain.CollectionOffering](), nil, nil, log),
		ComponentTemplates:  service.NewResource(memory.NewDocRepo[domain.ComponentTemplate](), nil, nil, log),
		ComponentOfferings:  service.NewResource(memory.NewDocRepo[domain.ComponentOffering](), nil, nil, log),
	}
	must := func(_ any, err error) {
		t.Helper()
		require.NoError(t, err)
	}
	must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "p-1", SourcedID: "ann", GivenName: "A", FamilyName: "A", Status: domain.StatusActive}))
	must(catalog.Organizations.Create(ctx, &domain.Organization{ID: "o-1", SourcedID: "uni", Name: "U"}))
	must(catalog.AcademicSessions.Create(ctx, &domain.AcademicSession{ID: "s-1", SourcedID: "ht26", Title: "HT26", StartDate: "2026-08-31", EndDate: "2027-01-15"}))
	must(catalog.Programs.Create(ctx, &domain.Program{ID: "g-1", SourcedID: "BSc", Title: "BSc Biology"}))
	must(catalog.Courses.Create(ctx, &domain.Course{ID: "c-1", SourcedID: "DA101", Title: "C", ProgramIDs: []string{"g-1"}}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-1", SourcedID: "DA101-ht26", CourseID: "c-1", AcademicSessionID: "s-1", CollectionOfferingIDs: []string{"k-1"}}))
	must(catalog.CollectionOfferings.Create(ctx, &domain.CollectionOffering{ID: "k-1", SourcedID: "BSc-ht26", ProgramID: "g-1", AcademicSessionID: "s-1"}))
	// Its program does not exist: an orphan, which is not published.
	must(catalog.CollectionOfferings.Create(ctx, &domain.CollectionOffering{ID: "k-orphan", SourcedID: "ORPHAN-ht26", ProgramID: "g-gone", AcademicSessionID: "s-1"}))
	must(catalog.ComponentTemplates.Create(ctx, &domain.ComponentTemplate{ID: "t-1", SourcedID: "DA101-lab", Title: "Lab", Type: "laboratory", CourseIDs: []string{"c-1"}}))
	must(catalog.ComponentOfferings.Create(ctx, &domain.ComponentOffering{ID: "l-1", SourcedID: "DA101-lab-ht26", ComponentID: "t-1", AcademicSessionID: "s-1", CourseOfferingIDs: []string{"f-1"}}))
	must(catalog.Affiliations.Create(ctx, &domain.Affiliation{ID: "a-1", SourcedID: "ann-uni", PersonID: "p-1", OrgID: "o-1", Role: domain.RoleStudent}))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")

	get := func(path string) (status, total int, body []map[string]any, single map[string]any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+srv.Addr().String()+eduBase+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, path)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err, path)
		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, 0, nil, nil
		}
		total, _ = strconv.Atoi(resp.Header.Get("X-Total-Count"))
		if len(raw) > 0 && raw[0] == '[' {
			require.NoError(t, json.Unmarshal(raw, &body), path)
		} else {
			require.NoError(t, json.Unmarshal(raw, &single), path)
		}
		return resp.StatusCode, total, body, single
	}
	ids := func(body []map[string]any) []any {
		out := make([]any, 0, len(body))
		for _, item := range body {
			out = append(out, item["sourcedId"])
		}
		return out
	}

	t.Run("affiliation", func(t *testing.T) {
		status, total, list, _ := get("/affiliations")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, 1, total)
		assert.Equal(t, []any{"ann-uni"}, ids(list))

		_, _, _, one := get("/affiliations/ann-uni")
		assert.Equal(t, "ann", one["person"], "references are sourcedIds")
		assert.Equal(t, "uni", one["organization"])
		assert.Equal(t, "student", one["role"])
		assert.Equal(t, "active", one["affiliationStatus"])

		status, _, _, _ = get("/affiliations/none")
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("component template", func(t *testing.T) {
		_, _, _, one := get("/componentTemplates/DA101-lab")
		assert.Equal(t, "laboratory", one["componentType"])
		assert.Equal(t, []any{"DA101"}, one["parent"], "parent holds the course's sourcedId")
	})

	t.Run("collection offering", func(t *testing.T) {
		status, total, list, _ := get("/collectionOfferings")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, []any{"BSc-ht26"}, ids(list), "the orphan is not published")
		assert.Equal(t, 2, total, "X-Total-Count counts stored records")

		_, _, _, one := get("/collectionOfferings/BSc-ht26")
		assert.Equal(t, "BSc", one["collection"])
		assert.Equal(t, "ht26", one["academicSession"])
		assert.Equal(t, "2026-08-31T00:00:00Z", one["startDate"], "dates come from the session")
		title := one["title"].([]any)[0].(map[string]any)
		assert.Equal(t, "BSc Biology", title["value"], "an untitled offering takes its program's title")

		status, _, _, _ = get("/collectionOfferings/ORPHAN-ht26")
		assert.Equal(t, http.StatusNotFound, status)

		_, _, list, _ = get("/academicSessions/ht26/collectionOfferings")
		assert.Equal(t, []any{"BSc-ht26"}, ids(list))
		status, _, _, _ = get("/academicSessions/none/collectionOfferings")
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("component offering", func(t *testing.T) {
		_, _, _, one := get("/componentOfferings/DA101-lab-ht26")
		assert.Equal(t, "DA101-lab", one["component"])
		assert.Equal(t, "laboratory", one["offeringType"])
		assert.Equal(t, []any{"DA101-ht26"}, one["parent"])

		_, _, list, _ := get("/academicSessions/ht26/componentOfferings")
		assert.Equal(t, []any{"DA101-lab-ht26"}, ids(list))
	})

	t.Run("parent references", func(t *testing.T) {
		_, _, _, one := get("/courseOfferings/DA101-ht26")
		assert.Equal(t, []any{"BSc-ht26"}, one["parent"])
		_, _, _, course := get("/courseTemplates/DA101")
		assert.Equal(t, []any{"BSc"}, course["parent"])
	})

	t.Run("education templates", func(t *testing.T) {
		status, total, list, _ := get("/educationTemplates")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, 3, total)
		assert.Equal(t, []any{"BSc", "DA101", "DA101-lab"}, ids(list), "ordered by sourcedId")
		for _, item := range list {
			assert.NotContains(t, item, "courseType")
			assert.NotContains(t, item, "parent")
		}

		_, _, page, _ := get("/educationTemplates?limit=1&offset=1")
		assert.Equal(t, []any{"DA101"}, ids(page))

		for _, id := range []string{"DA101", "BSc", "DA101-lab"} {
			_, _, _, one := get("/educationTemplates/" + id)
			assert.Equal(t, id, one["sourcedId"])
		}
		status, _, _, _ = get("/educationTemplates/none")
		assert.Equal(t, http.StatusNotFound, status)
		status, _, _, _ = get("/educationTemplates?sort=sourcedId")
		assert.Equal(t, http.StatusBadRequest, status, "the union cannot sort")
	})

	t.Run("education offerings", func(t *testing.T) {
		status, total, list, _ := get("/educationOfferings")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, 3, total)
		assert.Equal(t, []any{"BSc-ht26", "DA101-ht26", "DA101-lab-ht26"}, ids(list))
		kinds := map[any]any{}
		for _, item := range list {
			kinds[item["sourcedId"]] = item["offeringType"]
			assert.NotContains(t, item, "parent")
		}
		assert.Equal(t, map[any]any{"BSc-ht26": "collection", "DA101-ht26": "course", "DA101-lab-ht26": "component"}, kinds)

		for _, id := range []string{"DA101-ht26", "BSc-ht26", "DA101-lab-ht26"} {
			_, _, _, one := get("/educationOfferings/" + id)
			assert.Equal(t, id, one["sourcedId"])
		}
		status, _, _, _ = get("/educationOfferings/ORPHAN-ht26")
		assert.Equal(t, http.StatusNotFound, status)
	})

	// Last, because it adds records the counts above do not expect.
	t.Run("parent skips a collection offering whose program is gone", func(t *testing.T) {
		must(catalog.Programs.Create(ctx, &domain.Program{ID: "g-2", SourcedID: "MSc", Title: "MSc"}))
		must(catalog.CollectionOfferings.Create(ctx, &domain.CollectionOffering{ID: "k-2", SourcedID: "MSc-ht26", ProgramID: "g-2"}))
		must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "f-2", SourcedID: "DA102-ht26", CourseID: "c-1", CollectionOfferingIDs: []string{"k-1", "k-2"}}))

		_, _, _, one := get("/courseOfferings/DA102-ht26")
		assert.Equal(t, []any{"BSc-ht26", "MSc-ht26"}, one["parent"])

		require.NoError(t, catalog.Programs.Delete(ctx, "g-2"))

		status, _, _, _ := get("/collectionOfferings/MSc-ht26")
		require.Equal(t, http.StatusNotFound, status, "the offering is an orphan now")
		_, _, _, one = get("/courseOfferings/DA102-ht26")
		assert.Equal(t, []any{"BSc-ht26"}, one["parent"], "parent must not list an offering that answers 404")
	})
}
