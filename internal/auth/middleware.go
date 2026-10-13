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
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			abort(c, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := issuer.Parse(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
		if err != nil {
			abort(c, http.StatusUnauthorized, "invalid token")
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
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

func abort(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": "unauthorized", "message": msg})
}
