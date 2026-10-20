package helpers

import (
	"strings"
	"testing"
)

type urlRule struct {
	URL string `validate:"omitempty,httpurl"`
}

func TestHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"":                           true,
		"https://idp.example/realms": true,
		"http://localhost:9080/x":    true,
		"ftp://idp.example":          false,
		"idp.example":                false,
		"https://":                   false,
	}
	for raw, ok := range cases {
		err := Check(urlRule{URL: raw})
		if ok && err != nil {
			t.Errorf("%q: unexpected error: %v", raw, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "httpurl")) {
			t.Errorf("%q: expected httpurl failure, got %v", raw, err)
		}
	}
}
