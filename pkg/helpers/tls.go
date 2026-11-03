package helpers

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// CertPool returns the system roots plus the PEM certificates in caFiles,
// e.g. a development CA. Paths come from operator config.
func CertPool(caFiles []string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	for _, caFile := range caFiles {
		pemData, err := os.ReadFile(caFile) //nolint:gosec // path comes from operator config
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("ca file %s holds no PEM certificate", caFile)
		}
	}
	return pool, nil
}

// IsHTTPS reports whether raw parses as a URL with the https scheme.
func IsHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}
