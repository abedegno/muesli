package auth

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "correct horse" || len(hash) < 20 {
		t.Fatalf("hash looks wrong: %q", hash)
	}
	ok, err := VerifyPassword("correct horse", hash)
	if err != nil || !ok {
		t.Fatalf("verify correct: ok=%v err=%v", ok, err)
	}
	bad, err := VerifyPassword("wrong", hash)
	if err != nil || bad {
		t.Fatalf("verify wrong: ok=%v err=%v", bad, err)
	}
}

func TestHashPasswordPHCFormat(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=") {
		t.Fatalf("want PHC format, got %q", h)
	}
	ok, err := VerifyPassword("correct horse", h)
	if err != nil || !ok {
		t.Fatalf("verify own hash: ok=%v err=%v", ok, err)
	}
	bad, _ := VerifyPassword("wrong", h)
	if bad {
		t.Fatal("wrong password verified")
	}
}

func TestVerifyLegacyHash(t *testing.T) {
	// Legacy format: argon2id$<saltB64>$<hashB64> produced by the OLD HashPassword.
	salt := make([]byte, saltLen)
	key := argon2.IDKey([]byte("legacy pw"), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	legacy := "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
	ok, err := VerifyPassword("legacy pw", legacy)
	if err != nil || !ok {
		t.Fatalf("legacy verify: ok=%v err=%v", ok, err)
	}
}

func TestVerifyMalformed(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-hash"); err == nil {
		t.Fatal("expected error on malformed hash")
	}
}
