//go:build vcintegration

// Package vcintegration issues a credential from SIS through SUNET/vc's
// data_sources.external_api with a remote of type eduapi, playing the wallet.
//
// It runs against the stack of docker-compose.yaml plus docker-compose.vc.yaml
// (make vc-integration) and skips unless SIS_VC_APIGW_URL is set.
package vcintegration

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/eduapi"
)

// These are what developer_tools/vc/config.yaml and realm.json configure.
const (
	apigwPublicURL    = "https://localhost:18083"
	mellonPublicURL   = "https://localhost:9443"
	studentsPublicURL = "https://localhost:9444"
	walletClientID    = "sis-test-wallet"
	walletRedirect    = "http://localhost:3000"
	credentialScope   = "sis_enrollment"
	sisPersonID       = "liu:anna.andersson"
	unknownPersonID   = "liu:nobody"
	password          = "password"
)

type stack struct {
	apigw, mellon, students, sis string
	http                         *http.Client
}

func newStack(t *testing.T) *stack {
	t.Helper()
	apigw := os.Getenv("SIS_VC_APIGW_URL")
	if apigw == "" {
		t.Skip("set SIS_VC_APIGW_URL (make vc-integration) to run against the SUNET/vc stack")
	}
	caFile := envOr("SIS_VC_ROOT_CA", "../../developer_tools/pki/rootCA.crt")
	pem, err := os.ReadFile(caFile)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pem), "no certificate in %s", caFile)
	return &stack{
		apigw:    strings.TrimRight(apigw, "/"),
		mellon:   strings.TrimRight(envOr("SIS_VC_MELLON_URL", mellonPublicURL), "/"),
		students: strings.TrimRight(envOr("SIS_VC_STUDENTS_URL", studentsPublicURL), "/"),
		sis:      strings.TrimRight(envOr("SIS_VC_SIS_URL", "https://localhost:8080"), "/"),
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// reach turns the public URL a service advertises into one the test can
// connect to; the public form stays the one DPoP proofs are bound to.
func (s *stack) reach(u string) string {
	u = strings.Replace(u, apigwPublicURL, s.apigw, 1)
	u = strings.Replace(u, studentsPublicURL, s.students, 1)
	return strings.Replace(u, mellonPublicURL, s.mellon, 1)
}

func (s *stack) public(u string) string {
	return strings.Replace(u, s.apigw, apigwPublicURL, 1)
}

// sisGet reads one Edu-API resource from SIS with a token for the sunet-vc client.
func (s *stack) sisGet(t *testing.T, token, path string, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.sis+eduapi.BasePath+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.http.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "SIS %s: %s", path, body)
	require.NoError(t, json.Unmarshal(body, out))
}

func (s *stack) sisToken(t *testing.T) string {
	t.Helper()
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"sis.read"}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		s.mellon+"/realms/sis/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("sunet-vc", "sunet-vc-secret")
	resp, err := s.http.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "mellon token: %s", body)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(body, &tok))
	return tok.AccessToken
}

// TestIssueFromSIS issues sis_enrollment for a SIS student and compares every
// claim with what SIS's own Edu-API returns for that person.
func TestIssueFromSIS(t *testing.T) {
	s := newStack(t)

	claims, failure := s.issue(t, "anna")
	if failure != "" {
		skipIfPreFinalVC(t, failure)
		require.Fail(t, "issuance failed", failure)
	}

	token := s.sisToken(t)
	var person eduapi.Person
	s.sisGet(t, token, "/persons/"+url.PathEscape(sisPersonID), &person)
	require.NotNil(t, person.LegalName)

	assert.Equal(t, person.LegalName.GivenName, claims["given_name"])
	assert.Equal(t, person.LegalName.FamilyName, claims["family_name"])
	assert.Equal(t, person.DateOfBirth, claims["birth_date"])

	var enrollments []eduapi.Enrollment
	s.sisGet(t, token, "/enrollments?limit=500", &enrollments)
	var offeringID string
	for _, e := range enrollments {
		if e.Person == sisPersonID && e.IsActive {
			offeringID = e.EducationOffering
			break
		}
	}
	require.NotEmpty(t, offeringID, "SIS has no active enrollment for %s", sisPersonID)

	var offering eduapi.CourseOffering
	s.sisGet(t, token, "/courseOfferings/"+url.PathEscape(offeringID), &offering)
	require.NotEmpty(t, offering.Title)
	assert.Equal(t, offering.Title[0].Value, claims["course_title"])

	var org eduapi.Organization
	s.sisGet(t, token, "/organizations/"+url.PathEscape(offering.Organization), &org)
	require.NotEmpty(t, org.Name)
	assert.Equal(t, org.Name[0].Value, claims["institution_name"])
}

