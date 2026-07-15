package embedded

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAppDataDir(t *testing.T) {
	origGOOS := goos
	origUserHomeDir := userHomeDir
	origGetenv := getenv
	origMkdirAll := mkdirAll
	t.Cleanup(func() {
		goos = origGOOS
		userHomeDir = origUserHomeDir
		getenv = origGetenv
		mkdirAll = origMkdirAll
	})

	tests := []struct {
		name    string
		goos    string
		env     func(home string) map[string]string
		wantDir func(home string) string
	}{
		{
			name: "darwin",
			goos: "darwin",
			wantDir: func(home string) string {
				return filepath.Join(home, "Library", "Application Support", "Muesli")
			},
		},
		{
			name: "windows",
			goos: "windows",
			env: func(home string) map[string]string {
				return map[string]string{"APPDATA": filepath.Join(home, "AppData", "Roaming")}
			},
			wantDir: func(home string) string {
				return filepath.Join(home, "AppData", "Roaming", "Muesli")
			},
		},
		{
			name: "linux",
			goos: "linux",
			env: func(home string) map[string]string {
				return map[string]string{"XDG_DATA_HOME": filepath.Join(home, ".local", "share")}
			},
			wantDir: func(home string) string {
				return filepath.Join(home, ".local", "share", "muesli")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			goos = tc.goos
			userHomeDir = func() (string, error) { return home, nil }
			env := map[string]string{}
			if tc.env != nil {
				env = tc.env(home)
			}
			getenv = func(key string) string {
				if key == "MUESLI_APPDATA" {
					return ""
				}
				if v, ok := env[key]; ok {
					return v
				}
				return ""
			}

			got, err := AppDataDir()
			if err != nil {
				t.Fatalf("AppDataDir() error: %v", err)
			}
			want := tc.wantDir(home)
			if got != want {
				t.Fatalf("AppDataDir() = %q, want %q", got, want)
			}

			info, err := os.Stat(got)
			if err != nil {
				t.Fatalf("Stat(%q) error: %v", got, err)
			}
			if !info.IsDir() {
				t.Fatalf("%q is not a directory", got)
			}
			if runtime.GOOS != "windows" {
				if perm := info.Mode().Perm(); perm != 0o700 {
					t.Fatalf("mode = %v, want 0700", perm)
				}
			}
		})
	}
}

func TestAppDataDirOverride(t *testing.T) {
	origGOOS := goos
	origUserHomeDir := userHomeDir
	origGetenv := getenv
	origMkdirAll := mkdirAll
	t.Cleanup(func() {
		goos = origGOOS
		userHomeDir = origUserHomeDir
		getenv = origGetenv
		mkdirAll = origMkdirAll
	})

	override := t.TempDir()
	home := t.TempDir()
	goos = "darwin"
	userHomeDir = func() (string, error) { return home, nil }
	getenv = func(key string) string {
		switch key {
		case "MUESLI_APPDATA":
			return override
		case "APPDATA":
			return "ignored"
		case "XDG_DATA_HOME":
			return "ignored"
		default:
			return ""
		}
	}

	got, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() error: %v", err)
	}
	if got != override {
		t.Fatalf("AppDataDir() = %q, want override %q", got, override)
	}
	if info, err := os.Stat(got); err != nil {
		t.Fatalf("Stat(%q) error: %v", got, err)
	} else if !info.IsDir() {
		t.Fatalf("%q is not a directory", got)
	}
}

func TestStorageDir(t *testing.T) {
	origGOOS := goos
	origUserHomeDir := userHomeDir
	origGetenv := getenv
	origMkdirAll := mkdirAll
	t.Cleanup(func() {
		goos = origGOOS
		userHomeDir = origUserHomeDir
		getenv = origGetenv
		mkdirAll = origMkdirAll
	})

	home := t.TempDir()
	goos = "linux"
	userHomeDir = func() (string, error) { return home, nil }
	getenv = func(key string) string {
		switch key {
		case "MUESLI_APPDATA":
			return ""
		case "XDG_DATA_HOME":
			return filepath.Join(home, ".local", "share")
		default:
			return ""
		}
	}

	got, err := StorageDir()
	if err != nil {
		t.Fatalf("StorageDir() error: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "muesli", "storage", "audio")
	if got != want {
		t.Fatalf("StorageDir() = %q, want %q", got, want)
	}
}
