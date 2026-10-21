package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

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
	ctx := context.Background()
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
		if _, err := catalog.Courses.Create(ctx, &domain.Course{ID: "c" + id, SourcedID: "c" + id, Title: "T"}); err != nil {
			t.Fatal(err)
		}
		if _, err := catalog.Offerings.Create(ctx, &domain.Offering{ID: id, SourcedID: id, CourseID: "c" + id}); err != nil {
			t.Fatal(err)
		}
	}
	// Orphan the first offering: List leaves it out, X-Total-Count still counts it.
	if err := catalog.Courses.Delete(ctx, "co0"); err != nil {
		t.Fatal(err)
	}

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 2, 3
	s, iss := startWithIssuer(t, cfg, catalog)
	tok := iss.Token(t, "reader", "sis.read")

	seen := map[string]bool{}
	for offset, pages := 0, 0; ; pages++ {
		if pages > stored {
			t.Fatal("paging does not end")
		}
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/eduapi/classes?limit=500&offset=%d", s.Addr(), offset), nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Classes []struct{ SourcedID string } }
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("offset %d: status %d, %v", offset, resp.StatusCode, err)
		}
		limit, _ := strconv.Atoi(resp.Header.Get("X-Limit"))
		total, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
		if limit != 3 || total != stored {
			t.Fatalf("X-Limit %q, X-Total-Count %q; want 3, %d", resp.Header.Get("X-Limit"), resp.Header.Get("X-Total-Count"), stored)
		}
		for _, c := range body.Classes {
			seen[c.SourcedID] = true
		}
		if offset += limit; offset >= total {
			break
		}
	}
	if len(seen) != stored-1 || seen["o0"] {
		t.Fatalf("read %v; want o1..o%d", seen, stored-1)
	}
}
