// Package oidcrp is the OIDC Relying Party: it wraps go-oidc discovery, the
// OAuth2 code flow, and ID-token verification for delegated login.
package oidcrp

import (
	"context"
	"errors"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Claims is the subset of verified ID-token claims the service uses. Issuer
// and Subject together are the stable identity of the user; Email and Name
// are informational and must not be used to match local accounts.
type Claims struct {
	Issuer  string
	Subject string
	Email   string
	Name    string
}

// Client performs the OIDC authorization-code flow against a configured provider.
type Client struct {
	verifier *coreoidc.IDTokenVerifier
	oauth    oauth2.Config
}

// New builds the OIDC Relying Party from configuration, following the SUNET/vc
// shape New(ctx, cfg, deps..., log). When cfg.SIS.Auth.OIDCRP.IssuerURL is empty
// delegated login is disabled: (nil, nil) is returned so main can wire the
// resulting (possibly nil) Client into the HTTP server unconditionally. When
// set, provider discovery runs and a failure is reported.
func New(ctx context.Context, cfg *model.Cfg, log *logger.Log) (*Client, error) {
	oc := cfg.SIS.Auth.OIDCRP
	if oc.IssuerURL == "" {
		return nil, nil
	}
	log = log.New("oidcrp")
	provider, err := coreoidc.NewProvider(ctx, oc.IssuerURL)
	if err != nil {
		return nil, err
	}
	log.Info("OIDC RP enabled", "issuer", oc.IssuerURL)
	return &Client{
		verifier: provider.Verifier(&coreoidc.Config{ClientID: oc.ClientID}),
		oauth: oauth2.Config{
			ClientID:     oc.ClientID,
			ClientSecret: oc.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  oc.RedirectURL,
			Scopes:       []string{coreoidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

// AuthCodeURL builds the provider authorization URL for the given state and nonce.
func (r *Client) AuthCodeURL(state, nonce string) string {
	return r.oauth.AuthCodeURL(state, coreoidc.Nonce(nonce))
}

// Exchange trades the authorization code for tokens, verifies the ID token and
// its nonce, and returns the identity claims.
func (r *Client) Exchange(ctx context.Context, code, nonce string) (*Claims, error) {
	tok, err := r.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in token response")
	}
	idToken, err := r.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, err
	}
	if idToken.Nonce != nonce {
		return nil, errors.New("nonce mismatch")
	}
	var c struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&c); err != nil {
		return nil, err
	}
	return &Claims{Issuer: idToken.Issuer, Subject: idToken.Subject, Email: c.Email, Name: c.Name}, nil
}
