package auth

import "testing"

func TestGenerateToken(t *testing.T) {
	raw, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(raw) < 20 {
		t.Fatalf("raw token too short: %q", raw)
	}
	if hash != HashToken(raw) {
		t.Fatal("HashToken not stable / mismatched")
	}
	raw2, _, _ := GenerateToken()
	if raw == raw2 {
		t.Fatal("tokens not unique")
	}
}
