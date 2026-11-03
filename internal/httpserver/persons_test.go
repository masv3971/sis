package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
)

// TestPersonCentricAPI covers what the admin UI's student page needs: find a
// student (q), list their enrollments and results, and delete them together
// with those records.
func TestPersonCentricAPI(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:     service.New(memory.NewPersonRepository(), nil, nil, log),
		Courses:     service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:   service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments: service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
		Programs:    service.NewResource(memory.NewDocRepo[domain.Program](), nil, nil, log),
		Results:     service.NewResource(memory.NewDocRepo[domain.Result](), nil, nil, log),
	}
	must := func(_ any, err error) {
		t.Helper()
		require.NoError(t, err)
	}
	must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "anna", SourcedID: "liu-1", GivenName: "Anna", FamilyName: "Andersson", Status: domain.StatusActive,
		Identifiers: []domain.Identifier{{Type: "studentNumber", Value: "S-0001"}}}))
	must(catalog.Persons.CreatePerson(ctx, domain.Person{ID: "johan", SourcedID: "liu-2", GivenName: "Johan", FamilyName: "Berg", Status: domain.StatusActive}))
	must(catalog.Courses.Create(ctx, &domain.Course{ID: "c", Title: "C"}))
	must(catalog.Offerings.Create(ctx, &domain.Offering{ID: "o", CourseID: "c"}))
	must(catalog.Enrollments.Create(ctx, &domain.Enrollment{Base: domain.Base{ID: "e-anna"}, PersonID: "anna", OfferingID: "o"}))
	must(catalog.Enrollments.Create(ctx, &domain.Enrollment{Base: domain.Base{ID: "e-johan"}, PersonID: "johan", OfferingID: "o"}))
	must(catalog.Results.Create(ctx, &domain.Result{Base: domain.Base{ID: "r-anna"}, PersonID: "anna", CourseID: "c", Grade: "A"}))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.RestAdmin.DefaultLimit, cfg.SIS.Adapter.Inbound.RestAdmin.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	reader := iss.Token(t, "reader", "sis.read")
	writer := iss.Token(t, "writer", "sis.write")
	base := "http://" + srv.Addr().String() + "/api/v1"

	call := func(t *testing.T, method, path, token string, out any) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, base+path, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%s %s", method, path)
		defer resp.Body.Close()
		if out != nil && resp.StatusCode == http.StatusOK {
			require.NoError(t, json.NewDecoder(resp.Body).Decode(out), "%s %s: decode", method, path)
		}
		return resp.StatusCode
	}
	search := func(t *testing.T, q string) []string {
		t.Helper()
		var page struct {
			Persons []domain.Person
			Total   int
		}
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/persons?q="+url.QueryEscape(q), reader, &page), "q=%q", q)
		ids := make([]string, 0, len(page.Persons))
		for _, p := range page.Persons {
			ids = append(ids, p.ID)
		}
		assert.Len(t, ids, page.Total, "q=%q: total", q)
		return ids
	}

	t.Run("search", func(t *testing.T) {
		assert.Equal(t, []string{"anna", "johan"}, search(t, ""), "no q lists everyone")
		assert.Equal(t, []string{"anna"}, search(t, "s-0001"), "student number")
		assert.Equal(t, []string{"johan"}, search(t, "  berg  "), "surrounding space")
		assert.Equal(t, []string{"anna"}, search(t, "anna liu-1"), "every term")
		assert.Empty(t, search(t, "anna berg"), "every term")

		assert.Equal(t, http.StatusBadRequest, call(t, http.MethodGet, "/persons?q=a+b+c+d+e+f", reader, nil), "too many terms")
		assert.Equal(t, http.StatusBadRequest, call(t, http.MethodGet, "/persons?q="+strings.Repeat("a", 201), reader, nil), "too long")
	})

	t.Run("records of a person", func(t *testing.T) {
		var enrollments struct {
			Items []domain.Enrollment
			Total int
		}
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/persons/anna/enrollments", reader, &enrollments))
		require.Len(t, enrollments.Items, 1)
		assert.Equal(t, "e-anna", enrollments.Items[0].ID)
		assert.Equal(t, 1, enrollments.Total)

		var results struct{ Items []domain.Result }
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/persons/anna/results", reader, &results))
		require.Len(t, results.Items, 1)
		assert.Equal(t, "A", results.Items[0].Grade)

		assert.Equal(t, http.StatusNotFound, call(t, http.MethodGet, "/persons/nobody/enrollments", reader, nil), "unknown person")
		assert.Equal(t, http.StatusNotFound, call(t, http.MethodGet, "/persons/liu-1/results", reader, nil), "a sourcedId is not an id")
		assert.Equal(t, http.StatusForbidden, call(t, http.MethodGet, "/persons/anna/results", writer, nil), "needs sis.read")
	})

	t.Run("delete cascades", func(t *testing.T) {
		require.Equal(t, http.StatusNoContent, call(t, http.MethodDelete, "/persons/anna", writer, nil))
		_, err := catalog.Enrollments.Get(ctx, "e-anna")
		require.ErrorIs(t, err, domain.ErrNotFound, "enrollment deleted with the person")
		_, err = catalog.Results.Get(ctx, "r-anna")
		require.ErrorIs(t, err, domain.ErrNotFound, "result deleted with the person")
		_, err = catalog.Enrollments.Get(ctx, "e-johan")
		require.NoError(t, err, "someone else's enrollment is kept")
		assert.Equal(t, http.StatusNotFound, call(t, http.MethodDelete, "/persons/anna", writer, nil), "already gone")
	})
}
