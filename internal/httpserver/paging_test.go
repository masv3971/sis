package httpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
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

	huge := link("?offset=" + strconv.Itoa(math.MaxInt)) // offset+limit would overflow
	assert.Empty(t, linkRel(huge, "next"), "no next past the end")
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

// TestEduAPISortAndFilter checks sort, orderBy and filter on a list, and that
// a request the provider cannot honour is a 400 with the spec's error body,
// never unfiltered rows.
func TestEduAPISortAndFilter(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons: service.New(memory.NewPersonRepository(), nil, nil, log),
	}
	for _, p := range []domain.Person{
		{ID: "p1", SourcedID: "p1", GivenName: "Zed", FamilyName: "Berg", Status: domain.StatusActive},
		{ID: "p2", SourcedID: "p2", GivenName: "Amy", FamilyName: "Ahl", Status: domain.StatusActive},
		{ID: "p3", SourcedID: "p3", GivenName: "Bo", FamilyName: "Cole", Status: domain.StatusActive},
	} {
		_, err := catalog.Persons.CreatePerson(ctx, p)
		require.NoError(t, err)
	}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "reader", "sis.read")

	do := func(path string) (*http.Response, []string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+srv.Addr().String()+eduBase+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, path)
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			return resp, nil
		}
		var body []struct{ SourcedID string }
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), path)
		ids := make([]string, 0, len(body))
		for _, item := range body {
			ids = append(ids, item.SourcedID)
		}
		return resp, ids
	}
	q := url.QueryEscape

	t.Run("sort and orderBy", func(t *testing.T) {
		_, ids := do("/persons?sort=legalName.familyName")
		assert.Equal(t, []string{"p2", "p1", "p3"}, ids)
		_, ids = do("/persons?sort=legalName.givenName&orderBy=desc")
		assert.Equal(t, []string{"p1", "p3", "p2"}, ids)
	})
	t.Run("filter", func(t *testing.T) {
		resp, ids := do("/persons?filter=" + q("legalName.givenName='amy'"))
		assert.Equal(t, []string{"p2"}, ids, "equals ignores case")
		assert.Equal(t, "1", resp.Header.Get("X-Total-Count"))
		_, ids = do("/persons?sort=legalName.givenName&filter=" + q("legalName.familyName~'e' AND legalName.givenName!='Bo'"))
		assert.Equal(t, []string{"p1"}, ids)
		_, ids = do("/persons?sort=legalName.givenName&filter=" + q("legalName.givenName='Bo' OR legalName.givenName='Zed'"))
		assert.Equal(t, []string{"p3", "p1"}, ids)
	})
	t.Run("paging links keep the query", func(t *testing.T) {
		resp, ids := do("/persons?limit=1&sort=legalName.givenName&filter=" + q("legalName.familyName~'o'"))
		assert.Equal(t, []string{"p3"}, ids, "Bo Cole sorts first among Cole, and Berg has no o")
		assert.Equal(t, "1", resp.Header.Get("X-Total-Count"))
		resp, _ = do("/persons?limit=1&sort=legalName.givenName&filter=" + q("legalName.familyName~'e'"))
		next := linkRel(resp.Header.Get("Link"), "next")
		assert.Contains(t, next, "sort=legalName.givenName")
		assert.Contains(t, next, "filter=")
	})

	rejected := []struct {
		name, path, minor string
	}{
		{"unknown filter field", "/persons?filter=" + q("nope='a'"), "invalid_filter_field"},
		{"stored key is not a published name", "/persons?filter=" + q("givenName='a'"), "invalid_filter_field"},
		{"unknown sort field", "/persons?sort=nope", "invalid_query_parameter"},
		{"operator not supported", "/persons?filter=" + q("legalName.givenName>'a'"), "invalid_query_parameter"},
		{"unquoted value", "/persons?filter=" + q("legalName.givenName=a"), "invalid_query_parameter"},
		{"orderBy without sort", "/persons?orderBy=desc", "invalid_query_parameter"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			resp, _ := do(tt.path)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var body struct {
				CodeMajor string `json:"imsx_codeMajor"`
				Severity  string `json:"imsx_severity"`
				Minor     struct {
					Fields []struct {
						Value string `json:"imsx_codeMinorFieldValue"`
					} `json:"imsx_codeMinorField"`
				} `json:"imsx_codeMinor"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, "failure", body.CodeMajor)
			assert.Equal(t, "error", body.Severity)
			require.NotEmpty(t, body.Minor.Fields)
			assert.Equal(t, tt.minor, body.Minor.Fields[0].Value)
		})
	}
}
