package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatURL(t *testing.T) {
	got := formatURL("test-password", 54321)
	want := "postgres://postgres:test-password@127.0.0.1:54321/postgres?sslmode=disable"
	if got != want {
		t.Fatalf("formatURL() = %q, want %q", got, want)
	}
}

func TestGeneratePassword(t *testing.T) {
	const samples = 16

	seen := make(map[string]struct{}, samples)
	for i := 0; i < samples; i++ {
		got, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword() error: %v", err)
		}
		if got == "" {
			t.Fatal("generatePassword() returned an empty password")
		}
		if strings.ContainsAny(got, " \t\n\r") {
			t.Fatalf("generatePassword() returned whitespace: %q", got)
		}
		if _, ok := seen[got]; ok {
			t.Fatalf("generatePassword() returned a duplicate password: %q", got)
		}
		seen[got] = struct{}{}
	}
}

func TestRemoveStalePostmasterPID(t *testing.T) {
	dataDir := t.TempDir()
	pidPath := filepath.Join(dataDir, postmasterPIDFile)

	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", pidPath, err)
	}

	removed, err := removeStalePostmasterPID(dataDir)
	if err != nil {
		t.Fatalf("removeStalePostmasterPID() error: %v", err)
	}
	if !removed {
		t.Fatal("removeStalePostmasterPID() = false, want true")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("postmaster.pid still exists after removal, stat err=%v", err)
	}
}

func TestRemoveStalePostmasterPIDKeepsLivePID(t *testing.T) {
	dataDir := t.TempDir()
	pidPath := filepath.Join(dataDir, postmasterPIDFile)

	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", pidPath, err)
	}

	removed, err := removeStalePostmasterPID(dataDir)
	if err != nil {
		t.Fatalf("removeStalePostmasterPID() error: %v", err)
	}
	if removed {
		t.Fatal("removeStalePostmasterPID() = true, want false for live PID")
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("postmaster.pid unexpectedly removed: %v", err)
	}
}
