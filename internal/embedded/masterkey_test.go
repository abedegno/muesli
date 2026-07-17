package embedded

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureMasterKeyGeneratesPersistsAndIsStable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUESLI_APPDATA", dir)

	k1, err := EnsureMasterKey()
	if err != nil {
		t.Fatalf("EnsureMasterKey: %v", err)
	}
	if k1 == "" {
		t.Fatal("generated an empty master key")
	}
	// A valid RequireMasterKey key is 32 raw bytes, base64-encoded.
	if raw, err := base64.StdEncoding.DecodeString(k1); err != nil || len(raw) != 32 {
		t.Fatalf("key is not 32-byte base64: err=%v len=%d", err, len(raw))
	}
	// It must be persisted with restrictive perms.
	fi, err := os.Stat(filepath.Join(dir, masterKeyFile))
	if err != nil {
		t.Fatalf("key not persisted: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key perms = %o, want 600", fi.Mode().Perm())
	}
	// A later launch must reuse the same key (or old data becomes unreadable).
	k2, err := EnsureMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if k2 != k1 {
		t.Fatalf("key changed across calls: %q vs %q", k1, k2)
	}
}

func TestEnsureMasterKey_EmptyFileErrorsInsteadOfRegenerating(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "whitespace", content: "   \n\t\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("MUESLI_APPDATA", dir)

			path := filepath.Join(dir, masterKeyFile)
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("prewrite master.key: %v", err)
			}

			k, err := EnsureMasterKey()
			if err == nil {
				t.Fatalf("EnsureMasterKey returned key %q, want error", k)
			}
			if got := err.Error(); got == "" {
				t.Fatal("EnsureMasterKey returned a nil/empty error")
			}

			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read master.key after failure: %v", readErr)
			}
			if string(got) != tc.content {
				t.Fatalf("master.key changed: got %q want %q", string(got), tc.content)
			}
		})
	}
}

func TestEnsureMasterKey_MissingFileStillGenerates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUESLI_APPDATA", dir)

	k, err := EnsureMasterKey()
	if err != nil {
		t.Fatalf("EnsureMasterKey: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		t.Fatalf("generated key is not valid base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("generated key decodes to %d bytes, want 32", len(raw))
	}

	fi, err := os.Stat(filepath.Join(dir, masterKeyFile))
	if err != nil {
		t.Fatalf("key not persisted: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key perms = %o, want 600", fi.Mode().Perm())
	}
}

func TestEnsureMasterKey_ExistingValidKeyPreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUESLI_APPDATA", dir)

	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	key := base64.StdEncoding.EncodeToString(raw)
	path := filepath.Join(dir, masterKeyFile)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		t.Fatalf("prewrite master.key: %v", err)
	}

	k, err := EnsureMasterKey()
	if err != nil {
		t.Fatalf("EnsureMasterKey: %v", err)
	}
	if k != key {
		t.Fatalf("EnsureMasterKey returned %q, want %q", k, key)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read master.key: %v", err)
	}
	if string(got) != key {
		t.Fatalf("master.key changed: got %q want %q", string(got), key)
	}
}

func TestEnsureMasterKey_ExistingMalformedKeyRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUESLI_APPDATA", dir)

	path := filepath.Join(dir, masterKeyFile)
	if err := os.WriteFile(path, []byte("not-base64"), 0o600); err != nil {
		t.Fatalf("prewrite master.key: %v", err)
	}

	_, err := EnsureMasterKey()
	if err == nil {
		t.Fatal("EnsureMasterKey returned nil error for malformed key")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error %q does not refuse overwrite", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read master.key after failure: %v", readErr)
	}
	if string(got) != "not-base64" {
		t.Fatalf("master.key changed: got %q want %q", string(got), "not-base64")
	}
}
