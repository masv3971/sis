package auth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/auth/token"
)

const claimsKey = "authClaims"

// RequireAuth validates the Bearer token and stores its claims in the context.
func RequireAuth(issuer *token.Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abort(c, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := issuer.Parse(raw)
		if err != nil {
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

// RequireScopeOrRole allows the request when the principal holds the given
// scope (machine clients) or any of the given roles (human logins).
func RequireScopeOrRole(scope string, roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			abort(c, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if slices.Contains(claims.Scopes, scope) {
			c.Next()
			return
		}
		for _, r := range roles {
			if slices.Contains(claims.Roles, r) {
				c.Next()
				return
			}
		}
		abort(c, http.StatusForbidden, "insufficient permissions")
	}
}

// ClaimsFrom returns the authenticated claims, or nil if unauthenticated.
func ClaimsFrom(c *gin.Context) *token.Claims {
	v, ok := c.Get(claimsKey)
	if !ok {
		return nil
	}
	claims, _ := v.(*token.Claims)
	return claims
}

// ErrorResponse is the JSON body the auth middleware writes when it rejects a
// request.
type ErrorResponse struct {
	// Error is "unauthorized" (401: missing or invalid token) or "forbidden"
	// (403: the token lacks the required scope or role).
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
