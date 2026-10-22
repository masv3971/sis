package helpers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		{"deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			gc, _ := gin.CreateTestContext(w)
			WriteError(gc, tc.err)

			require.Equal(t, tc.wantStatus, w.Code, "status")
			require.NotContains(t, w.Body.String(), "connection refused", "internal detail leaked to client")
			logged := len(gc.Errors) == 1 && errors.Is(gc.Errors[0].Err, tc.err)
			assert.Equal(t, tc.wantLogged, logged, "recorded on context (errors=%v)", gc.Errors)
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

			var bound payload
			err := BindJSON(gc, &bound)
			if tc.wantStatus == 0 {
				require.NoError(t, err, "BindJSON")
				assert.Equal(t, "a", bound.Name)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tc.wantStatus, NewErrorFromError(err).Status(), "status (err=%v)", err)
		})
	}
}
