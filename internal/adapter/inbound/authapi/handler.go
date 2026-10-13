// Package authapi is the inbound HTTP adapter for authentication: local login
// and the OAuth2 client-credentials token endpoint.
package authapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/auth"
)

// Handler serves the auth endpoints.
type Handler struct {
	svc *auth.Service
}

// New returns a Handler backed by the auth service.
func New(svc *auth.Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the (unauthenticated) auth routes to the engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.POST("/auth/login", h.login)
	engine.POST("/oauth/token", h.token)
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	tok, err := h.svc.Authenticate(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, tokenResponse(tok))
}

// token implements the OAuth2 client-credentials grant (RFC 6749 §4.4).
// Credentials come from form fields or HTTP Basic auth.
func (h *Handler) token(c *gin.Context) {
	if c.PostForm("grant_type") != "client_credentials" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}
	clientID, secret := c.PostForm("client_id"), c.PostForm("client_secret")
	if clientID == "" {
		if id, s, ok := c.Request.BasicAuth(); ok {
			clientID, secret = id, s
		}
	}
	tok, err := h.svc.ClientCredentials(c.Request.Context(), clientID, secret)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.JSON(http.StatusOK, tokenResponse(tok))
}

func writeAuthError(c *gin.Context, err error) {
	if errors.Is(err, auth.ErrInvalidCredentials) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
}

func tokenResponse(t auth.Token) gin.H {
	return gin.H{
		"access_token": t.AccessToken,
		"token_type":   t.TokenType,
		"expires_in":   int(time.Until(t.ExpiresAt).Seconds()),
	}
}
