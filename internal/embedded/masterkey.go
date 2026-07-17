package embedded

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// masterKeyFile is where the auto-generated embedded master key is persisted,
// under the app data dir.
const masterKeyFile = "master.key"

// EnsureMasterKey returns the embedded app's master key, generating and
// persisting a fresh 32-byte base64 key in the app data dir on first run and
// reusing it on later launches. Embedded/desktop mode has no operator to supply
// MUESLI_MASTER_KEY, so it manages one itself; the key must stay stable across
// launches or previously-encrypted data becomes unreadable.
func EnsureMasterKey() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", fmt.Errorf("embedded master key: %w", err)
	}
	path := filepath.Join(dir, masterKeyFile)
	if _, err := os.Stat(path); err == nil {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("embedded master key: existing key file %q is unreadable; refusing to overwrite (previously-encrypted data would be lost); restore it from backup or remove it to start fresh: %w", path, err)
		}
		key := strings.TrimSpace(string(b))
		if key == "" {
			return "", fmt.Errorf("embedded master key: existing key file %q is empty or unreadable; refusing to overwrite (previously-encrypted data would be lost); restore it from backup or remove it to start fresh", path)
		}
		if err := validateMasterKey(key); err != nil {
			return "", fmt.Errorf("embedded master key: existing key file %q is malformed; refusing to overwrite (previously-encrypted data would be lost); restore it from backup or remove it to start fresh: %w", path, err)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("embedded master key: stat %q: %w", path, err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("embedded master key: generate: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(raw)
	if err := persistMasterKey(path, key); err != nil {
		return "", err
	}
	return key, nil
}

func validateMasterKey(key string) error {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("master key is not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return fmt.Errorf("master key must decode to 32 bytes, got %d", len(raw))
	}
	return nil
}

func persistMasterKey(path, key string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, masterKeyFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("embedded master key: persist temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.WriteString(key + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("embedded master key: persist temp write: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("embedded master key: persist temp chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("embedded master key: persist temp close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("embedded master key: persist: %w", err)
	}
	return nil
}
