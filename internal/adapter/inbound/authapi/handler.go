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
	log          *logger.Log
}

// New builds the authapi Client.
func New(_ context.Context, cfg *model.Cfg, authClient *auth.Client, log *logger.Log) (*Client, error) {
	return &Client{
		auth:         authClient,
		postLoginURL: cfg.SIS.Adapter.Inbound.AuthAPI.PostLoginRedirectURL,
		log:          log.New("authapi"),
	}, nil
}

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
	gc.SetSameSite(http.SameSiteLaxMode)
	gc.SetCookie("oidc_state", state, oidcCookieTTL, "/", "", false, true)
	gc.SetCookie("oidc_nonce", nonce, oidcCookieTTL, "/", "", false, true)
	gc.Redirect(http.StatusFound, c.auth.OIDCAuthCodeURL(state, nonce))
}

// OIDCCallback completes the OIDC RP flow and hands a token to the SPA via the
// URL fragment.
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

	// Clear the one-shot cookies and hand the token to the SPA via the URL
	// fragment (kept out of server logs and the Referer header).
	noStore(gc)
	gc.SetCookie("oidc_state", "", -1, "/", "", false, true)
	gc.SetCookie("oidc_nonce", "", -1, "/", "", false, true)
	gc.Redirect(http.StatusFound, c.postLoginURL+"#access_token="+tok.AccessToken)
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
