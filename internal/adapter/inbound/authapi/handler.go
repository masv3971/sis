// Package authapi is the inbound HTTP adapter for authentication: local login,
// the OAuth2 client-credentials token endpoint, and OIDC RP login. The entry
// points are methods on Client; route declarations live in the httpserver
// package.
package authapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

const oidcCookieTTL = 300 // seconds

// The OIDC callback hands the access token to the SPA through a short-lived
// HttpOnly cookie instead of the URL. Only the callback sets it, and only after
// the state and nonce checks pass against cookies set by /auth/oidc/login in
// the same browser, so the handoff is bound to the browser that started the
// login. The SPA collects it once with POST /auth/oidc/session, which clears
// it. The cookie is scoped to that path so no other request carries it.
const (
	oidcLoginCookie     = "oidc_login"
	oidcLoginCookiePath = "/auth/oidc/session"
	oidcLoginCookieTTL  = 60 // seconds
	// oidcLoginMarker is appended to the post-login URL so the SPA knows a
	// handoff is waiting. It carries no secret: a forged link only makes
	// the SPA ask for a cookie the browser does not have.
	oidcLoginMarker = "#oidc_login"
)

// Client bundles the auth handlers with the dependencies they need (the auth
// Client, the post-login redirect URL, and a logger for operational-error
// paths). It is a "Client" rather than a "Service" because it owns no runtime
// resources — just configuration and injected deps — and has nothing to shut
// down.
//
// OIDC capability is checked on the auth Client directly (OIDCEnabled); the
// httpserver only mounts the OIDC routes when that returns true, so handlers
// here can assume OIDC is enabled when they are called.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	auth         *auth.Client
	postLoginURL string
	// secureCookies marks the OIDC state/nonce cookies Secure. On in
	// production; off in development so login works over plain http.
	secureCookies bool
	// verifySlots bounds concurrent password checks (Argon2, ~64 MiB each)
	// across Login and Token. A request that finds no free slot gets 429
	// instead of queueing, so a flood cannot exhaust memory.
	verifySlots chan struct{}
	log         *logger.Log
}

// New builds the authapi Client.
func New(_ context.Context, cfg *model.Cfg, authClient *auth.Client, log *logger.Log) (*Client, error) {
	ac := cfg.SIS.Adapter.Inbound.AuthAPI
	return &Client{
		auth:          authClient,
		postLoginURL:  ac.PostLoginRedirectURL,
		secureCookies: cfg.Common.Production,
		verifySlots:   make(chan struct{}, max(ac.MaxConcurrentLogins, 1)),
		log:           log.New("authapi"),
	}, nil
}

// acquireVerify takes a password-check slot, or writes 429 and returns false
// when all are busy. The caller must call release when acquireVerify
// returns true.
func (c *Client) acquireVerify(gc *gin.Context) bool {
	select {
	case c.verifySlots <- struct{}{}:
		return true
	default:
		gc.Header("Retry-After", "1")
		gc.JSON(http.StatusTooManyRequests, gin.H{"error": "too_many_requests"})
		return false
	}
}

func (c *Client) releaseVerify() { <-c.verifySlots }

// Config reports which login methods are available, so the web UI only
// offers SSO when the OIDC routes are actually mounted. Public: it reveals
// nothing beyond what probing /auth/oidc/login would.
func (c *Client) Config(gc *gin.Context) {
	gc.JSON(http.StatusOK, gin.H{"oidc": c.auth.OIDCEnabled()})
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles local username/password login, returning a signed token.
func (c *Client) Login(gc *gin.Context) {
	var req loginRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		gc.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if !c.acquireVerify(gc) {
		return
	}
	defer c.releaseVerify()
	tok, err := c.auth.Authenticate(gc.Request.Context(), req.Username, req.Password)
	if err != nil {
		c.writeAuthError(gc, err)
		return
	}
	writeToken(gc, tok)
}

// Token implements the OAuth2 client-credentials grant (RFC 6749 §4.4).
// Credentials come from form fields or HTTP Basic auth.
func (c *Client) Token(gc *gin.Context) {
	if gc.PostForm("grant_type") != "client_credentials" {
		gc.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}
	clientID, secret := gc.PostForm("client_id"), gc.PostForm("client_secret")
	if clientID == "" {
		if id, sec, ok := gc.Request.BasicAuth(); ok {
			clientID, secret = id, sec
		}
	}
	if !c.acquireVerify(gc) {
		return
	}
	defer c.releaseVerify()
	tok, err := c.auth.ClientCredentials(gc.Request.Context(), clientID, secret)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			gc.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
			return
		}
		c.log.Error("client-credentials token issue failed", "err", err)
		gc.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	writeToken(gc, tok)
}

// OIDCLogin starts the OIDC RP flow by redirecting to the provider.
func (c *Client) OIDCLogin(gc *gin.Context) {
	state, nonce := randToken(), randToken()
	c.setOIDCCookies(gc, state, nonce, oidcCookieTTL)
	gc.Redirect(http.StatusFound, c.auth.OIDCAuthCodeURL(state, nonce))
}

