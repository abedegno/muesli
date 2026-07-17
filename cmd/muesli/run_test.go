package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/abedegno/muesli/internal/config"
)

func TestRunEmbeddedModeCleansUpPostgresOnLateFailure(t *testing.T) {
	root := t.TempDir()
	appDataDir := filepath.Join(root, "appdata")
	storagePath := filepath.Join(root, "storage.bin")
	if err := os.WriteFile(storagePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write storage placeholder: %v", err)
	}

	t.Setenv("MUESLI_APPDATA", appDataDir)
	t.Setenv("MUESLI_STORAGE_DIR", storagePath)
	t.Setenv("MUESLI_EMBEDDED_PGVECTOR_DIR", "")

	cfg := config.Config{
		Embedded:       true,
		MasterKey:      mustBase64Key(t),
		StorageDir:     storagePath,
		PublicURL:      "http://127.0.0.1:8080",
		InternalURL:    "http://127.0.0.1:8080",
		DatabaseURL:    "",
		Addr:           "127.0.0.1:0",
		LogLevel:       "info",
		LogFormat:      "text",
		Production:     false,
		AudioRetention: "keep",
	}

	if err := run(context.Background(), cfg); err == nil {
		t.Fatal("run() succeeded, want late failure")
	}

	pidFile := filepath.Join(appDataDir, "postgres", "data", "postmaster.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read postmaster pid file: %v", err)
		}
		return
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		t.Fatalf("postmaster pid file malformed: %q", string(data))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil {
		t.Fatalf("parse postmaster pid: %v", err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("postmaster process %d still alive after run returned", pid)
	}
}
