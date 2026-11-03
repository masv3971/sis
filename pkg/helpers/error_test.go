package helpers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// slowDeadlineWriter is a response writer whose SetReadDeadline takes a
// while, so a BindJSON that returns before its cancellation callback ends is
// caught.
type slowDeadlineWriter struct {
	*httptest.ResponseRecorder
	done atomic.Bool
}

func (w *slowDeadlineWriter) SetReadDeadline(time.Time) error {
	time.Sleep(50 * time.Millisecond)
	w.done.Store(true)
	return nil
}

// TestBindJSONWaitsForCancelCallback checks BindJSON does not return while
// its read-deadline callback is still running: gin reuses the context and
// writer once the handler returns, so a late callback would race with the
// next request.
func TestBindJSONWaitsForCancelCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := &slowDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	gc, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	gc.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))

	var bound struct{ Name string }
	err := BindJSON(gc, &bound)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, w.done.Load(), "BindJSON returned before its callback finished")
}

// TestBindJSONClientCanceled checks a request whose client went away while
// the body was read is a classified 400, not an unclassified 500 that
// WriteError logs as an internal failure.
func TestBindJSONClientCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	gc.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))

	var bound struct{ Name string }
	err := BindJSON(gc, &bound)

	require.Error(t, err)
	apiErr := NewErrorFromError(err)
	assert.False(t, apiErr.IsUnclassified(), "unclassified (err=%v)", err)
	assert.Equal(t, http.StatusBadRequest, apiErr.Status())
}
