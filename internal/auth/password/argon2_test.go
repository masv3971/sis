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

// TestVerifyRejectsBadParams covers parameters that would make argon2.IDKey
// panic (t=0, p=0) or allocate unbounded memory (huge m).
func TestVerifyRejectsBadParams(t *testing.T) {
	const salt, key = "c2FsdHNhbHRzYWx0c2FsdA", "a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U"
	for _, params := range []string{
		"m=65536,t=0,p=4",
		"m=65536,t=1,p=0",
		"m=4194304,t=1,p=4",
		"m=1,t=1,p=4",
		"m=65536,t=99,p=4",
	} {
		h := "$argon2id$v=19$" + params + "$" + salt + "$" + key
		ok, err := password.Verify("x", h)
		if err == nil || ok {
			t.Errorf("%s: expected rejection, got ok=%v err=%v", params, ok, err)
		}
	}
}
