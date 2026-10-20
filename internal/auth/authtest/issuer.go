// Package authtest is a local OIDC token issuer for tests: discovery, a JWKS
// endpoint, and RS256 signing with stdlib crypto, so tests need neither a
// real identity provider nor a JOSE dependency.
package authtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SUNET/sis/pkg/model"
)

// Issuer serves discovery and JWKS for one RSA key and signs tokens with it.
type Issuer struct {
	URL      string
	Audience string
	key      *rsa.PrivateKey
	kid      string
	// JWKSHits counts JWKS fetches.
	JWKSHits atomic.Int64
	// Down makes discovery and JWKS answer 503.
	Down atomic.Bool
	// JWKSURI, when set before the first request, replaces the jwks_uri
	// that discovery advertises.
	JWKSURI string
	// srv is set for TLS issuers, to expose their certificate.
	srv *httptest.Server
}

// NewIssuer starts an http issuer with a fresh key; it stops with the test.
func NewIssuer(t testing.TB) *Issuer { return newIssuer(t, false) }

// NewTLSIssuer starts an https issuer whose certificate is from its own CA,
// which nothing trusts until CAFile is added to the trusted roots.
func NewTLSIssuer(t testing.TB) *Issuer { return newIssuer(t, true) }

// CAFile writes the TLS issuer's certificate (its own CA) to a PEM file and
// returns its path.
func (i *Issuer) CAFile(t testing.TB) string {
	t.Helper()
	if i.srv == nil || i.srv.TLS == nil {
		t.Fatal("authtest: CAFile needs NewTLSIssuer")
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	block := &pem.Block{Type: "CERTIFICATE", Bytes: i.srv.Certificate().Raw}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newIssuer(t testing.TB, useTLS bool) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	iss := &Issuer{Audience: "sis", key: key, kid: "k1"}
	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	if useTLS {
		srv.StartTLS()
		iss.srv = srv
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	iss.URL = srv.URL + "/realms/sis"
	mux.HandleFunc("/realms/sis/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		if iss.Down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		jwks := iss.URL + "/certs"
		if iss.JWKSURI != "" {
			jwks = iss.JWKSURI
		}
		writeJSON(w, map[string]any{"issuer": iss.URL, "jwks_uri": jwks})
	})
	mux.HandleFunc("/realms/sis/certs", func(w http.ResponseWriter, _ *http.Request) {
		iss.JWKSHits.Add(1)
		if iss.Down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": iss.kid,
			"n": b64(iss.key.N.Bytes()),
			"e": b64(big.NewInt(int64(iss.key.E)).Bytes()),
		}}})
	})
	return iss
}

// Config is the sis.auth.issuers entry that trusts this issuer.
func (i *Issuer) Config() model.Issuer {
	return model.Issuer{Issuer: i.URL, Audience: i.Audience}
}

// Token signs an access token for sub with the given space-separated scope,
// valid for an hour, with iss and aud set to this issuer's.
func (i *Issuer) Token(t testing.TB, sub, scope string) string {
	t.Helper()
	now := time.Now()
	return i.Sign(t, map[string]any{
		"iss": i.URL, "sub": sub, "aud": []string{i.Audience},
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "scope": scope,
	})
}

// Sign signs arbitrary claims with this issuer's key (RS256).
func (i *Issuer) Sign(t testing.TB, claims map[string]any) string {
	t.Helper()
	return i.SignTyp(t, "JWT", claims)
}

// SignTyp is Sign with the given JOSE typ header ("" omits it).
func (i *Issuer) SignTyp(t testing.TB, typ string, claims map[string]any) string {
	t.Helper()
	h := map[string]string{"alg": "RS256", "kid": i.kid}
	if typ != "" {
		h["typ"] = typ
	}
	return sign(t, i.key, h, claims)
}

// SignWith signs claims with an unrelated key under this issuer's key id, so
// the signature does not verify.
func (i *Issuer) SignWith(t testing.TB, claims map[string]any) string {
	t.Helper()
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return sign(t, other, map[string]string{"alg": "RS256", "typ": "JWT", "kid": i.kid}, claims)
}

// Unsigned returns claims as an alg=none token.
func Unsigned(t testing.TB, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	p, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return b64(h) + "." + b64(p) + "."
}

func sign(t testing.TB, key *rsa.PrivateKey, header map[string]string, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(header)
	p, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	input := b64(h) + "." + b64(p)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + b64(sig)
}

// Bearer returns the Authorization header value for tok.
func Bearer(tok string) string { return "Bearer " + strings.TrimSpace(tok) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
