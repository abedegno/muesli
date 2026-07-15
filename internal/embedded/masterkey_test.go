package embedded

import (
	"encoding/base64"
	"os"
	"path/filepath"
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
