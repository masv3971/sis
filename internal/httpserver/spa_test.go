package httpserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAPIPath(t *testing.T) {
	cases := map[string]bool{
		"/api":            true,
		"/api/":           true,
		"/api/v1/persons": true,
		"/oeapi":          true,
		"/eduapi":         true,
		"/auth":           true,
		"/auth/config":    true,
		"/healthz":        true,
		"/swagger":        true,
		"/swagger/x":      true,
		"/apix":           false,
		"/authors":        false,
		"/":               false,
		"/persons/42":     false,
	}
	for path, want := range cases {
		assert.Equal(t, want, isAPIPath(path), "isAPIPath(%q)", path)
	}
}
