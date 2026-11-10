package identity

import (
	"strings"
	"testing"
)

var fastParams = Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashAndVerify(t *testing.T) {
	h := NewPasswordHasher(fastParams)
	encoded, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("unexpected encoding: %s", encoded)
	}

	match, rehash, err := h.Verify("correct horse battery", encoded)
	if err != nil || !match || rehash {
		t.Fatalf("match=%v rehash=%v err=%v", match, rehash, err)
	}
	if match, _, _ := h.Verify("wrong password!", encoded); match {
		t.Fatal("wrong password must not match")
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	h := NewPasswordHasher(fastParams)
	a, _ := h.Hash("same password")
	b, _ := h.Hash("same password")
	if a == b {
		t.Fatal("two hashes of the same password must differ")
	}
}

func TestVerifyFlagsOutdatedParameters(t *testing.T) {
	old, _ := NewPasswordHasher(fastParams).Hash("some password")
	stronger := fastParams
	stronger.Time = 2

	match, rehash, err := NewPasswordHasher(stronger).Verify("some password", old)
	if err != nil || !match || !rehash {
		t.Fatalf("old hash must still verify and ask for rehash: match=%v rehash=%v err=%v", match, rehash, err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	h := NewPasswordHasher(fastParams)
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=18$m=1,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=19$m=x$c2FsdA$a2V5"} {
		if _, _, err := h.Verify("x", bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
