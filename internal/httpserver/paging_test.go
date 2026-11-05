package httpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

// TestEduAPIPaging pages the Edu-API course offerings by following
// rel="next", asking for more than max_limit. Every publishable record must
// be read although the limit is clamped and an orphan makes a page short.
func TestEduAPIPaging(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:     service.New(memory.NewPersonRepository(), nil, nil, log),
		Courses:     service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:   service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments: service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
	}
	const stored = 7
	for i := range stored {
		id := fmt.Sprintf("o%d", i)
		_, err := catalog.Courses.Create(ctx, &domain.Course{ID: "c" + id, SourcedID: "c" + id, Title: "T"})
		require.NoError(t, err)
		_, err = catalog.Offerings.Create(ctx, &domain.Offering{ID: id, SourcedID: id, CourseID: "c" + id})
		require.NoError(t, err)
	}
	// Orphan the first offering: List leaves it out, X-Total-Count still counts it.
	require.NoError(t, catalog.Courses.Delete(ctx, "co0"))

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 2, 3
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")

	seen := map[string]bool{}
	next := eduBase + "/courseOfferings?limit=500" // above max_limit, which is 3
	for pages := 0; next != ""; pages++ {
		require.LessOrEqual(t, pages, stored, "paging does not end")
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+srv.Addr().String()+next, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		var body []struct{ SourcedID string }
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		require.NoError(t, err, "%s: a list is a bare array", next)
		require.Equal(t, http.StatusOK, resp.StatusCode, next)
		require.Empty(t, resp.Header.Get("X-Limit"), "not part of Edu-API")
		total, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
		require.Equal(t, stored, total, "X-Total-Count")
		assert.LessOrEqual(t, len(body), 3, "limit above max_limit is clamped")
		for _, offering := range body {
			seen[offering.SourcedID] = true
		}
		next = linkRel(resp.Header.Get("Link"), "next")
	}
	assert.Len(t, seen, stored-1, "read %v; want o1..o%d", seen, stored-1)
	assert.False(t, seen["o0"], "orphan o0 must not be listed")
}

// TestEduAPILinks checks first/prev/next/last on the first, a middle and the
// last page, and that the applied (clamped) limit is in every link.
func TestEduAPILinks(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons: service.New(memory.NewPersonRepository(), nil, nil, log),
	}
	for i := range 7 {
		_, err := catalog.Persons.CreatePerson(t.Context(), domain.Person{
			ID: fmt.Sprintf("p%d", i), SourcedID: fmt.Sprintf("p%d", i), GivenName: "A", FamilyName: "B", Status: domain.StatusActive,
		})
		require.NoError(t, err)
	}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 2, 3
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")

	link := func(query string) string {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+srv.Addr().String()+eduBase+"/persons"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, query)
		return resp.Header.Get("Link")
	}
	p := eduBase + "/persons"

	first := link("?limit=500")
	assert.Equal(t, p+"?limit=3&offset=0", linkRel(first, "first"))
	assert.Equal(t, p+"?limit=3&offset=3", linkRel(first, "next"))
	assert.Equal(t, p+"?limit=3&offset=6", linkRel(first, "last"))
	assert.Empty(t, linkRel(first, "prev"), "no prev on the first page")

	middle := link("?limit=500&offset=3")
	assert.Equal(t, p+"?limit=3&offset=0", linkRel(middle, "prev"))
	assert.Equal(t, p+"?limit=3&offset=6", linkRel(middle, "next"))

	last := link("?offset=6") // no limit: the default, 2
	assert.Equal(t, p+"?limit=2&offset=4", linkRel(last, "prev"))
	assert.Empty(t, linkRel(last, "next"), "no next on the last page")
	assert.Equal(t, p+"?limit=2&offset=6", linkRel(last, "last"))
}

// linkRel returns the target of the Link header entry with the given rel, or
// "" when there is none. It handles the form pageHeaders writes:
// <target>; rel="name", ...
func linkRel(header, rel string) string {
	for _, entry := range strings.Split(header, ", ") {
		target, params, ok := strings.Cut(entry, ">; ")
		if ok && params == fmt.Sprintf("rel=%q", rel) {
			return strings.TrimPrefix(target, "<")
		}
	}
	return ""
}

// TestEduAPIRejectsSortAndFilter checks the unsupported optional query
// parameters answer 400 with the spec's error body, not unfiltered rows.
func TestEduAPIRejectsSortAndFilter(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons: service.New(memory.NewPersonRepository(), nil, nil, log),
	}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("http://%s%s/persons?filter=givenName%%3D%%27A%%27", srv.Addr(), eduBase), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body struct {
		CodeMajor string `json:"imsx_codeMajor"`
		Severity  string `json:"imsx_severity"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "failure", body.CodeMajor)
	assert.Equal(t, "error", body.Severity)
}
