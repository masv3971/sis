package auth_test

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/authtest"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func newVerifier(t *testing.T, issuers ...model.Issuer) *auth.Verifier {
	t.Helper()
	cfg := &model.Cfg{}
	cfg.SIS.Auth.Issuers = issuers
	verifier, err := auth.New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	require.NoError(t, err, "auth.New")
	return verifier
}

func TestVerifyAcceptsTrustedTokens(t *testing.T) {
	iss := authtest.NewIssuer(t)
	verifier := newVerifier(t, iss.Config())

	claims, err := verifier.Verify(t.Context(), iss.Token(t, "sunet-vc", "openid sis.read"))
	require.NoError(t, err, "Verify")
	assert.Equal(t, "sunet-vc", claims.Subject)
	assert.Equal(t, iss.URL, claims.Issuer)
	assert.Equal(t, []string{"openid", "sis.read"}, claims.Scopes)
	assert.True(t, claims.HasScope(auth.ScopeRead), "HasScope(read) for %v", claims.Scopes)
	assert.False(t, claims.HasScope(auth.ScopeWrite), "HasScope(write) for %v", claims.Scopes)

	// Some issuers send scope as a JSON array.
	now := time.Now()
	claims, err = verifier.Verify(t.Context(), iss.Sign(t, map[string]any{
		"iss": iss.URL, "sub": "liu", "aud": "sis", "exp": now.Add(time.Hour).Unix(),
		"scope": []string{"sis.read", "sis.write"},
	}))
	require.NoError(t, err, "array scope")
	assert.True(t, claims.HasScope(auth.ScopeWrite), "array scope: %+v", claims)

	// Access token typ headers in use (RFC 9068 at+jwt, plain JWT, none).
	for _, typ := range []string{"at+jwt", "application/at+jwt", "AT+JWT", "JWT", ""} {
		token := iss.SignTyp(t, typ, map[string]any{
			"iss": iss.URL, "sub": "x", "aud": "sis", "exp": now.Add(time.Hour).Unix(), "scope": "sis.read", "typ": "Bearer",
		})
		_, err := verifier.Verify(t.Context(), token)
		require.NoError(t, err, "typ %q", typ)
	}
}

// TestVerifyRejects covers every way a token must fail. Removing the
// signature, issuer, audience or expiry check from Verify fails a case here.
func TestVerifyRejects(t *testing.T) {
	iss := authtest.NewIssuer(t)
	other := authtest.NewIssuer(t) // a real issuer SIS does not trust
	verifier := newVerifier(t, iss.Config())
	now := time.Now()
	claims := func(over map[string]any) map[string]any {
		base := map[string]any{"iss": iss.URL, "sub": "x", "aud": "sis", "exp": now.Add(time.Hour).Unix(), "scope": "sis.read"}
		for key, val := range over {
			if val == nil {
				delete(base, key)
				continue
			}
			base[key] = val
		}
		return base
	}
	cases := map[string]string{
		"not a jwt":         "abc",
		"garbage payload":   "a.b.c",
		"untrusted issuer":  other.Token(t, "x", "sis.read sis.write"),
		"forged iss":        other.Sign(t, claims(nil)),
		"bad signature":     iss.SignWith(t, claims(nil)),
		"alg none":          authtest.Unsigned(t, claims(nil)),
		"expired":           iss.Sign(t, claims(map[string]any{"exp": now.Add(-time.Minute).Unix()})),
		"no exp":            iss.Sign(t, claims(map[string]any{"exp": nil})),
		"wrong audience":    iss.Sign(t, claims(map[string]any{"aud": "some-other-service"})),
		"no audience":       iss.Sign(t, claims(map[string]any{"aud": nil})),
		"not yet valid":     iss.Sign(t, claims(map[string]any{"nbf": now.Add(time.Hour).Unix()})),
		"scope wrong type":  iss.Sign(t, claims(map[string]any{"scope": 42})),
		"iss trailing /":    iss.Sign(t, claims(map[string]any{"iss": iss.URL + "/"})),
		"iss case mismatch": iss.Sign(t, claims(map[string]any{"iss": iss.URL + "X"})),
		"keycloak id token": iss.Sign(t, claims(map[string]any{"typ": "ID"})),
		"logout token":      iss.SignTyp(t, "logout+jwt", claims(nil)),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := verifier.Verify(t.Context(), token)
			assert.ErrorIs(t, err, auth.ErrInvalidToken)
		})
	}
}

