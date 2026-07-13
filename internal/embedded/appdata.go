package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var (
	goos        = runtime.GOOS
	userHomeDir = os.UserHomeDir
	getenv      = os.Getenv
	mkdirAll    = os.MkdirAll
)

// AppDataDir returns the OS-appropriate app-data directory for Muesli and
// ensures it exists with 0700 permissions.
func AppDataDir() (string, error) {
	if override := getenv("MUESLI_APPDATA"); override != "" {
		return ensureDir(override)
	}

	var dir string
	switch goos {
	case "darwin":
		home, err := userHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "Library", "Application Support", "Muesli")
	case "windows":
		appData := getenv("APPDATA")
		if appData == "" {
			home, err := userHomeDir()
			if err != nil {
				return "", err
			}
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		dir = filepath.Join(appData, "Muesli")
	default:
		base := getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := userHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
		dir = filepath.Join(base, "muesli")
	}

	return ensureDir(dir)
}

func ensureDir(dir string) (string, error) {
	if err := mkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create app data dir %q: %w", dir, err)
	}
	return dir, nil
}
