package token_test

import (
	"testing"
	"time"

	"github.com/SUNET/sis/internal/auth/token"
)

func TestIssueAndParse(t *testing.T) {
	iss := token.NewIssuer("secret", "sis", time.Hour)

	signed, exp, err := iss.Issue("client-1", token.KindClient, nil, []string{"sis.read"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if time.Until(exp) <= 0 {
		t.Fatal("expected a future expiry")
	}

	claims, err := iss.Parse(signed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != "client-1" || claims.Kind != token.KindClient {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if len(claims.Scopes) != 1 || claims.Scopes[0] != "sis.read" {
		t.Fatalf("expected scope sis.read, got %v", claims.Scopes)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	signed, _, _ := token.NewIssuer("secret-a", "sis", time.Hour).
		Issue("u", token.KindUser, []string{"admin"}, nil)

	if _, err := token.NewIssuer("secret-b", "sis", time.Hour).Parse(signed); err == nil {
		t.Fatal("expected parse to fail under a different secret")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	signed, _, _ := token.NewIssuer("secret", "sis", -time.Minute).
		Issue("u", token.KindUser, nil, nil)

	if _, err := token.NewIssuer("secret", "sis", time.Hour).Parse(signed); err == nil {
		t.Fatal("expected parse to fail for an expired token")
	}
}
