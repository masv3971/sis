// Package auth checks access tokens. SIS issues no tokens and stores no
// credentials: a caller gets a JWT from a trusted issuer (an OIDC provider)
// and sends it as a Bearer token. Verifier checks it was signed by one of
// the issuers in sis.auth.issuers, and the middleware lets the request
// through when the token's scope claim holds the scope the route needs
// (sis.read or sis.write). Who may do what is decided at the issuer, so
// access changes need no restart of SIS.
package auth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Scopes the API routes require.
const (
	// ScopeRead allows every read: GET on /api/v1, POST /api/v1/parse, and
	// all of /oeapi and /eduapi.
	ScopeRead = "sis.read"
	// ScopeWrite allows every write: POST, PUT and DELETE on /api/v1 and
	// POST /api/v1/populate. It does not include ScopeRead.
	ScopeWrite = "sis.write"
)

// Claims is what SIS uses from a verified access token.
type Claims struct {
	Issuer  string
	Subject string
	// Scopes from the scope claim (RFC 9068 §2.2.3: a space-separated
	// string). A JSON array is accepted too.
	Scopes []string
}

// HasScope reports whether the token grants scope.
func (c *Claims) HasScope(scope string) bool { return slices.Contains(c.Scopes, scope) }

// ErrInvalidToken is returned for any token that is not accepted, without
// saying why to the caller (the reason is in the wrapped error, for logs).
var ErrInvalidToken = errors.New(helpers.MsgInvalidToken)

// ErrIssuerUnavailable is returned when the token's issuer could not be
// reached (discovery or keys), so the token could not be checked at all.
var ErrIssuerUnavailable = errors.New(helpers.MsgIssuerUnavailable)

// httpTimeout bounds every request to an issuer (discovery and JWKS), so an
// issuer that accepts the connection but never answers cannot hang a
// request. A var so tests can shorten it.
var httpTimeout = 10 * time.Second

// retryDelay is how long a failed discovery is reused before the issuer is
// asked again, so an outage costs one attempt per delay, not one per request.
var retryDelay = 5 * time.Second

// Verifier checks access tokens against the trusted issuers.
//
// It is a "Client" in all but name: it holds no runtime resources that need
// closing (key sets are plain HTTP caches), so it has no Close.
type Verifier struct {
	issuers map[string]*issuer
	log     *logger.Log
}

// issuer is one trusted issuer. Its key set is built on first use, so SIS
// starts even when an issuer is down; until it is reachable its tokens are
// rejected and the next request tries again.
type issuer struct {
	cfg        model.Issuer
	httpClient *http.Client
	// httpsOnly (production) rejects an http jwks_uri from discovery.
	httpsOnly bool

	mu       sync.Mutex
	verifier *coreoidc.IDTokenVerifier
	failErr  error
	failedAt time.Time
}

// New builds the Verifier from sis.auth.issuers. It does not contact the
// issuers.
func New(_ context.Context, cfg *model.Cfg, log *logger.Log) (*Verifier, error) {
	log = log.New("auth")
	v := &Verifier{issuers: map[string]*issuer{}, log: log}
	httpsOnly := cfg.Common.Production
	httpClient, err := newHTTPClient(cfg.SIS.Auth.CAFiles, httpsOnly)
	if err != nil {
		return nil, err
	}
	for _, issuerCfg := range cfg.SIS.Auth.Issuers {
		if _, dup := v.issuers[issuerCfg.Issuer]; dup {
			return nil, fmt.Errorf("auth: issuer %q listed twice", issuerCfg.Issuer)
		}
		v.issuers[issuerCfg.Issuer] = &issuer{cfg: issuerCfg, httpClient: httpClient, httpsOnly: httpsOnly}
		log.Info("trusting token issuer", "issuer", issuerCfg.Issuer, "audience", issuerCfg.Audience)
	}
	if len(v.issuers) == 0 {
		log.Warn("sis.auth.issuers is empty; every API request will be rejected")
	}
	return v, nil
}

