package httpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestRequestLoggerRecordsRedactedCause checks the cause of a redacted 500
// reaches the service log at error level, while the client sees only the
// generic title.
func TestRequestLoggerRecordsRedactedCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	log := logger.NewWithWriter("test", false, "info", &buf)

	router := gin.New()
	router.Use(requestLogger(log))
	router.GET("/boom", func(gc *gin.Context) { helpers.WriteError(gc, errors.New("pg: connection refused")) })
	router.GET("/ok", func(gc *gin.Context) { gc.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "connection refused", "internal detail must be redacted")
	out := buf.String()
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, "pg: connection refused")

	buf.Reset()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	out = buf.String()
	assert.Contains(t, out, "level=INFO")
	assert.NotContains(t, out, "err=")
}

// TestRequestTimeoutExtendsWriteDeadline checks a route with its own timeout
// outlives the server's write timeout, while a route without one is still
// cut off.
func TestRequestTimeoutExtendsWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	slow := func(gc *gin.Context) {
		time.Sleep(300 * time.Millisecond)
		gc.String(http.StatusOK, "done")
	}
	router := gin.New()
	// The default (ctx) timeout is generous so only the write deadline
	// matters for /other.
	router.Use(requestTimeout(5*time.Second, map[string]time.Duration{"/doc": 5 * time.Second}, log))
	router.GET("/doc", slow)
	router.GET("/other", slow)
	httpCfg := model.HTTPServer{Addr: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: 100 * time.Millisecond}
	s, err := start(t.Context(), httpCfg, router, log)
	require.NoError(t, err)
	// t.Context is already cancelled when Cleanup runs.
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	base := "http://" + s.Addr().String()

	resp, err := http.Get(base + "/doc")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "done", string(body))

	if resp, err := http.Get(base + "/other"); err == nil {
		resp.Body.Close()
		t.Fatalf("/other: want the connection cut at the server write timeout, got %d", resp.StatusCode)
	}
}

// TestRequestTimeoutCancelsWork checks the handler's context ends at the
// route's timeout and the client gets 503 rather than a hung request.
func TestRequestTimeoutCancelsWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	router := gin.New()
	router.Use(requestTimeout(time.Hour, map[string]time.Duration{"/slow": 50 * time.Millisecond}, log))
	router.GET("/slow", func(gc *gin.Context) {
		<-gc.Request.Context().Done()
		helpers.WriteError(gc, gc.Request.Context().Err())
	})
	router.GET("/fast", func(gc *gin.Context) {
		deadline, ok := gc.Request.Context().Deadline()
		assert.True(t, ok, "every request gets a deadline")
		assert.WithinDuration(t, time.Now().Add(time.Hour), deadline, time.Minute)
		gc.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	began := time.Now()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))
	assert.Less(t, time.Since(began), 5*time.Second)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), helpers.ErrUnavailable)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fast", nil))
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// TestSecurityHeaders checks every response, including a 404, carries the
// security headers, and HSTS only over TLS.
func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tls := range []bool{false, true} {
		router := gin.New()
		router.Use(securityHeaders(tls))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
		header := w.Header()
		assert.Equal(t, "nosniff", header.Get("X-Content-Type-Options"), "tls=%v", tls)
		assert.Equal(t, "DENY", header.Get("X-Frame-Options"), "tls=%v", tls)
		assert.Equal(t, "frame-ancestors 'none'", header.Get("Content-Security-Policy"), "tls=%v", tls)
		assert.Equal(t, "no-referrer", header.Get("Referrer-Policy"), "tls=%v", tls)
		assert.Equal(t, tls, header.Get("Strict-Transport-Security") != "", "tls=%v: HSTS present", tls)
	}
}