// OIDCCallback completes the OIDC RP flow and hands a token to the SPA via a
// browser-bound HttpOnly cookie (see oidcLoginCookie).
func (c *Client) OIDCCallback(gc *gin.Context) {
	state, err := gc.Cookie("oidc_state")
	if err != nil || state == "" || gc.Query("state") != state {
		gc.JSON(http.StatusBadRequest, gin.H{"error": "invalid_state"})
		return
	}
	nonce, _ := gc.Cookie("oidc_nonce")

	claims, err := c.auth.OIDCExchange(gc.Request.Context(), gc.Query("code"), nonce)
	if err != nil {
		// Expected on bad state/code/nonce mismatch but also on provider
		// outages — log once so operators can tell them apart.
		c.log.Warn("oidc exchange failed", "err", err)
		gc.JSON(http.StatusUnauthorized, gin.H{"error": "oidc_failed"})
		return
	}
	tok, err := c.auth.IssueForOIDC(gc.Request.Context(), claims)
	if err != nil {
		c.writeAuthError(gc, err)
		return
	}

	c.handOff(gc, tok)
}

// handOff clears the one-shot state/nonce cookies, parks the token in the
// login cookie, and sends the browser to the post-login URL with only the
// marker. The token never appears in a URL, so a crafted link cannot plant
// another user's token in this browser (login CSRF).
func (c *Client) handOff(gc *gin.Context, tok auth.Token) {
	noStore(gc)
	c.setOIDCCookies(gc, "", "", -1)
	c.setLoginCookie(gc, tok.AccessToken, oidcLoginCookieTTL)
	gc.Redirect(http.StatusFound, c.postLoginURL+oidcLoginMarker)
}

// OIDCSession returns the token parked by OIDCCallback and clears the cookie,
// so each handoff is collected once. POST only: SameSite=Lax withholds the
// cookie from cross-site POSTs, and CORS keeps other origins from reading
// the response. Without a valid pending handoff it answers 401 and the SPA
// keeps whatever session it already had.
func (c *Client) OIDCSession(gc *gin.Context) {
	noStore(gc)
	raw, err := gc.Cookie(oidcLoginCookie)
	c.setLoginCookie(gc, "", -1)
	if err != nil || raw == "" {
		gc.JSON(http.StatusUnauthorized, gin.H{"error": "no_pending_login"})
		return
	}
	claims, err := c.auth.Issuer().Parse(raw)
	if err != nil || claims.ExpiresAt == nil {
		gc.JSON(http.StatusUnauthorized, gin.H{"error": "no_pending_login"})
		return
	}
	writeToken(gc, auth.Token{AccessToken: raw, TokenType: "Bearer", ExpiresAt: claims.ExpiresAt.Time})
}

// setLoginCookie sets (or, with maxAge -1, clears) the token handoff cookie.
// Setting and clearing use the same attributes so the browser matches and
// removes the same cookie.
func (c *Client) setLoginCookie(gc *gin.Context, value string, maxAge int) {
	gc.SetSameSite(http.SameSiteLaxMode)
	gc.SetCookie(oidcLoginCookie, value, maxAge, oidcLoginCookiePath, "", c.secureCookies, true)
}

// setOIDCCookies sets (or, with maxAge -1, clears) the one-shot state and
// nonce cookies. Setting and clearing use the same attributes so the browser
// matches and removes the same cookies.
func (c *Client) setOIDCCookies(gc *gin.Context, state, nonce string, maxAge int) {
	gc.SetSameSite(http.SameSiteLaxMode)
	gc.SetCookie("oidc_state", state, maxAge, "/", "", c.secureCookies, true)
	gc.SetCookie("oidc_nonce", nonce, maxAge, "/", "", c.secureCookies, true)
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// writeAuthError maps auth errors to HTTP responses, logging only the server
// faults so expected 401s do not pollute the operational log.
func (c *Client) writeAuthError(gc *gin.Context, err error) {
	if errors.Is(err, auth.ErrInvalidCredentials) {
		gc.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	c.log.Error("auth handler internal error", "err", err)
	gc.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
}

// writeToken writes a successful token response. RFC 6749 §5.1 requires
// responses carrying tokens to forbid caching.
func writeToken(gc *gin.Context, t auth.Token) {
	noStore(gc)
	gc.JSON(http.StatusOK, gin.H{
		"access_token": t.AccessToken,
		"token_type":   t.TokenType,
		"expires_in":   int(time.Until(t.ExpiresAt).Seconds()),
	})
}

// noStore stops browsers and intermediaries from caching a response that
// carries credentials.
func noStore(gc *gin.Context) {
	gc.Header("Cache-Control", "no-store")
	gc.Header("Pragma", "no-cache")
}
