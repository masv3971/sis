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

// serveTimeouts serves router behind requestTimeout over a real listener
// (an httptest.ResponseRecorder does not enforce write deadlines).
// requestTimeout's write deadline replaces the server's for each request.
func serveTimeouts(t *testing.T, defaultTimeout time.Duration, perRoute map[string]time.Duration, grace time.Duration, routes map[string]gin.HandlerFunc) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	router := gin.New()
	router.Use(requestTimeout(defaultTimeout, perRoute, grace, log))
	for path, handler := range routes {
		router.GET(path, handler)
	}
	httpCfg := model.HTTPServer{Addr: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: 10 * time.Millisecond}
	s, err := start(t.Context(), httpCfg, router, log)
	require.NoError(t, err)
	// t.Context is already cancelled when Cleanup runs.
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return "http://" + s.Addr().String()
}

// get fetches url, returning the status and body, or the transport error.
func get(t *testing.T, url string) (int, string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}

// TestRequestTimeoutDelivers503 checks a handler stopped by the request
// deadline still delivers its 503 over a real connection, both on the
// default timeout and on a route with its own.
func TestRequestTimeoutDelivers503(t *testing.T) {
	waitThenFail := func(gc *gin.Context) {
		<-gc.Request.Context().Done()
		helpers.WriteError(gc, gc.Request.Context().Err())
	}
	base := serveTimeouts(t, 100*time.Millisecond, map[string]time.Duration{"/doc": 300 * time.Millisecond}, 2*time.Second,
		map[string]gin.HandlerFunc{"/default": waitThenFail, "/doc": waitThenFail})

	for path, timeout := range map[string]time.Duration{"/default": 100 * time.Millisecond, "/doc": 300 * time.Millisecond} {
		began := time.Now()
		status, body, err := get(t, base+path)
		require.NoError(t, err, "%s: the 503 must reach the client", path)
		assert.Equal(t, http.StatusServiceUnavailable, status, path)
		assert.Contains(t, body, helpers.ErrUnavailable, path)
		assert.GreaterOrEqual(t, time.Since(began), timeout, "%s: work ran until its own deadline", path)
	}
}

// TestRequestTimeoutBoundsWrite checks the write deadline: a route with its
// own timeout may reply after the default one, while a reply later than
// timeout + grace is cut off.
func TestRequestTimeoutBoundsWrite(t *testing.T) {
	ignoreCtx := func(gc *gin.Context) {
		time.Sleep(300 * time.Millisecond)
		gc.String(http.StatusOK, "done")
	}
	base := serveTimeouts(t, 50*time.Millisecond, map[string]time.Duration{"/doc": 5 * time.Second}, 50*time.Millisecond,
		map[string]gin.HandlerFunc{"/doc": ignoreCtx, "/other": ignoreCtx})

	status, body, err := get(t, base+"/doc")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "done", body)

	if _, _, err := get(t, base+"/other"); err == nil {
		t.Fatal("/other: want the connection cut at timeout + grace")
	}
}

// TestRequestTimeoutSetsDeadline checks every request context gets the
// default deadline.
func TestRequestTimeoutSetsDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	router := gin.New()
	router.Use(requestTimeout(time.Hour, nil, time.Second, log))
	router.GET("/fast", func(gc *gin.Context) {
		deadline, ok := gc.Request.Context().Deadline()
		assert.True(t, ok, "every request gets a deadline")
		assert.WithinDuration(t, time.Now().Add(time.Hour), deadline, time.Minute)
		gc.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
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
