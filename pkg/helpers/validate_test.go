package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
	for raw, valid := range cases {
		err := Check(urlRule{URL: raw})
		if valid {
			assert.NoError(t, err, "%q", raw)
		} else {
			assert.ErrorContains(t, err, "httpurl", "%q: expected httpurl failure", raw)
		}
	}
}
