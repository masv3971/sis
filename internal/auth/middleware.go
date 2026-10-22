package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/pkg/helpers"
)

const claimsKey = "authClaims"

// RequireAuth verifies the Bearer token and stores its claims in the context.
func (v *Verifier) RequireAuth() gin.HandlerFunc {
	return func(gc *gin.Context) {
		raw, ok := BearerToken(gc.GetHeader("Authorization"))
		if !ok {
			challenge(gc, "")
			abort(gc, http.StatusUnauthorized, helpers.MsgMissingBearer)
			return
		}
		claims, err := v.Verify(gc.Request.Context(), raw)
		if errors.Is(err, ErrIssuerUnavailable) {
			abort(gc, http.StatusServiceUnavailable, helpers.MsgIssuerUnavailable)
			return
		}
		if err != nil {
			v.log.Debug("token rejected", "err", err)
			challenge(gc, `error="invalid_token"`)
			abort(gc, http.StatusUnauthorized, helpers.MsgInvalidToken)
			return
		}
		gc.Set(claimsKey, claims)
		gc.Next()
	}
}

// BearerToken extracts the credentials from an Authorization header using the
// Bearer scheme. The scheme name is matched case-insensitively (RFC 9110
// §11.1), so "bearer <token>" and "BEARER <token>" are accepted too. It
// reports false when the scheme is not Bearer or no token follows it.
func BearerToken(header string) (string, bool) {
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
	return func(gc *gin.Context) {
		claims := ClaimsFrom(gc)
		if claims == nil {
			challenge(gc, "")
			abort(gc, http.StatusUnauthorized, helpers.MsgUnauthenticated)
			return
		}
		if !claims.HasScope(scope) {
			challenge(gc, `error="insufficient_scope", scope="`+scope+`"`)
			abort(gc, http.StatusForbidden, helpers.MsgLacksScope(scope))
			return
		}
		gc.Next()
	}
}

// challenge sets the RFC 6750 §3 WWW-Authenticate header, with optional
// auth-params.
func challenge(gc *gin.Context, params string) {
	value := "Bearer"
	if params != "" {
		value += " " + params
	}
	gc.Header("WWW-Authenticate", value)
}

// ClaimsFrom returns the verified claims, or nil if unauthenticated.
func ClaimsFrom(gc *gin.Context) *Claims {
	stored, ok := gc.Get(claimsKey)
	if !ok {
		return nil
	}
	claims, _ := stored.(*Claims)
	return claims
}

// ErrorResponse is the JSON body the auth middleware writes when it rejects a
// request.
type ErrorResponse struct {
	// Error is "unauthorized" (401: missing, invalid or expired token, or
	// one from an untrusted issuer), "forbidden" (403: the token lacks the
	// required scope) or "unavailable" (503: the token's issuer could not be
	// reached to check it; retry later).
	Error   string `json:"error" example:"unauthorized"`
	Message string `json:"message" example:"missing bearer token"`
}

// abort ends the request with a JSON error whose code follows the status, so
// clients can tell a failed authentication (401 unauthorized) from missing
// permissions (403 forbidden) and an issuer outage (503 unavailable).
func abort(gc *gin.Context, status int, msg string) {
	code := helpers.ErrUnauthorized
	switch status {
	case http.StatusForbidden:
		code = helpers.ErrForbidden
	case http.StatusServiceUnavailable:
		code = helpers.ErrUnavailable
	}
	gc.AbortWithStatusJSON(status, ErrorResponse{Error: code, Message: msg})
}
