package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/auth/token"
)

func TestAbortCodeFollowsStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	issuer := token.NewIssuer("secret", "sis", time.Hour)
	readOnly, _, _ := issuer.Issue("u", token.KindUser, []string{"readonly"}, nil)

	r := gin.New()
	r.GET("/x", RequireAuth(issuer), RequireScopeOrRole("sis.write", "admin"), func(c *gin.Context) { c.Status(200) })

	cases := []struct {
		auth   string
		status int
		code   string
	}{
		{"", http.StatusUnauthorized, "unauthorized"},
		{"Bearer " + readOnly, http.StatusForbidden, "forbidden"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/x", nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		r.ServeHTTP(w, req)
		var body struct{ Error string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != c.status || body.Error != c.code {
			t.Errorf("with token=%v: got %d %q, want %d %q", c.auth != "", w.Code, body.Error, c.status, c.code)
		}
	}
}
