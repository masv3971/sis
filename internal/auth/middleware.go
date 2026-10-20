package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const claimsKey = "authClaims"

// RequireAuth verifies the Bearer token and stores its claims in the context.
func (v *Verifier) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abort(c, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := v.Verify(c.Request.Context(), raw)
		if err != nil {
			v.log.Debug("token rejected", "err", err)
			abort(c, http.StatusUnauthorized, "invalid token")
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
}

// bearerToken extracts the credentials from an Authorization header using the
// Bearer scheme. The scheme name is matched case-insensitively (RFC 9110
// §11.1), so "bearer <token>" and "BEARER <token>" are accepted too. It
// reports false when the scheme is not Bearer or no token follows it.
func bearerToken(header string) (string, bool) {
	scheme, credentials, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	credentials = strings.TrimSpace(credentials)
	return credentials, credentials != ""
}

// RequireScope allows the request when the verified token grants scope. It
// must run after RequireAuth. People and machine clients are treated the
// same: only the scope counts.
func RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			abort(c, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if !claims.HasScope(scope) {
			abort(c, http.StatusForbidden, "token lacks scope "+scope)
			return
		}
		c.Next()
	}
}

// ClaimsFrom returns the verified claims, or nil if unauthenticated.
func ClaimsFrom(c *gin.Context) *Claims {
	v, ok := c.Get(claimsKey)
	if !ok {
		return nil
	}
	claims, _ := v.(*Claims)
	return claims
}

// ErrorResponse is the JSON body the auth middleware writes when it rejects a
// request.
type ErrorResponse struct {
	// Error is "unauthorized" (401: missing, invalid or expired token, or
	// one from an untrusted issuer) or "forbidden" (403: the token lacks the
	// required scope).
	Error   string `json:"error" example:"unauthorized"`
	Message string `json:"message" example:"missing bearer token"`
}

// abort ends the request with a JSON error whose code follows the status, so
// clients can tell a failed authentication (401 unauthorized) from missing
// permissions (403 forbidden).
func abort(c *gin.Context, status int, msg string) {
	code := "unauthorized"
	if status == http.StatusForbidden {
		code = "forbidden"
	}
	c.AbortWithStatusJSON(status, ErrorResponse{Error: code, Message: msg})
}
