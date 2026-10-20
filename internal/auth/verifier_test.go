package auth_test

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/authtest"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func newVerifier(t *testing.T, issuers ...model.Issuer) *auth.Verifier {
	t.Helper()
	cfg := &model.Cfg{}
	cfg.SIS.Auth.Issuers = issuers
	v, err := auth.New(context.Background(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return v
}

func TestVerifyAcceptsTrustedTokens(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := newVerifier(t, iss.Config())

	c, err := v.Verify(context.Background(), iss.Token(t, "sunet-vc", "openid sis.read"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != "sunet-vc" || c.Issuer != iss.URL || !slices.Equal(c.Scopes, []string{"openid", "sis.read"}) {
		t.Fatalf("claims = %+v", c)
	}
	if !c.HasScope(auth.ScopeRead) || c.HasScope(auth.ScopeWrite) {
		t.Fatalf("HasScope wrong for %v", c.Scopes)
	}

	// Some issuers send scope as a JSON array.
	now := time.Now()
	c, err = v.Verify(context.Background(), iss.Sign(t, map[string]any{
		"iss": iss.URL, "sub": "liu", "aud": "sis", "exp": now.Add(time.Hour).Unix(),
		"scope": []string{"sis.read", "sis.write"},
	}))
	if err != nil || !c.HasScope(auth.ScopeWrite) {
		t.Fatalf("array scope: %+v %v", c, err)
	}
}

// TestVerifyRejects covers every way a token must fail. Removing the
// signature, issuer, audience or expiry check from Verify fails a case here.
func TestVerifyRejects(t *testing.T) {
	iss := authtest.NewIssuer(t)
	other := authtest.NewIssuer(t) // a real issuer SIS does not trust
	v := newVerifier(t, iss.Config())
	now := time.Now()
	claims := func(over map[string]any) map[string]any {
		c := map[string]any{"iss": iss.URL, "sub": "x", "aud": "sis", "exp": now.Add(time.Hour).Unix(), "scope": "sis.read"}
		for k, val := range over {
			if val == nil {
				delete(c, k)
				continue
			}
			c[k] = val
		}
		return c
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
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(context.Background(), tok); !errors.Is(err, auth.ErrInvalidToken) {
				t.Fatalf("expected ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestVerifyWithoutAudienceAcceptsAnyAud(t *testing.T) {
	iss := authtest.NewIssuer(t)
	cfg := iss.Config()
	cfg.Audience = ""
	v := newVerifier(t, cfg)
	iss.Audience = "whatever"
	if _, err := v.Verify(context.Background(), iss.Token(t, "x", "sis.read")); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyMissingScopeGrantsNothing(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := newVerifier(t, iss.Config())
	c, err := v.Verify(context.Background(), iss.Sign(t, map[string]any{
		"iss": iss.URL, "sub": "x", "aud": "sis", "exp": time.Now().Add(time.Hour).Unix(),
	}))
	if err != nil || len(c.Scopes) != 0 {
		t.Fatalf("got %+v %v", c, err)
	}
}

// TestVerifyTrustsSeveralIssuers checks each issuer's tokens verify against
// its own keys.
func TestVerifyTrustsSeveralIssuers(t *testing.T) {
	a, b := authtest.NewIssuer(t), authtest.NewIssuer(t)
	v := newVerifier(t, a.Config(), b.Config())
	for _, iss := range []*authtest.Issuer{a, b} {
		if _, err := v.Verify(context.Background(), iss.Token(t, "x", "sis.read")); err != nil {
			t.Fatalf("%s: %v", iss.URL, err)
		}
	}
}

// TestVerifyRecoversWhenIssuerComesUp checks a down issuer only rejects its
// tokens until it is reachable, without a restart, and that keys are cached.
func TestVerifyRecoversWhenIssuerComesUp(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := newVerifier(t, iss.Config())
	tok := iss.Token(t, "x", "sis.read")

	iss.Down.Store(true)
	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("down issuer: expected ErrInvalidToken, got %v", err)
	}
	iss.Down.Store(false)
	for range 3 {
		if _, err := v.Verify(context.Background(), tok); err != nil {
			t.Fatalf("after recovery: %v", err)
		}
	}
	if hits := iss.JWKSHits.Load(); hits != 1 {
		t.Fatalf("JWKS fetched %d times, want 1 (cached)", hits)
	}
}

// TestVerifyUsesConfiguredJWKSURL checks jwks_url skips discovery, for when
// SIS reaches the issuer under another address than iss.
func TestVerifyUsesConfiguredJWKSURL(t *testing.T) {
	iss := authtest.NewIssuer(t)
	cfg := iss.Config()
	cfg.JWKSURL = iss.URL + "/certs"
	cfg.Issuer = "https://issuer.example/realms/sis" // not reachable: discovery would fail
	v := newVerifier(t, cfg)
	tok := iss.Sign(t, map[string]any{
		"iss": cfg.Issuer, "sub": "x", "aud": "sis", "exp": time.Now().Add(time.Hour).Unix(), "scope": "sis.read",
	})
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestVerifyTrustsCAFiles checks an issuer served over https with a
// certificate from a private CA is reachable only once that CA is in
// sis.auth.ca_files.
func TestVerifyTrustsCAFiles(t *testing.T) {
	iss := authtest.NewTLSIssuer(t)
	tok := iss.Token(t, "x", "sis.read")

	if _, err := newVerifier(t, iss.Config()).Verify(context.Background(), tok); err == nil {
		t.Fatal("expected failure: the issuer's CA is not trusted")
	}

	cfg := &model.Cfg{}
	cfg.SIS.Auth.Issuers = []model.Issuer{iss.Config()}
	cfg.SIS.Auth.CAFiles = []string{iss.CAFile(t)}
	v, err := auth.New(context.Background(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("with ca_files: %v", err)
	}

	cfg.SIS.Auth.CAFiles = []string{"/no/such/ca.pem"}
	if _, err := auth.New(context.Background(), cfg, logger.NewWithWriter("test", false, "error", io.Discard)); err == nil {
		t.Fatal("expected an error for a missing ca file")
	}
}

func TestNewRejectsDuplicateIssuer(t *testing.T) {
	cfg := &model.Cfg{}
	cfg.SIS.Auth.Issuers = []model.Issuer{{Issuer: "https://a"}, {Issuer: "https://a"}}
	if _, err := auth.New(context.Background(), cfg, logger.NewWithWriter("test", false, "error", io.Discard)); err == nil {
		t.Fatal("expected duplicate issuer error")
	}
}

// TestVerifyProductionRequiresHTTPSKeys checks that in production signing
// keys are never fetched over http, whether discovery advertises an http
// jwks_uri or an https key URL redirects to http. Outside production both
// work, so the failure is the scheme check.
func TestVerifyProductionRequiresHTTPSKeys(t *testing.T) {
	keys := authtest.NewIssuer(t) // http: serves the keys the tokens are signed with
	exp := time.Now().Add(time.Hour).Unix()

	verify := func(t *testing.T, production bool, ic model.Issuer, caFile, tok string) error {
		t.Helper()
		cfg := &model.Cfg{}
		cfg.Common.Production = production
		cfg.SIS.Auth.Issuers = []model.Issuer{ic}
		cfg.SIS.Auth.CAFiles = []string{caFile}
		v, err := auth.New(context.Background(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
		if err != nil {
			t.Fatal(err)
		}
		_, err = v.Verify(context.Background(), tok)
		return err
	}

	t.Run("discovered http jwks_uri", func(t *testing.T) {
		iss := authtest.NewTLSIssuer(t)
		iss.JWKSURI = keys.URL + "/certs"
		tok := keys.Sign(t, map[string]any{"iss": iss.URL, "sub": "x", "aud": "sis", "exp": exp, "scope": "sis.read"})
		ca := iss.CAFile(t)
		if err := verify(t, false, iss.Config(), ca, tok); err != nil {
			t.Fatalf("development: %v", err)
		}
		if err := verify(t, true, iss.Config(), ca, tok); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("production: expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("redirect to http", func(t *testing.T) {
		redir := httptest.NewTLSServer(http.RedirectHandler(keys.URL+"/certs", http.StatusFound))
		t.Cleanup(redir.Close)
		ca := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: redir.Certificate().Raw}), 0o600); err != nil {
			t.Fatal(err)
		}
		ic := keys.Config()
		ic.JWKSURL = redir.URL + "/certs"
		tok := keys.Token(t, "x", "sis.read")
		if err := verify(t, false, ic, ca, tok); err != nil {
			t.Fatalf("development: %v", err)
		}
		if err := verify(t, true, ic, ca, tok); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("production: expected ErrInvalidToken, got %v", err)
		}
	})
}
