package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKeyB64(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := New(newKeyB64(t))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	plain := []byte(`{"api_key":"sk-secret"}`)

	ct, err := c.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if ct == string(plain) || ct == "" {
		t.Fatalf("ciphertext looks wrong: %q", ct)
	}

	got, err := c.Open(ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestSealIsNondeterministic(t *testing.T) {
	c, _ := New(newKeyB64(t))
	a, _ := c.Seal([]byte("same"))
	b, _ := c.Seal([]byte("same"))
	if a == b {
		t.Fatal("two seals of the same plaintext must differ (random nonce)")
	}
}

func TestOpenRejectsTamperedAndWrongKey(t *testing.T) {
	c, _ := New(newKeyB64(t))
	ct, _ := c.Seal([]byte("secret"))

	// Tampered ciphertext fails authentication.
	bad := []byte(ct)
	bad[len(bad)-1] ^= 0xff
	if _, err := c.Open(string(bad)); err == nil {
		t.Fatal("tampered ciphertext should not open")
	}

	// A different key cannot open it.
	other, _ := New(newKeyB64(t))
	if _, err := other.Open(ct); err == nil {
		t.Fatal("wrong key should not open")
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("empty key should error")
	}
	if _, err := New(base64.StdEncoding.EncodeToString(make([]byte, 16))); err == nil {
		t.Fatal("16-byte key should error (need 32)")
	}
	if _, err := New("not-base64!!!"); err == nil {
		t.Fatal("non-base64 key should error")
	}
}