// A student SIS does not know is never issued a credential. Login and the OIDC
// callback must succeed, so the stop is vc's lookup of the person in SIS.
func TestUnknownPersonIsNotIssued(t *testing.T) {
	s := newStack(t)

	// SIS itself must answer 404 for this person, or the test proves nothing.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		s.sis+eduapi.BasePath+"/persons/"+url.PathEscape(unknownPersonID), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+s.sisToken(t))
	resp, err := s.http.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "SIS person lookup: %s", readAll(resp))

	claims, failure := s.issue(t, "nobody")
	skipIfPreFinalVC(t, failure)
	assert.Nil(t, claims)
	assert.NotEmpty(t, failure, "a person SIS has no record of must not be issued a credential")
}

// skipIfPreFinalVC skips when SIS_VC_SKIP_PRE_FINAL=1 and issuance stopped in
// vc, for a vc build whose Edu-API client still asks for OneRoster paths, which
// SIS does not serve (it speaks Edu-API v1.0 Final). vc redacts that cause from
// its responses, so the build cannot be detected from the failure text.
func skipIfPreFinalVC(t *testing.T, failure string) {
	t.Helper()
	if failure != "" && os.Getenv("SIS_VC_SKIP_PRE_FINAL") == "1" {
		t.Skip("SIS_VC_SKIP_PRE_FINAL=1 and issuance stopped in vc: " + failure)
	}
}

