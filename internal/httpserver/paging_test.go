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

// TestEduAPIPagingByXLimit pages /eduapi the way the developer scripts do:
// ask for more than max_limit, then advance offset by X-Limit until it
// reaches X-Total-Count. Every publishable record must be read even though
// the requested limit is clamped and an orphan makes a page short.
func TestEduAPIPagingByXLimit(t *testing.T) {
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
	for offset, pages := 0, 0; ; pages++ {
		require.LessOrEqual(t, pages, stored, "paging does not end")
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/eduapi/classes?limit=500&offset=%d", srv.Addr(), offset), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		var body struct{ Classes []struct{ SourcedID string } }
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		require.NoError(t, err, "offset %d", offset)
		require.Equal(t, http.StatusOK, resp.StatusCode, "offset %d", offset)
		limit, _ := strconv.Atoi(resp.Header.Get("X-Limit"))
		total, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
		require.Equal(t, 3, limit, "X-Limit")
		require.Equal(t, stored, total, "X-Total-Count")
		for _, class := range body.Classes {
			seen[class.SourcedID] = true
		}
		if offset += limit; offset >= total {
			break
		}
	}
	assert.Len(t, seen, stored-1, "read %v; want o1..o%d", seen, stored-1)
	assert.False(t, seen["o0"], "orphan o0 must not be listed")
}