func TestVerifyWithoutAudienceAcceptsAnyAud(t *testing.T) {
	iss := authtest.NewIssuer(t)
	cfg := iss.Config()
	cfg.Audience = ""
	verifier := newVerifier(t, cfg)
	iss.Audience = "whatever"
	_, err := verifier.Verify(t.Context(), iss.Token(t, "x", "sis.read"))
	assert.NoError(t, err, "Verify")
}

func TestVerifyMissingScopeGrantsNothing(t *testing.T) {
	iss := authtest.NewIssuer(t)
	verifier := newVerifier(t, iss.Config())
	claims, err := verifier.Verify(t.Context(), iss.Sign(t, map[string]any{
		"iss": iss.URL, "sub": "x", "aud": "sis", "exp": time.Now().Add(time.Hour).Unix(),
	}))
	require.NoError(t, err)
	assert.Empty(t, claims.Scopes)
}

// TestVerifyTrustsSeveralIssuers checks each issuer's tokens verify against
// its own keys.
func TestVerifyTrustsSeveralIssuers(t *testing.T) {
	first, second := authtest.NewIssuer(t), authtest.NewIssuer(t)
	verifier := newVerifier(t, first.Config(), second.Config())
	for _, iss := range []*authtest.Issuer{first, second} {
		_, err := verifier.Verify(t.Context(), iss.Token(t, "x", "sis.read"))
		require.NoError(t, err, iss.URL)
	}
}

// TestVerifyRecoversWhenIssuerComesUp checks a down issuer only rejects its
// tokens until it is reachable, without a restart, that a failed discovery
// is reused for the retry delay, and that keys are cached.
func TestVerifyRecoversWhenIssuerComesUp(t *testing.T) {
	iss := authtest.NewIssuer(t)
	verifier := newVerifier(t, iss.Config())
	token := iss.Token(t, "x", "sis.read")

	auth.SetRetryDelay(t, time.Hour)
	iss.Down.Store(true)
	_, err := verifier.Verify(t.Context(), token)
	require.ErrorIs(t, err, auth.ErrIssuerUnavailable, "down issuer")
	iss.Down.Store(false)
	_, err = verifier.Verify(t.Context(), token)
	require.ErrorIs(t, err, auth.ErrIssuerUnavailable, "within retry delay: expected the cached failure")
	auth.SetRetryDelay(t, 0)
	for range 3 {
		_, err := verifier.Verify(t.Context(), token)
		require.NoError(t, err, "after recovery")
	}
	assert.EqualValues(t, 1, iss.JWKSHits.Load(), "JWKS fetches, want 1 (cached)")
}

// TestVerifyKeysUnavailable checks a JWKS endpoint that fails is reported as
// an issuer outage, not as a bad token.
func TestVerifyKeysUnavailable(t *testing.T) {
	iss := authtest.NewIssuer(t)
	cfg := iss.Config()
	cfg.JWKSURL = iss.URL + "/certs"
	verifier := newVerifier(t, cfg)
	iss.Down.Store(true)
	_, err := verifier.Verify(t.Context(), iss.Token(t, "x", "sis.read"))
	assert.ErrorIs(t, err, auth.ErrIssuerUnavailable)
}

