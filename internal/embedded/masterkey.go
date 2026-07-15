package embedded

import (
	"crypto/rand"
	"encoding/base64"
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
	if b, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(b)); key != "" {
			return key, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("embedded master key: generate: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(raw)
	// 0600: the key must not be world-readable. AppDataDir already ensures the
	// directory exists with 0700.
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("embedded master key: persist: %w", err)
	}
	return key, nil
}
