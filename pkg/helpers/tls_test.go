package helpers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/helpers"
)

func TestRequireHTTPSRedirect(t *testing.T) {
	redirect := func(target string, hops int) error {
		req, err := http.NewRequest(http.MethodPost, target, http.NoBody)
		require.NoError(t, err)
		return helpers.RequireHTTPSRedirect(req, make([]*http.Request, hops))
	}
	assert.NoError(t, redirect("https://collector.example/v1/traces", 1), "https")
	assert.NoError(t, redirect("HTTPS://collector.example/v1/traces", 1), "scheme is case-insensitive")
	assert.ErrorContains(t, redirect("http://collector.example/v1/traces", 1), "https is required", "http")
	assert.ErrorContains(t, redirect("https://collector.example/", 10), "10 redirects", "limit")
}

// TestHTTPSOnlyClientRefusesPlaintextRedirect: a 307 replays the request and
// its body at the new address, so an https endpoint that redirects to http
// would leak the payload.
func TestHTTPSOnlyClientRefusesPlaintextRedirect(t *testing.T) {
	var leaked int
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked++ }))
	t.Cleanup(plain.Close)
	redirector := httptest.NewServer(http.RedirectHandler(plain.URL, http.StatusTemporaryRedirect))
	t.Cleanup(redirector.Close)

	client := helpers.HTTPSOnlyClient(time.Second)
	assert.Equal(t, time.Second, client.Timeout, "timeout")
	_, err := client.Post(redirector.URL, "application/x-protobuf", http.NoBody) //nolint:bodyclose // the request fails
	require.ErrorContains(t, err, "https is required")
	assert.Zero(t, leaked, "the plaintext server received nothing")

	// The control: the default client follows it.
	resp, err := http.Post(redirector.URL, "application/x-protobuf", http.NoBody)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, 1, leaked, "the default client followed the redirect")
}