// TestVerifyUsesConfiguredJWKSURL checks jwks_url skips discovery, for when
// SIS reaches the issuer under another address than iss.
func TestVerifyUsesConfiguredJWKSURL(t *testing.T) {
	iss := authtest.NewIssuer(t)
	cfg := iss.Config()
	cfg.JWKSURL = iss.URL + "/certs"
	cfg.Issuer = "https://issuer.example/realms/sis" // not reachable: discovery would fail
	verifier := newVerifier(t, cfg)
	token := iss.Sign(t, map[string]any{
		"iss": cfg.Issuer, "sub": "x", "aud": "sis", "exp": time.Now().Add(time.Hour).Unix(), "scope": "sis.read",
	})
	_, err := verifier.Verify(t.Context(), token)
	assert.NoError(t, err, "Verify")
}

// TestVerifyTrustsCAFiles checks an issuer served over https with a
// certificate from a private CA is reachable only once that CA is in
// sis.auth.ca_files.
func TestVerifyTrustsCAFiles(t *testing.T) {
	iss := authtest.NewTLSIssuer(t)
	token := iss.Token(t, "x", "sis.read")

	_, err := newVerifier(t, iss.Config()).Verify(t.Context(), token)
	require.Error(t, err, "expected failure: the issuer's CA is not trusted")

	cfg := &model.Cfg{}
	cfg.SIS.Auth.Issuers = []model.Issuer{iss.Config()}
	cfg.SIS.Auth.CAFiles = []string{iss.CAFile(t)}
	verifier, err := auth.New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	require.NoError(t, err)
	_, err = verifier.Verify(t.Context(), token)
	require.NoError(t, err, "with ca_files")

	cfg.SIS.Auth.CAFiles = []string{"/no/such/ca.pem"}
	_, err = auth.New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	assert.Error(t, err, "expected an error for a missing ca file")
}

func TestNewRejectsDuplicateIssuer(t *testing.T) {
	cfg := &model.Cfg{}
	cfg.SIS.Auth.Issuers = []model.Issuer{{Issuer: "https://a"}, {Issuer: "https://a"}}
	_, err := auth.New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	assert.Error(t, err, "expected duplicate issuer error")
}

// TestVerifyProductionRequiresHTTPSKeys checks that in production signing
// keys are never fetched over http, whether discovery advertises an http
// jwks_uri or an https key URL redirects to http. Outside production both
// work, so the failure is the scheme check.
func TestVerifyProductionRequiresHTTPSKeys(t *testing.T) {
	keys := authtest.NewIssuer(t) // http: serves the keys the tokens are signed with
	exp := time.Now().Add(time.Hour).Unix()

	verify := func(t *testing.T, production bool, issuer model.Issuer, caFile, token string) error {
		t.Helper()
		cfg := &model.Cfg{}
		cfg.Common.Production = production
		cfg.SIS.Auth.Issuers = []model.Issuer{issuer}
		cfg.SIS.Auth.CAFiles = []string{caFile}
		verifier, err := auth.New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
		require.NoError(t, err)
		_, err = verifier.Verify(t.Context(), token)
		return err
	}

	t.Run("discovered http jwks_uri", func(t *testing.T) {
		iss := authtest.NewTLSIssuer(t)
		iss.JWKSURI = keys.URL + "/certs"
		token := keys.Sign(t, map[string]any{"iss": iss.URL, "sub": "x", "aud": "sis", "exp": exp, "scope": "sis.read"})
		ca := iss.CAFile(t)
		require.NoError(t, verify(t, false, iss.Config(), ca, token), "development")
		require.ErrorIs(t, verify(t, true, iss.Config(), ca, token), auth.ErrIssuerUnavailable, "production")
	})

	t.Run("redirect to http", func(t *testing.T) {
		redir := httptest.NewTLSServer(http.RedirectHandler(keys.URL+"/certs", http.StatusFound))
		t.Cleanup(redir.Close)
		ca := filepath.Join(t.TempDir(), "ca.pem")
		require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: redir.Certificate().Raw}), 0o600))
		issuer := keys.Config()
		issuer.JWKSURL = redir.URL + "/certs"
		token := keys.Token(t, "x", "sis.read")
		require.NoError(t, verify(t, false, issuer, ca, token), "development")
		require.ErrorIs(t, verify(t, true, issuer, ca, token), auth.ErrIssuerUnavailable, "production")
	})
}
