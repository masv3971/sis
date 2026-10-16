package helpers

import (
	"strings"
	"testing"
)

type jwtProdMinCommon struct{ Production bool }

type jwtProdMinInner struct {
	Secret string `validate:"jwt_secret_production_min=4"`
}

type jwtProdMinRoot struct {
	Common jwtProdMinCommon
	Inner  struct{ Nested jwtProdMinInner }
}

type jwtProdMinNoCommon struct {
	Secret string `validate:"jwt_secret_production_min=4"`
}

func TestJWTSecretProductionMin(t *testing.T) {
	cases := []struct {
		name   string
		prod   bool
		secret string
		ok     bool
	}{
		{"prod short", true, "abc", false},
		{"prod empty", true, "", false},
		{"prod exact", true, "abcd", true},
		{"dev short", false, "a", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var r jwtProdMinRoot
			r.Common.Production = c.prod
			r.Inner.Nested.Secret = c.secret
			err := Check(r)
			if c.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !c.ok && (err == nil || !strings.Contains(err.Error(), "jwt_secret_production_min")) {
				t.Fatalf("expected jwt_secret_production_min failure, got %v", err)
			}
		})
	}
}

// A root without Common.Production is treated as non-production.
func TestJWTSecretProductionMinWithoutCommon(t *testing.T) {
	if err := Check(jwtProdMinNoCommon{Secret: "a"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
