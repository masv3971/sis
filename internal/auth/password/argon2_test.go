package password_test

import (
	"testing"

	"github.com/SUNET/sis/internal/auth/password"
)

func TestHashVerify(t *testing.T) {
	h, err := password.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, err := password.Verify("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("expected verify to succeed, got ok=%v err=%v", ok, err)
	}

	ok, err = password.Verify("wrong password", h)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if ok {
		t.Fatal("expected verify to fail for wrong password")
	}
}

func TestHashIsSalted(t *testing.T) {
	h1, _ := password.Hash("same")
	h2, _ := password.Hash("same")
	if h1 == h2 {
		t.Fatal("expected distinct hashes for the same password (random salt)")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if _, err := password.Verify("x", "not-a-phc-string"); err == nil {
		t.Fatal("expected error for malformed hash")
	}
}
