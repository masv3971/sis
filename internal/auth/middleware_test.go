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
			if got := w.Header().Get("WWW-Authenticate"); got != c.challenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, c.challenge)
			}
		})
	}
}

// TestRequireAuthIssuerDown checks an unreachable issuer is a 503, so clients
// retry instead of discarding a good token.
func TestRequireAuthIssuerDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	v := newVerifier(t, iss.Config())
	iss.Down.Store(true)

	r := gin.New()
	r.GET("/x", v.RequireAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", authtest.Bearer(iss.Token(t, "x", "sis.read")))
	r.ServeHTTP(w, req)
	var body struct{ Error string }
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusServiceUnavailable || body.Error != "unavailable" {
		t.Fatalf("got %d %q, want 503 \"unavailable\"", w.Code, body.Error)
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
