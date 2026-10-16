package httpserver

import "testing"

func TestIsAPIPath(t *testing.T) {
	cases := map[string]bool{
		"/api":            true,
		"/api/":           true,
		"/api/v1/persons": true,
		"/oeapi":          true,
		"/eduapi":         true,
		"/auth":           true,
		"/oauth":          true,
		"/healthz":        true,
		"/swagger":        true,
		"/swagger/x":      true,
		"/apix":           false,
		"/authors":        false,
		"/":               false,
		"/persons/42":     false,
	}
	for path, want := range cases {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", path, got, want)
		}
	}
}
