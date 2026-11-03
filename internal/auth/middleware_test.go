package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/authtest"
)

// TestRequireScope checks the status and error code for every outcome:
// people and machine clients are told apart by nothing but the scope.
func TestRequireScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	verifier := newVerifier(t, iss.Config())

	router := gin.New()
	router.GET("/write", verifier.RequireAuth(), auth.RequireScope(auth.ScopeWrite), func(gc *gin.Context) { gc.Status(http.StatusOK) })

	cases := []struct {
		name      string
		auth      string
		status    int
		code      string
		challenge string
	}{
		{"no token", "", http.StatusUnauthorized, "unauthorized", "Bearer"},
		{"garbage", "Bearer x.y.z", http.StatusUnauthorized, "unauthorized", `Bearer error="invalid_token"`},
		{"untrusted issuer", authtest.Bearer(authtest.NewIssuer(t).Token(t, "x", "sis.write")), http.StatusUnauthorized, "unauthorized", `Bearer error="invalid_token"`},
		{"read only", authtest.Bearer(iss.Token(t, "sunet-vc", "sis.read")), http.StatusForbidden, "forbidden", `Bearer error="insufficient_scope", scope="sis.write"`},
		{"write", authtest.Bearer(iss.Token(t, "liu", "sis.write")), http.StatusOK, "", ""},
		{"read and write", authtest.Bearer(iss.Token(t, "alice", "openid sis.read sis.write")), http.StatusOK, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/write", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			router.ServeHTTP(w, req)
			var body struct{ Error string }
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, body.Error)
			assert.Equal(t, tc.challenge, w.Header().Get("WWW-Authenticate"))
		})
	}
}

// TestRequireAuthIssuerDown checks an unreachable issuer is a 503, so clients
// retry instead of discarding a good token.
func TestRequireAuthIssuerDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	verifier := newVerifier(t, iss.Config())
	iss.Down.Store(true)

	router := gin.New()
	router.GET("/x", verifier.RequireAuth(), func(gc *gin.Context) { gc.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", authtest.Bearer(iss.Token(t, "x", "sis.read")))
	router.ServeHTTP(w, req)
	var body struct{ Error string }
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "unavailable", body.Error)
}

// TestRequireAuthSchemeIsCaseInsensitive checks the Bearer scheme name is
// matched case-insensitively (RFC 9110 §11.1), while a different scheme or a
// missing token is still rejected.
func TestRequireAuthSchemeIsCaseInsensitive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	verifier := newVerifier(t, iss.Config())
	token := iss.Token(t, "u", "sis.read")

	router := gin.New()
	router.GET("/x", verifier.RequireAuth(), func(gc *gin.Context) { gc.Status(http.StatusOK) })

	cases := map[string]int{
		"Bearer " + token:     http.StatusOK,
		"bearer " + token:     http.StatusOK,
		"BEARER " + token:     http.StatusOK,
		"Bearer   " + token:   http.StatusOK,
		"Basic " + token:      http.StatusUnauthorized,
		"Bearer":              http.StatusUnauthorized,
		"Bearer ":             http.StatusUnauthorized,
		"Bearertoken" + token: http.StatusUnauthorized,
	}
	for header, want := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", header)
		router.ServeHTTP(w, req)
		assert.Equal(t, want, w.Code, "Authorization %.20q", header)
	}
}
