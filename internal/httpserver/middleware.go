package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/pkg/logger"
)

// requestLogger logs one line per request after it completes. Errors handlers
// attached to the context (helpers.WriteError does so for unclassified errors
// before redacting them from the response) are included on that line, which
// is then logged at error level, so a 500 always carries its cause.
func requestLogger(log *logger.Log) gin.HandlerFunc {
	return func(gc *gin.Context) {
		start := time.Now()
		gc.Next()
		attrs := []any{
			"method", gc.Request.Method,
			"path", gc.Request.URL.Path,
			"status", gc.Writer.Status(),
			"dur", time.Since(start).String(),
		}
		if len(gc.Errors) == 0 {
			log.Info("http", attrs...)
			return
		}
		log.Error("http", append(attrs, "err", strings.Join(gc.Errors.Errors(), "; "))...)
	}
}

// securityHeaders sets headers that are safe for every response: no MIME
// sniffing, no framing, no Referer (the SPA's URL carries the OIDC code after
// sign-in), and HSTS when served over TLS.
func securityHeaders(tls bool) gin.HandlerFunc {
	return func(gc *gin.Context) {
		header := gc.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Content-Security-Policy", "frame-ancestors 'none'")
		header.Set("Referrer-Policy", "no-referrer")
		if tls {
			header.Set("Strict-Transport-Security", "max-age=31536000")
		}
		gc.Next()
	}
}

// requestTimeout gives the request defaultTimeout, or perRoute[route] for a
// matched route pattern (gin's FullPath), counted from now. It bounds both
// the work and the reply:
//   - the request context gets that deadline, so database calls and other
//     ctx-aware work stop when it passes and the handler answers 503
//     (helpers.WriteError maps context.DeadlineExceeded);
//   - a per-route timeout also replaces the server's write timeout, so a
//     slow route is not cut off at the global deadline. The default needs
//     no extension: it equals the server's write timeout.
//
// Global middleware runs after routing, so the pattern is known here.
func requestTimeout(defaultTimeout time.Duration, perRoute map[string]time.Duration, log *logger.Log) gin.HandlerFunc {
	return func(gc *gin.Context) {
		timeout := defaultTimeout
		if routeTimeout, ok := perRoute[gc.FullPath()]; ok {
			timeout = routeTimeout
			if err := http.NewResponseController(gc.Writer).SetWriteDeadline(time.Now().Add(timeout)); err != nil {
				log.Warn("set write deadline", "path", gc.FullPath(), "err", err)
			}
		}
		ctx, cancel := context.WithTimeout(gc.Request.Context(), timeout)
		defer cancel()
		gc.Request = gc.Request.WithContext(ctx)
		gc.Next()
	}
}

// bodyLimit caps request body size to guard against oversized payloads:
// defaultLimit bytes, or perRoute[route] for a matched route pattern (gin's
// FullPath, e.g. "/api/v1/parse"). Global middleware runs after routing, so
// the pattern is known here.
func bodyLimit(defaultLimit int64, perRoute map[string]int64) gin.HandlerFunc {
	return func(gc *gin.Context) {
		limit := defaultLimit
		if routeLimit, ok := perRoute[gc.FullPath()]; ok {
			limit = routeLimit
		}
		gc.Request.Body = http.MaxBytesReader(gc.Writer, gc.Request.Body, limit)
		gc.Next()
	}
}
