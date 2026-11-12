package helpers

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// CertPool returns the system roots plus the PEM certificates in caFiles,
// e.g. a development CA. Paths come from operator config.
func CertPool(caFiles []string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	for _, caFile := range caFiles {
		pemData, err := os.ReadFile(caFile) // #nosec G304 -- path comes from operator config
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("ca file %s holds no PEM certificate", caFile)
		}
	}
	return pool, nil
}

// RequireHTTPSRedirect is an http.Client CheckRedirect that refuses a
// redirect to anything but https, so a 307 or 308 cannot replay the request
// and its body over plaintext. Like the default policy it stops after 10.
func RequireHTTPSRedirect(req *http.Request, via []*http.Request) error {
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("redirect to %s: https is required in production", req.URL.Redacted())
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

// HTTPSOnlyClient returns a client applying RequireHTTPSRedirect. Its
// transport clones the default one, so proxy settings from the environment
// still apply.
func HTTPSOnlyClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     http.DefaultTransport.(*http.Transport).Clone(),
		Timeout:       timeout,
		CheckRedirect: RequireHTTPSRedirect,
	}
}

// IsHTTPS reports whether raw parses as a URL with the https scheme.
func IsHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}
