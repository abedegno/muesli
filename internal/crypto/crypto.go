// Package crypto provides authenticated symmetric encryption (AES-256-GCM) for
// secrets stored at rest, keyed by a base64-encoded 32-byte master key.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Crypto seals and opens secrets with a fixed AES-256-GCM key.
type Crypto struct {
	aead cipher.AEAD
}

// New builds a Crypto from a base64-encoded 32-byte key (e.g. MUESLI_MASTER_KEY,
// generated via `openssl rand -base64 32`).
func New(masterKeyB64 string) (*Crypto, error) {
	if masterKeyB64 == "" {
		return nil, fmt.Errorf("master key is empty")
	}
	key, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("master key is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must decode to 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Crypto{aead: aead}, nil
}

// Seal encrypts plaintext and returns a base64 string of nonce||ciphertext.
func (c *Crypto) Seal(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal, authenticating before returning the plaintext.
func (c *Crypto) Open(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("ciphertext is not valid base64: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	return c.aead.Open(nil, nonce, ct, nil)
}
