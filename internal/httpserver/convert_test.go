package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
)

// skewClock is a clock offset from real time by a settable amount, so a
// test can jump past a ttl while the handler stamps expiries in real time.
type skewClock struct{ skew atomic.Int64 }

func (clock *skewClock) now() time.Time { return time.Now().Add(time.Duration(clock.skew.Load())) }

func memCatalog(clock func() time.Time, log *logger.Log) *readmodel.Client {
	return &readmodel.Client{
		Persons:          service.New(memory.NewPersonRepository().WithClock(clock), nil, clock, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization]().WithClock(clock), nil, clock, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession]().WithClock(clock), nil, clock, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course]().WithClock(clock), nil, clock, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering]().WithClock(clock), nil, clock, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment]().WithClock(clock), nil, clock, log),
		Programs:         service.NewResource(memory.NewDocRepo[domain.Program]().WithClock(clock), nil, clock, log),
		Results:          service.NewResource(memory.NewDocRepo[domain.Result]().WithClock(clock), nil, clock, log),
	}
}

// TestParseAndPopulate drives the document endpoints end to end: parse
// returns the conversion without storing, populate stores it so the
// providers serve it, a ttl hides it once passed, and the guards, format
// checks and body cap apply.
func TestParseAndPopulate(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	clock := &skewClock{}
	catalog := memCatalog(clock.now, log)

	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	cfg.SIS.Adapter.Inbound.ConvertAPI.MaxBodyBytes = 64 << 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	reader, writer := iss.Token(t, "svc", "sis.read"), iss.Token(t, "svc", "sis.read sis.write")
	base := "http://" + srv.Addr().String()

	do := func(t *testing.T, token, method, path, body string, want int) map[string]any {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequestWithContext(ctx, method, base+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%s %s", method, path)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		require.Equal(t, want, resp.StatusCode, "%s %s: body=%s", method, path, raw)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}

	elm, err := os.ReadFile("../convert/testdata/edc_bsc.json")
	require.NoError(t, err)
	envelope := func(meta, extra string) string {
		return `{"metadata":` + meta + extra + `,"data":` + string(elm) + `}`
	}
	const course = "/eduapi/courses/urn:epass:learningAchievementSpec:bi101"

	t.Run("parse", func(t *testing.T) {
		for _, format := range []string{"eduapi", "oeapi"} {
			out := do(t, reader, http.MethodPost, "/api/v1/parse",
				envelope(`{"input_format":"elm","output_format":"`+format+`"}`, ""), http.StatusOK)
			data, _ := out["data"].(map[string]any)
			courses, _ := data["courses"].([]any)
			require.Len(t, courses, 1, "%s: courses", format)
			meta, _ := out["metadata"].(map[string]any)
			require.Equal(t, format, meta["output_format"], "metadata not echoed: %v", out["metadata"])
		}
		do(t, reader, http.MethodGet, course, "", http.StatusNotFound) // nothing stored
	})

	t.Run("bad requests", func(t *testing.T) {
		do(t, reader, http.MethodPost, "/api/v1/parse", envelope(`{"input_format":"xml","output_format":"eduapi"}`, ""), http.StatusBadRequest)
		do(t, reader, http.MethodPost, "/api/v1/parse", envelope(`{"input_format":"elm","output_format":"csv"}`, ""), http.StatusBadRequest)
		do(t, reader, http.MethodPost, "/api/v1/parse", `{"metadata":{"input_format":"elm","output_format":"eduapi"},"data":{"type":"x"}}`, http.StatusBadRequest)
		do(t, reader, http.MethodPost, "/api/v1/parse", `{"metadata":{"input_format":"elm","output_format":"eduapi"},"data":{},"extra":1}`, http.StatusBadRequest)
		do(t, writer, http.MethodPost, "/api/v1/populate", envelope(`{"input_format":"elm"}`, `,"ttl":"-1h"`), http.StatusBadRequest)
		do(t, writer, http.MethodPost, "/api/v1/populate", envelope(`{"input_format":"elm"}`, `,"ttl":"soon"`), http.StatusBadRequest)
		do(t, writer, http.MethodPost, "/api/v1/populate", envelope(`{"input_format":"elm","output_format":"eduapi"}`, ""), http.StatusBadRequest)
		big := `{"metadata":{"input_format":"elm","output_format":"eduapi"},"data":{"x":"` + strings.Repeat("a", 64<<10) + `"}}`
		do(t, reader, http.MethodPost, "/api/v1/parse", big, http.StatusRequestEntityTooLarge)
	})

	t.Run("populate needs write", func(t *testing.T) {
		do(t, reader, http.MethodPost, "/api/v1/populate", envelope(`{"input_format":"elm"}`, ""), http.StatusForbidden)
	})

	t.Run("populate with ttl", func(t *testing.T) {
		out := do(t, writer, http.MethodPost, "/api/v1/populate", envelope(`{"input_format":"elm"}`, `,"ttl":"1h"`), http.StatusOK)
		items, _ := out["items"].([]any)
		require.Len(t, items, 10, "populate reply = %v", out)
		require.NotNil(t, out["expiresAt"], "populate reply = %v", out)
		got := do(t, reader, http.MethodGet, course, "", http.StatusOK)
		gotCourse, _ := got["course"].(map[string]any)
		require.Equal(t, "BI101", gotCourse["courseCode"], "course = %v", got)
		res := do(t, reader, http.MethodGet, "/oeapi/results/urn:epass:learningAchievement:bi101", "", http.StatusOK)
		assert.Equal(t, "A", res["score"], "oeapi result = %v", res)
		assert.Equal(t, "urn:epass:learningAchievementSpec:bi101", res["courseId"], "oeapi result = %v", res)

		clock.skew.Store(int64(2 * time.Hour))
		do(t, reader, http.MethodGet, course, "", http.StatusNotFound)
		do(t, reader, http.MethodGet, "/eduapi/persons/urn:epass:person:liu-student-4711", "", http.StatusNotFound)
		clock.skew.Store(0)
	})
}
