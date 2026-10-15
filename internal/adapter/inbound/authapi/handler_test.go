package authapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func TestWriteTokenForbidsCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(w)
	writeToken(gc, auth.Token{AccessToken: "t", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})

	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := w.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
}

func newTestClient(t *testing.T, production bool, slots int) *Client {
	t.Helper()
	cfg := &model.Cfg{Common: model.Common{Production: production}}
	cfg.SIS.Auth.JWTSecret = "0123456789abcdef0123456789abcdef"
	cfg.SIS.Auth.TokenTTL = time.Hour
	cfg.SIS.Adapter.Inbound.AuthAPI.MaxConcurrentLogins = slots
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	a, err := auth.New(context.Background(), cfg, memory.NewAccountRepository(), memory.NewClientRepository(), log)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	c, err := New(context.Background(), cfg, a, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func serve(h gin.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(req.Method, req.URL.Path, h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func loginReq() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"u","password":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func tokenReq() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader("grant_type=client_credentials&client_id=c&client_secret=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestLoginRejectsWhenSlotsBusy checks that both credential endpoints answer
// 429 without running a password check once every slot is taken.
func TestLoginRejectsWhenSlotsBusy(t *testing.T) {
	c := newTestClient(t, false, 1)
	c.verifySlots <- struct{}{} // occupy the only slot

	for name, tc := range map[string]struct {
		h   gin.HandlerFunc
		req *http.Request
	}{
		"login": {c.Login, loginReq()},
		"token": {c.Token, tokenReq()},
	} {
		w := serve(tc.h, tc.req)
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
			t.Errorf("%s: got %d (Retry-After %q), want 429 with Retry-After", name, w.Code, w.Header().Get("Retry-After"))
		}
	}
}

// TestLoginReleasesSlot checks that a finished check frees its slot, so a
// single-slot client keeps answering 401 rather than 429.
func TestLoginReleasesSlot(t *testing.T) {
	c := newTestClient(t, false, 1)
	for i := range 3 {
		if w := serve(c.Login, loginReq()); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401", i, w.Code)
		}
		if w := serve(c.Token, tokenReq()); w.Code != http.StatusUnauthorized {
			t.Fatalf("token attempt %d: got %d, want 401", i, w.Code)
		}
	}
}

func TestOIDCCookiesSecureInProduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, production := range []bool{true, false} {
		c := newTestClient(t, production, 1)
		for _, maxAge := range []int{oidcCookieTTL, -1} {
			w := httptest.NewRecorder()
			gc, _ := gin.CreateTestContext(w)
			c.setOIDCCookies(gc, "s", "n", maxAge)
			cookies := w.Result().Cookies()
			if len(cookies) != 2 {
				t.Fatalf("expected 2 cookies, got %d", len(cookies))
			}
			for _, ck := range cookies {
				if ck.Secure != production || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode {
					t.Errorf("production=%v maxAge=%d %s: Secure=%v HttpOnly=%v SameSite=%v",
						production, maxAge, ck.Name, ck.Secure, ck.HttpOnly, ck.SameSite)
				}
			}
		}
	}
}
