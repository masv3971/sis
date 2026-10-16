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
