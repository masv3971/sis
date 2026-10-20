package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/authtest"
)

// TestRequireScope checks the status and error code for every outcome:
// people and machine clients are told apart by nothing but the scope.
func TestRequireScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	v := newVerifier(t, iss.Config())

	r := gin.New()
	r.GET("/write", v.RequireAuth(), auth.RequireScope(auth.ScopeWrite), func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := []struct {
		name   string
		auth   string
		status int
		code   string
	}{
		{"no token", "", http.StatusUnauthorized, "unauthorized"},
		{"garbage", "Bearer x.y.z", http.StatusUnauthorized, "unauthorized"},
		{"untrusted issuer", authtest.Bearer(authtest.NewIssuer(t).Token(t, "x", "sis.write")), http.StatusUnauthorized, "unauthorized"},
		{"read only", authtest.Bearer(iss.Token(t, "sunet-vc", "sis.read")), http.StatusForbidden, "forbidden"},
		{"write", authtest.Bearer(iss.Token(t, "liu", "sis.write")), http.StatusOK, ""},
		{"read and write", authtest.Bearer(iss.Token(t, "alice", "openid sis.read sis.write")), http.StatusOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/write", nil)
			if c.auth != "" {
				req.Header.Set("Authorization", c.auth)
			}
			r.ServeHTTP(w, req)
			var body struct{ Error string }
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != c.status || body.Error != c.code {
				t.Fatalf("got %d %q, want %d %q", w.Code, body.Error, c.status, c.code)
			}
		})
	}
}

// TestRequireAuthSchemeIsCaseInsensitive checks the Bearer scheme name is
// matched case-insensitively (RFC 9110 §11.1), while a different scheme or a
// missing token is still rejected.
func TestRequireAuthSchemeIsCaseInsensitive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	v := newVerifier(t, iss.Config())
	tok := iss.Token(t, "u", "sis.read")

	r := gin.New()
	r.GET("/x", v.RequireAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := map[string]int{
		"Bearer " + tok:     http.StatusOK,
		"bearer " + tok:     http.StatusOK,
		"BEARER " + tok:     http.StatusOK,
		"Bearer   " + tok:   http.StatusOK,
		"Basic " + tok:      http.StatusUnauthorized,
		"Bearer":            http.StatusUnauthorized,
		"Bearer ":           http.StatusUnauthorized,
		"Bearertoken" + tok: http.StatusUnauthorized,
	}
	for header, want := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", header)
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("Authorization %.20q: got %d, want %d", header, w.Code, want)
		}
	}
}
