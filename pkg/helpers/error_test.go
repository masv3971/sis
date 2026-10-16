package helpers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/domain"
)

// TestWriteErrorRecordsRedactedCause checks an unclassified error is redacted
// from the response but kept on the gin context for server-side logging,
// while classified errors (safe detail already in the body) are not recorded.
func TestWriteErrorRecordsRedactedCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantLogged bool
	}{
		{"unclassified", errors.New("pg: connection refused"), http.StatusInternalServerError, true},
		{"not found", domain.ErrNotFound, http.StatusNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			gc, _ := gin.CreateTestContext(w)
			WriteError(gc, tc.err)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if strings.Contains(w.Body.String(), "connection refused") {
				t.Fatalf("internal detail leaked to client: %s", w.Body)
			}
			logged := len(gc.Errors) == 1 && errors.Is(gc.Errors[0].Err, tc.err)
			if logged != tc.wantLogged {
				t.Fatalf("recorded on context = %v, want %v (errors=%v)", logged, tc.wantLogged, gc.Errors)
			}
		})
	}
}

// TestBindJSONRequiresSingleValue checks the body must be exactly one JSON
// value: trailing garbage or a second object is a 400, and bytes past the
// body limit are a 413 even when the leading object is complete.
func TestBindJSONRequiresSingleValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type payload struct {
		Name string `json:"name" binding:"required"`
	}
	const limit = 64
	cases := []struct {
		name       string
		body       string
		wantStatus int // 0 means success
	}{
		{"single object", `{"name":"a"}`, 0},
		{"surrounding whitespace", " \n{\"name\":\"a\"}\n\t ", 0},
		{"trailing garbage", `{"name":"a"}garbage`, http.StatusBadRequest},
		{"second object", `{"name":"a"}{"name":"b"}`, http.StatusBadRequest},
		{"trailing scalar", `{"name":"a"} 1`, http.StatusBadRequest},
		{"validation still runs", `{}`, http.StatusBadRequest},
		{"empty", ``, http.StatusBadRequest},
		{"whitespace past limit", `{"name":"a"}` + strings.Repeat(" ", limit), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			gc, _ := gin.CreateTestContext(w)
			gc.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			gc.Request.Body = http.MaxBytesReader(w, gc.Request.Body, limit)

			var p payload
			err := BindJSON(gc, &p)
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("BindJSON: %v", err)
				}
				if p.Name != "a" {
					t.Fatalf("name = %q, want a", p.Name)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := NewErrorFromError(err).Status(); got != tc.wantStatus {
				t.Fatalf("status = %d, want %d (err=%v)", got, tc.wantStatus, err)
			}
		})
	}
}
