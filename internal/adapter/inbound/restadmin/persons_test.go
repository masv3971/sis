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
		Persons: service.New(memory.NewPersonRepository(), nil, nil, log),
		Courses: service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
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
