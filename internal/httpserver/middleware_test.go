package httpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

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

	r := gin.New()
	r.Use(requestLogger(log))
	r.GET("/boom", func(gc *gin.Context) { helpers.WriteError(gc, errors.New("pg: connection refused")) })
	r.GET("/ok", func(gc *gin.Context) { gc.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "connection refused") {
		t.Fatalf("response = %d %s; want redacted 500", w.Code, w.Body)
	}
	out := buf.String()
	if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "pg: connection refused") {
		t.Fatalf("log line missing error level or cause: %s", out)
	}

	buf.Reset()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	if out := buf.String(); !strings.Contains(out, "level=INFO") || strings.Contains(out, "err=") {
		t.Fatalf("successful request should log at info without err: %s", out)
	}
}

// TestWriteDeadline checks a route with writeDeadline outlives the server's
// write timeout, while a route without it is still cut off.
func TestWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	slow := func(gc *gin.Context) {
		time.Sleep(300 * time.Millisecond)
		gc.String(http.StatusOK, "done")
	}
	r := gin.New()
	r.GET("/doc", writeDeadline(5*time.Second, log), slow)
	r.GET("/other", slow)
	hc := model.HTTPServer{Addr: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: 100 * time.Millisecond}
	s, err := start(t.Context(), hc, r, log)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	base := "http://" + s.Addr().String()

	resp, err := http.Get(base + "/doc")
	if err != nil {
		t.Fatalf("/doc: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "done" {
		t.Fatalf("/doc = %d %q", resp.StatusCode, body)
	}

	if resp, err := http.Get(base + "/other"); err == nil {
		resp.Body.Close()
		t.Fatalf("/other: want the connection cut at the server write timeout, got %d", resp.StatusCode)
	}
}

// TestSecurityHeaders checks every response, including a 404, carries the
// security headers, and HSTS only over TLS.
func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tls := range []bool{false, true} {
		r := gin.New()
		r.Use(securityHeaders(tls))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
		h := w.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" ||
			h.Get("Content-Security-Policy") != "frame-ancestors 'none'" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("tls=%v: headers %v", tls, h)
		}
		if got := h.Get("Strict-Transport-Security") != ""; got != tls {
			t.Fatalf("tls=%v: HSTS present = %v", tls, got)
		}
	}
}