// issue runs the wallet side of OpenID4VCI as user (a student IdP account) and
// returns the claims of the issued SD-JWT, or why issuance stopped.
func (s *stack) issue(t *testing.T, user string) (map[string]any, string) {
	t.Helper()
	ctx := t.Context()

	var issuerMeta struct {
		CredentialIssuer   string `json:"credential_issuer"`
		CredentialEndpoint string `json:"credential_endpoint"`
	}
	s.getJSON(t, s.apigw+"/.well-known/openid-credential-issuer", &issuerMeta)
	var oauthMeta struct {
		TokenEndpoint string `json:"token_endpoint"`
		PAREndpoint   string `json:"pushed_authorization_request_endpoint"`
		AuthEndpoint  string `json:"authorization_endpoint"`
	}
	s.getJSON(t, s.apigw+"/.well-known/oauth-authorization-server", &oauthMeta)

	verifier := uuid.NewString() + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	par := url.Values{
		"response_type":         {"code"},
		"client_id":             {walletClientID},
		"redirect_uri":          {walletRedirect},
		"state":                 {uuid.NewString()},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"scope":                 {credentialScope},
	}
	resp, err := s.http.PostForm(s.reach(oauthMeta.PAREndpoint), par)
	require.NoError(t, err)
	body := readAll(resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "PAR: %s", body)
	var parResp struct {
		RequestURI string `json:"request_uri"`
	}
	require.NoError(t, json.Unmarshal(body, &parResp))

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	follow := &http.Client{Transport: s.http.Transport, Jar: jar, Timeout: s.http.Timeout}
	manual := &http.Client{
		Transport:     s.http.Transport,
		Jar:           jar,
		Timeout:       s.http.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// /authorize sets the session and lands on the consent page, which links to the OIDC login.
	resp, err = follow.Get(s.reach(oauthMeta.AuthEndpoint) + "?client_id=" + url.QueryEscape(walletClientID) +
		"&request_uri=" + url.QueryEscape(parResp.RequestURI))
	require.NoError(t, err)
	consent := readAll(resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, "authorize: %s", consent)
	loginURL := dataAttr(string(consent), "data-redirect-url")
	require.NotEmpty(t, loginURL, "consent page has no data-redirect-url")

	callback := s.studentLogin(t, manual, s.reach(loginURL), user)

	resp, err = manual.Get(s.reach(callback))
	require.NoError(t, err)
	body = readAll(resp)
	require.Less(t, resp.StatusCode, http.StatusBadRequest, "oidc callback: %s", body)

	// vc fetches the document from SIS when the consent page is visited.
	if loc := resp.Header.Get("Location"); loc != "" {
		callbackURL, err := url.Parse(s.reach(callback))
		require.NoError(t, err)
		next, err := callbackURL.Parse(loc)
		require.NoError(t, err)
		resp, err = follow.Get(s.reach(next.String()))
		require.NoError(t, err)
		body = readAll(resp)
		if resp.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Sprintf("consent %d: %s", resp.StatusCode, body)
		}
	}

	resp, err = manual.Get(s.apigw + "/user/lookup")
	require.NoError(t, err)
	body = readAll(resp)
	code := codeFrom(resp.Header.Get("Location"))
	if code == "" {
		var lookup struct {
			RedirectURL string `json:"redirect_url"`
		}
		if json.Unmarshal(body, &lookup) == nil {
			code = codeFrom(lookup.RedirectURL)
		}
	}
	if code == "" {
		return nil, fmt.Sprintf("user/lookup %d: %s", resp.StatusCode, body)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tokenURL := s.reach(oauthMeta.TokenEndpoint)
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {walletRedirect},
		"client_id":     {walletClientID},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(tokenForm.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", signJWT(t, key, "dpop+jwt", map[string]any{
		"jti": uuid.NewString(), "htm": http.MethodPost, "htu": s.public(tokenURL), "iat": time.Now().Unix(),
	}))
	resp, err = s.http.Do(req)
	require.NoError(t, err)
	body = readAll(resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, "token: %s", body)
	var tok struct {
		AccessToken string `json:"access_token"`
		CNonce      string `json:"c_nonce"`
	}
	require.NoError(t, json.Unmarshal(body, &tok))

	credURL := s.reach(issuerMeta.CredentialEndpoint)
	credBody, err := json.Marshal(map[string]any{
		"credential_configuration_id": credentialScope,
		"proofs": map[string]any{"jwt": []string{signJWT(t, key, "openid4vci-proof+jwt", map[string]any{
			"aud": issuerMeta.CredentialIssuer, "iat": time.Now().Unix(), "iss": walletClientID, "nonce": tok.CNonce,
		})}},
	})
	require.NoError(t, err)
	ath := sha256.Sum256([]byte(tok.AccessToken))
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, credURL, bytes.NewReader(credBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DPoP "+tok.AccessToken)
	req.Header.Set("DPoP", signJWT(t, key, "dpop+jwt", map[string]any{
		"jti": uuid.NewString(), "htm": http.MethodPost, "htu": s.public(credURL), "iat": time.Now().Unix(),
		"ath": base64.RawURLEncoding.EncodeToString(ath[:]),
	}))
	resp, err = s.http.Do(req)
	require.NoError(t, err)
	body = readAll(resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, "credential: %s", body)
	var cred struct {
		Credentials []struct {
			Credential string `json:"credential"`
		} `json:"credentials"`
	}
	require.NoError(t, json.Unmarshal(body, &cred))
	require.NotEmpty(t, cred.Credentials)
	return sdJWTClaims(t, cred.Credentials[0].Credential), ""
}

