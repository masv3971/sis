// Package authapi tells the web UI how to sign in. SIS takes no part in
// sign-in itself: the browser signs in at the configured issuer
// (authorization code flow with PKCE) and sends the access token it gets to
// the APIs. The entry points are methods on Client; route declarations live
// in the httpserver package.
package authapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/pkg/model"
)

// Client serves the web UI's sign-in settings. It is a "Client" rather than
// a "Service" because it owns no runtime resources and has nothing to shut
// down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	cfg *model.Cfg
}

// New builds the authapi Client.
func New(_ context.Context, cfg *model.Cfg) (*Client, error) {
	return &Client{cfg: cfg}, nil
}

// ConfigReply is the body of GET /auth/config.
type ConfigReply struct {
	// Enabled is false when the web UI has no sign-in configured; the other
	// fields are then empty.
	Enabled bool `json:"enabled"`
	// Issuer is where the browser signs in. Its discovery document gives
	// the authorization and token endpoints.
	Issuer string `json:"issuer,omitempty"`
	// ClientID is the web UI's public client at the issuer.
	ClientID string `json:"client_id,omitempty"`
	// Scope to request at sign-in.
	Scope string `json:"scope,omitempty"`
}

// Config returns the web UI's sign-in settings. Public: they are what the
// browser sends to the issuer anyway, and contain no secret.
func (c *Client) Config(gc *gin.Context) {
	a := c.cfg.SIS.Adapter.Inbound.AuthAPI
	if a.Issuer == "" {
		gc.JSON(http.StatusOK, ConfigReply{})
		return
	}
	gc.JSON(http.StatusOK, ConfigReply{Enabled: true, Issuer: a.Issuer, ClientID: a.ClientID, Scope: a.Scope})
}
