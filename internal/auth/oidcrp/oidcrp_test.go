package oidcrp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestNewDisabledWithoutIssuer checks an empty issuer disables the RP.
func TestNewDisabledWithoutIssuer(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	c, err := New(context.Background(), &model.Cfg{}, log)
	if c != nil || err != nil {
		t.Fatalf("New() = %v, %v; want nil, nil", c, err)
	}
}

// TestNewBoundsDiscovery checks a provider that accepts the connection but
// never answers discovery makes New fail after discoveryTimeout instead of
// hanging startup.
func TestNewBoundsDiscovery(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	old := discoveryTimeout
	discoveryTimeout = 100 * time.Millisecond
	t.Cleanup(func() { discoveryTimeout = old })

	cfg := &model.Cfg{}
	cfg.SIS.Auth.OIDCRP.IssuerURL = srv.URL
	log := logger.NewWithWriter("test", false, "error", io.Discard)

	done := make(chan error, 1)
	go func() {
		_, err := New(context.Background(), cfg, log)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected discovery to fail on a stalled provider")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("New did not return: discovery is unbounded")
	}
}

// testProvider is a local OIDC provider: discovery, a JWKS endpoint serving
// key's public half, and a token endpoint answering with whatever ID token
// idToken holds. Tokens are signed with stdlib crypto so the test needs no
// JOSE dependency.
type testProvider struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	idToken string
}

const testKID = "k1"

func newTestProvider(t *testing.T) *testProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	p := &testProvider{key: key}
	mux := http.NewServeMux()
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                p.srv.URL,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": testKID,
			"n": b64(key.N.Bytes()),
			"e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": p.idToken})
	})
	return p
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// sign builds an RS256 compact JWS over claims with key.
func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": testKID})
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	input := b64(header) + "." + b64(payload)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + b64(sig)
}

// TestExchangeVerifiesIDToken drives Exchange through a full code exchange
// and ID-token verification. Removing the nonce, signature, audience or
// expiry check from Exchange (or its verifier config) fails a case here.
func TestExchangeVerifiesIDToken(t *testing.T) {
	p := newTestProvider(t)
	cfg := &model.Cfg{}
	cfg.SIS.Auth.OIDCRP.IssuerURL = p.srv.URL
	cfg.SIS.Auth.OIDCRP.ClientID = "sis"
	cfg.SIS.Auth.OIDCRP.ClientSecret = "secret"
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	c, err := New(context.Background(), cfg, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Now()
	valid := func() map[string]any {
		return map[string]any{
			"iss": p.srv.URL, "sub": "user-1", "aud": "sis", "nonce": "n1",
			"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			"email": "ada@uni.edu", "name": "Ada",
		}
	}
	with := func(k string, v any) map[string]any { m := valid(); m[k] = v; return m }

	cases := []struct {
		name    string
		idToken string
		nonce   string
		wantErr bool
	}{
		{"valid", sign(t, p.key, valid()), "n1", false},
		{"nonce mismatch", sign(t, p.key, valid()), "other", true},
		{"missing nonce", sign(t, p.key, with("nonce", "")), "n1", true},
		{"invalid signature", sign(t, otherKey, valid()), "n1", true},
		{"wrong audience", sign(t, p.key, with("aud", "someone-else")), "n1", true},
		{"wrong issuer", sign(t, p.key, with("iss", "https://evil.example")), "n1", true},
		{"expired", sign(t, p.key, with("exp", now.Add(-time.Hour).Unix())), "n1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p.idToken = tc.idToken
			claims, err := c.Exchange(context.Background(), "code", tc.nonce)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got claims %+v", claims)
				}
				return
			}
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			want := Claims{Issuer: p.srv.URL, Subject: "user-1", Email: "ada@uni.edu", Name: "Ada"}
			if *claims != want {
				t.Fatalf("claims = %+v, want %+v", *claims, want)
			}
		})
	}
}

// TestExchangeBoundsStalledTokenEndpoint checks the login callback cannot
// hang on a provider that serves discovery but accepts the token request and
// never answers: Exchange must fail after exchangeTimeout even though the
// caller's context has no deadline.
func TestExchangeBoundsStalledTokenEndpoint(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/token", func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release); srv.Close() })

	old := exchangeTimeout
	exchangeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { exchangeTimeout = old })

	cfg := &model.Cfg{}
	cfg.SIS.Auth.OIDCRP.IssuerURL = srv.URL
	cfg.SIS.Auth.OIDCRP.ClientID = "sis"
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	c, err := New(context.Background(), cfg, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Exchange(context.Background(), "code", "nonce")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Exchange to fail on a stalled token endpoint")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exchange did not return: token request is unbounded")
	}
}