var (
	formAction  = regexp.MustCompile(`<form[^>]+action="([^"]+)"`)
	hiddenField = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)"`)
)

// studentLogin signs the student in at the student IdP (mellon, standing in for
// eduID) and returns the redirect back to apigw. A failed login fails the test.
func (s *stack) studentLogin(t *testing.T, c *http.Client, authURL, user string) string {
	t.Helper()
	current := authURL
	var page []byte
	// /auth redirects to the login page.
	for range 3 {
		resp, err := c.Get(current)
		require.NoError(t, err)
		page = readAll(resp)
		loc := resp.Header.Get("Location")
		if resp.StatusCode != http.StatusFound || loc == "" {
			require.Equal(t, http.StatusOK, resp.StatusCode, "student IdP: %s", page)
			break
		}
		next, err := url.Parse(current)
		require.NoError(t, err)
		ref, err := url.Parse(loc)
		require.NoError(t, err)
		current = s.reach(next.ResolveReference(ref).String())
	}

	m := formAction.FindSubmatch(page)
	require.NotNil(t, m, "no login form from the student IdP: %s", page)
	form := url.Values{"username": {user}, "password": {password}}
	for _, f := range hiddenField.FindAllSubmatch(page, -1) {
		form.Set(string(f[1]), html.UnescapeString(string(f[2])))
	}
	base, err := url.Parse(current)
	require.NoError(t, err)
	ref, err := url.Parse(html.UnescapeString(string(m[1])))
	require.NoError(t, err)

	resp, err := c.PostForm(base.ResolveReference(ref).String(), form)
	require.NoError(t, err)
	body := readAll(resp)
	loc := resp.Header.Get("Location")
	require.True(t, resp.StatusCode == http.StatusFound && loc != "", "student login %d: %s", resp.StatusCode, body)
	return loc
}

func (s *stack) getJSON(t *testing.T, u string, out any) {
	t.Helper()
	resp, err := s.http.Get(u)
	require.NoError(t, err)
	body := readAll(resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s: %s", u, body)
	require.NoError(t, json.Unmarshal(body, out))
}

func readAll(resp *http.Response) []byte {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return body
}

func dataAttr(page, name string) string {
	re := regexp.MustCompile(name + `="([^"]*)"`)
	m := re.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

func codeFrom(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get("code")
}

// sdJWTClaims merges the signed claims of an SD-JWT with its disclosed ones.
// The issuer's signature is vc's to test; this only reads the claims.
func sdJWTClaims(t *testing.T, sdjwt string) map[string]any {
	t.Helper()
	parts := strings.Split(sdjwt, "~")
	jwtParts := strings.Split(parts[0], ".")
	require.Len(t, jwtParts, 3, "not a JWT: %.40s", parts[0])
	claims := map[string]any{}
	raw, err := base64.RawURLEncoding.DecodeString(jwtParts[1])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &claims))
	for _, d := range parts[1:] {
		if d == "" {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(d)
		require.NoError(t, err)
		var disclosure []any
		require.NoError(t, json.Unmarshal(raw, &disclosure))
		if len(disclosure) == 3 {
			if name, ok := disclosure[1].(string); ok {
				claims[name] = disclosure[2]
			}
		}
	}
	return claims
}

// signJWT signs claims with ES256 and embeds the public key as the jwk header,
// the shape both the DPoP proof and the OpenID4VCI key proof take.
func signJWT(t *testing.T, key *ecdsa.PrivateKey, typ string, claims map[string]any) string {
	t.Helper()
	pad := func(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.FillBytes(make([]byte, 32))) }
	header := map[string]any{
		"typ": typ, "alg": "ES256",
		"jwk": map[string]string{"kty": "EC", "crv": "P-256", "x": pad(key.X), "y": pad(key.Y)},
	}
	enc := func(v any) string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	require.NoError(t, err)
	return signing + "." + base64.RawURLEncoding.EncodeToString(append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...))
}
