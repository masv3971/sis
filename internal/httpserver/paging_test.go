package httpserver

import (
	"encoding/json"
	"fmt"
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

// TestEduAPIPaging pages the Edu-API course offerings: advance offset by
// limit until it reaches X-Total-Count. Every publishable record must be
// read although an orphan makes a page short, and a limit above max_limit
// is clamped.
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
	const limit = 3 // the configured max_limit
	for offset, pages := 0, 0; ; pages++ {
		require.LessOrEqual(t, pages, stored, "paging does not end")
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s%s/courseOfferings?limit=%d&offset=%d", srv.Addr(), eduBase, limit, offset), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		var body []struct{ SourcedID string }
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		require.NoError(t, err, "offset %d: a list is a bare array", offset)
		require.Equal(t, http.StatusOK, resp.StatusCode, "offset %d", offset)
		require.Empty(t, resp.Header.Get("X-Limit"), "not part of Edu-API")
		total, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
		require.Equal(t, stored, total, "X-Total-Count")
		for _, offering := range body {
			seen[offering.SourcedID] = true
		}
		if offset += limit; offset >= total {
			break
		}
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s%s/courseOfferings?limit=500", srv.Addr(), eduBase), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var clamped []struct{ SourcedID string }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&clamped))
	_ = resp.Body.Close()
	assert.LessOrEqual(t, len(clamped), limit, "limit above max_limit is clamped")
	assert.Len(t, seen, stored-1, "read %v; want o1..o%d", seen, stored-1)
	assert.False(t, seen["o0"], "orphan o0 must not be listed")
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