// newHTTPClient returns the client for discovery and JWKS requests. caFiles
// are trusted in addition to the system roots, e.g. a development CA. With
// httpsOnly, redirects to anything but https are refused.
func newHTTPClient(caFiles []string, httpsOnly bool) (*http.Client, error) {
	httpClient := &http.Client{Timeout: httpTimeout}
	if httpsOnly {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("redirect to %s: https is required in production", req.URL.Redacted())
			}
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		}
	}
	if len(caFiles) == 0 {
		return httpClient, nil
	}
	pool, err := helpers.CertPool(caFiles)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	httpClient.Transport = transport
	return httpClient, nil
}

// Verify checks raw was signed by a trusted issuer and is valid now
// (signature, iss, exp, nbf and, when configured, aud), and returns its
// claims. A token that fails is ErrInvalidToken; one that cannot be checked
// because its issuer is unreachable is ErrIssuerUnavailable.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	var unverified struct {
		Issuer string `json:"iss"`
	}
	if err := unverifiedPayload(raw, &unverified); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	// Only the issuer is read before verification, to pick which keys to
	// check the signature with. An unknown issuer is rejected without
	// any network call.
	trusted, ok := v.issuers[unverified.Issuer]
	if !ok {
		return nil, fmt.Errorf("%w: untrusted issuer %q", ErrInvalidToken, unverified.Issuer)
	}
	idVerifier, err := trusted.tokenVerifier(ctx)
	if err != nil {
		v.log.Warn("token issuer unavailable", "issuer", trusted.cfg.Issuer, "err", err)
		return nil, fmt.Errorf("%w: %v", ErrIssuerUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(coreoidc.ClientContext(ctx, trusted.httpClient), httpTimeout)
	defer cancel()
	var fetchErr error
	idToken, err := idVerifier.Verify(context.WithValue(ctx, fetchErrKey{}, &fetchErr), raw)
	if fetchErr != nil {
		v.log.Warn("token issuer keys unavailable", "issuer", trusted.cfg.Issuer, "err", fetchErr)
		return nil, fmt.Errorf("%w: %v", ErrIssuerUnavailable, fetchErr)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if err := checkAccessToken(raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	var extra struct {
		Scope json.RawMessage `json:"scope"`
		Typ   string          `json:"typ"`
	}
	if err := idToken.Claims(&extra); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	// Keycloak marks its ID tokens with typ "ID" in the payload.
	if strings.EqualFold(extra.Typ, "ID") {
		return nil, fmt.Errorf("%w: an ID token is not an access token", ErrInvalidToken)
	}
	scopes, err := parseScope(extra.Scope)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return &Claims{Issuer: idToken.Issuer, Subject: idToken.Subject, Scopes: scopes}, nil
}

// tokenVerifier returns the issuer's verifier, building it on first success.
// A failed discovery is reused for retryDelay, then retried. Callers that
// queue on mu meanwhile get that one attempt's result.
func (i *issuer) tokenVerifier(ctx context.Context) (*coreoidc.IDTokenVerifier, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.verifier != nil {
		return i.verifier, nil
	}
	if i.failErr != nil && time.Since(i.failedAt) < retryDelay {
		return nil, i.failErr
	}
	idVerifier, err := i.newTokenVerifier(ctx)
	if err != nil {
		i.failErr, i.failedAt = err, time.Now()
		return nil, err
	}
	i.verifier, i.failErr = idVerifier, nil
	return idVerifier, nil
}

func (i *issuer) newTokenVerifier(ctx context.Context) (*coreoidc.IDTokenVerifier, error) {
	// Key sets refresh in the background on an unknown key id, long after
	// this request: they keep the HTTP client from ctx, so detach from its
	// cancellation but keep the timeout-bound client.
	keyCtx := coreoidc.ClientContext(context.WithoutCancel(ctx), i.httpClient)
	jwksURL := i.cfg.JWKSURL
	if jwksURL == "" {
		var err error
		if jwksURL, err = discoverJWKS(keyCtx, i.httpClient, i.cfg.Issuer); err != nil {
			return nil, err
		}
		// The keys decide who gets in: in production they must not come
		// over a tamperable channel, whatever discovery advertises.
		if i.httpsOnly && !helpers.IsHTTPS(jwksURL) {
			return nil, fmt.Errorf("discovered jwks_uri %s: https is required in production", jwksURL)
		}
	}
	return coreoidc.NewVerifier(i.cfg.Issuer, keySet{coreoidc.NewRemoteKeySet(keyCtx, jwksURL)}, &coreoidc.Config{
		ClientID: i.cfg.Audience,
		// Without an audience every token of the issuer is accepted;
		// production config requires one.
		SkipClientIDCheck: i.cfg.Audience == "",
		// Asymmetric algorithms only: a shared-secret (HS*) token would
		// mean the issuer's verification key is also a signing key.
		SupportedSigningAlgs: []string{
			coreoidc.RS256, coreoidc.RS384, coreoidc.RS512,
			coreoidc.ES256, coreoidc.ES384, coreoidc.ES512,
			coreoidc.PS256, coreoidc.PS384, coreoidc.PS512, coreoidc.EdDSA,
		},
	}), nil
}

type fetchErrKey struct{}

// keySet reports a failed key fetch into the *error stored under
// fetchErrKey, because go-oidc flattens KeySet errors into a string.
type keySet struct{ remote *coreoidc.RemoteKeySet }

func (k keySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	payload, err := k.remote.VerifySignature(ctx, jwt)
	// RemoteKeySet prefixes exactly its fetch failures this way.
	if err != nil && strings.HasPrefix(err.Error(), "fetching keys") {
		if errSlot, ok := ctx.Value(fetchErrKey{}).(*error); ok {
			*errSlot = err
		}
	}
	return payload, err
}

// discoverJWKS reads jwks_uri from the issuer's discovery document. It does
// not require the document's issuer to match: the configured issuer is
// checked against every token's iss claim instead.
func discoverJWKS(ctx context.Context, httpClient *http.Client, issuerURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("discovery %s: %w", discoveryURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery %s: status %d", discoveryURL, resp.StatusCode)
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("discovery %s: %w", discoveryURL, err)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery %s: no jwks_uri", discoveryURL)
	}
	return doc.JWKSURI, nil
}

// parseScope reads a scope claim: a space-separated string (RFC 9068), or a
// JSON array of strings as some issuers send. A missing claim grants nothing.
func parseScope(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var spaced string
	if err := json.Unmarshal(raw, &spaced); err == nil {
		return strings.Fields(spaced), nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("scope claim is neither a string nor a list of strings")
	}
	return list, nil
}

// checkAccessToken rejects a JWT whose JOSE typ header names another kind of
// token (e.g. logout+jwt). RFC 9068 access tokens say at+jwt; many issuers
// send JWT or nothing, so those pass too. Call it after the signature check.
func checkAccessToken(raw string) error {
	head, _, _ := strings.Cut(raw, ".")
	headerJSON, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(head, "="))
	if err != nil {
		return fmt.Errorf("header: %w", err)
	}
	var header struct {
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return fmt.Errorf("header: %w", err)
	}
	switch strings.TrimPrefix(strings.ToLower(header.Typ), "application/") {
	case "", "jwt", "at+jwt":
		return nil
	}
	return fmt.Errorf("typ %q is not an access token", header.Typ)
}

// unverifiedPayload decodes a compact JWS payload without checking the
// signature. Use it only to route a token to its verifier.
func unverifiedPayload(raw string, v any) error {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return errors.New("not a compact JWT")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	return json.Unmarshal(payloadJSON, v)
}
