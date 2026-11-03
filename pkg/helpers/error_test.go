package helpers

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// slowDeadlineWriter records when the read-deadline callback finishes.
type slowDeadlineWriter struct {
	gin.ResponseWriter
	finished atomic.Bool
}

func (w *slowDeadlineWriter) SetReadDeadline(time.Time) error {
	time.Sleep(50 * time.Millisecond)
	w.finished.Store(true)
	return nil
}

// cancelOnRead cancels the request context as the body reaches EOF.
type cancelOnRead struct{ cancel context.CancelFunc }

func (r cancelOnRead) Read([]byte) (int, error) {
	r.cancel()
	return 0, io.EOF
}

// TestBindJSONWaitsForDeadlineCallback checks BindJSON does not return while
// its cancellation callback still uses gc, which gin reuses afterwards.
func TestBindJSONWaitsForDeadlineCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	writer := &slowDeadlineWriter{ResponseWriter: gc.Writer}
	gc.Writer = writer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	gc.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/", io.NopCloser(cancelOnRead{cancel}))

	err := BindJSON(gc, &struct{}{})
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, writer.finished.Load(), "BindJSON returned before the callback finished")
}

// cancelOnDecode cancels the request context while it is being decoded.
type cancelOnDecode struct{ cancel context.CancelFunc }

func (p *cancelOnDecode) UnmarshalJSON([]byte) error {
	p.cancel()
	return nil
}

// TestBindJSONRejectsContextEndedDuringDecode checks a context that ends
// while decoding yields its error, not decoded input.
func TestBindJSONRejectsContextEndedDuringDecode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	gc.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(`{}`))

	err := BindJSON(gc, &cancelOnDecode{cancel: cancel})
	require.ErrorIs(t, err, context.Canceled)
}
