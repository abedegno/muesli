package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.manifest != "" || opts.cache != "" || opts.report != "" {
		t.Fatalf("expected empty defaults deferred to run(): %+v", opts)
	}
	if opts.workers != 1 {
		t.Fatalf("expected default workers=1, got %d", opts.workers)
	}
	if opts.offline || opts.check {
		t.Fatalf("expected offline/check false by default: %+v", opts)
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	opts, err := parseFlags([]string{"--offline", "--check", "--workers", "4", "--cache", "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.offline || !opts.check || opts.workers != 4 || opts.cache != "/tmp/x" {
		t.Fatalf("unexpected opts: %+v", opts)
	}
}

func TestParseFlagsHelp(t *testing.T) {
	_, err := parseFlags([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
}

func TestRunFailsOnMissingManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = run([]string{"--manifest", filepath.Join(dir, "does-not-exist.json")}, f)
	if err == nil {
		t.Fatal("expected failure for a missing manifest file")
	}
}

func TestRunRejectsDeniedCachePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = run([]string{"--cache", filepath.Join(dir, "internal", "evil-cache")}, f)
	if err == nil {
		t.Fatal("expected cache path rejection before any manifest read is attempted")
	}
}
