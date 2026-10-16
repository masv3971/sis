package restadmin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// newTestRouter mounts the person and course write handlers the way
// httpserver does, with the same strict JSON decoding.
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gin.EnableJsonDecoderDisallowUnknownFields()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:       service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations: service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		Courses:       service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
	}
	if _, err := catalog.Organizations.Create(context.Background(), &domain.Organization{Base: domain.Base{ID: "o-id", SourcedID: "uni"}, Name: "U"}); err != nil {
		t.Fatal(err)
	}
	c, err := New(context.Background(), &model.Cfg{}, catalog, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := gin.New()
	r.POST("/persons", c.CreatePerson)
	r.PUT("/persons/:id", c.UpdatePerson)
	r.POST("/courses", ResourceCreate(c, catalog.Courses))
	r.PUT("/courses/:id", ResourceUpdate(c, catalog.Courses))
	return r
}

// TestWriteHandlersRejectBadBodiesWith400 is the regression test for
// request-binding failures that the classifier did not recognise (strict
// unknown-field rejection, unparsable timestamps) being returned as 500.
func TestWriteHandlersRejectBadBodiesWith400(t *testing.T) {
	r := newTestRouter(t)
	bodies := map[string]string{
		"unknown field":     `{"givenName":"A","familyName":"B","status":"active","bogus":1}`,
		"invalid timestamp": `{"givenName":"A","familyName":"B","status":"active","createdAt":"yesterday"}`,
	}
	courseBodies := map[string]string{
		"unknown field":     `{"title":"T","bogus":1}`,
		"invalid timestamp": `{"title":"T","createdAt":"yesterday"}`,
	}
	routes := []struct {
		method, path string
		bodies       map[string]string
	}{
		{http.MethodPost, "/persons", bodies},
		{http.MethodPut, "/persons/x", bodies},
		{http.MethodPost, "/courses", courseBodies},
		{http.MethodPut, "/courses/x", courseBodies},
	}
	for _, rt := range routes {
		for name, body := range rt.bodies {
			t.Run(rt.method+" "+rt.path+" "+name, func(t *testing.T) {
				req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body)
				}
				if !strings.Contains(w.Body.String(), `"invalid_request"`) {
					t.Fatalf("expected invalid_request title, got %s", w.Body)
				}
			})
		}
	}
}

// TestResourceWritesRequireCanonicalRefs checks a reference must be the
// canonical id of an existing record: a sourcedId or unknown id is a 400
// naming the field, so stored references are never ambiguous.
func TestResourceWritesRequireCanonicalRefs(t *testing.T) {
	r := newTestRouter(t)
	cases := []struct {
		name, body string
		want       int
	}{
		{"canonical id", `{"title":"T","orgId":"o-id"}`, http.StatusCreated},
		{"sourcedId", `{"title":"T","orgId":"uni"}`, http.StatusBadRequest},
		{"unknown id", `{"title":"T","orgId":"nope"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/courses", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.want, w.Body)
			}
			if tc.want == http.StatusBadRequest && !strings.Contains(w.Body.String(), `"orgId"`) {
				t.Fatalf("expected error naming orgId, got %s", w.Body)
			}
		})
	}
}

// Service-layer errors must keep their own classification after binding
// succeeds: an update of a missing record is still 404, not 400.
func TestWriteHandlersKeepServiceErrorClassification(t *testing.T) {
	r := newTestRouter(t)
	req := httptest.NewRequest(http.MethodPut, "/persons/missing",
		strings.NewReader(`{"givenName":"A","familyName":"B","status":"active"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body)
	}
}

// TestWritesIgnoreExpiresAt checks expiresAt is server-managed: /api/v1
// writes cannot make a record expire (only POST /api/v1/populate with a ttl
// can), so a client cannot schedule the deletion of shared data.
func TestWritesIgnoreExpiresAt(t *testing.T) {
	r := newTestRouter(t)
	for _, path := range []string{"/persons", "/courses"} {
		body := `{"givenName":"A","familyName":"B","status":"active","expiresAt":"2030-01-01T00:00:00Z"}`
		if path == "/courses" {
			body = `{"title":"T","expiresAt":"2030-01-01T00:00:00Z"}`
		}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("%s: status = %d; body=%s", path, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "expiresAt") {
			t.Fatalf("%s: caller-supplied expiresAt was stored: %s", path, w.Body)
		}
	}
}
