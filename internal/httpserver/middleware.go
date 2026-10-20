package httpserver

import (
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
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"dur", time.Since(start).String(),
		}
		if len(c.Errors) == 0 {
			log.Info("http", attrs...)
			return
		}
		log.Error("http", append(attrs, "err", strings.Join(c.Errors.Errors(), "; "))...)
	}
}

// securityHeaders sets headers that are safe for every response: no MIME
// sniffing, no framing, no Referer (the SPA's URL carries the OIDC code after
// sign-in), and HSTS when served over TLS.
func securityHeaders(tls bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		if tls {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}

// bodyLimit caps request body size to guard against oversized payloads: n
// bytes, or perRoute[route] for a matched route pattern (gin's FullPath,
// e.g. "/api/v1/parse"). Global middleware runs after routing, so the
// pattern is known here.
func bodyLimit(n int64, perRoute map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := n
		if l, ok := perRoute[c.FullPath()]; ok {
			limit = l
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
