package pluginkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A pre-bundled model file (packaged desktop app) must be used directly, with
// no MODEL_URL and no network access.
func TestResolveModelPrefersBundledFile(t *testing.T) {
	dir := t.TempDir()
	bundled := filepath.Join(dir, "ggml-tiny.en.bin")
	if err := os.WriteFile(bundled, []byte("fake-model"), 0o644); err != nil {
		t.Fatal(err)
	}
	// name has no ".bin" suffix and there is no URL: must still resolve to the
	// bundled file rather than erroring or downloading.
	got, err := ResolveModel(context.Background(), dir, "ggml-tiny.en", "", nil)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != bundled {
		t.Fatalf("got %q, want bundled %q", got, bundled)
	}
}

// A name that already carries the .bin suffix resolves to the same file and
// still short-circuits the URL download.
func TestResolveModelBundledNameWithBinSuffix(t *testing.T) {
	dir := t.TempDir()
	bundled := filepath.Join(dir, "base.en.bin")
	if err := os.WriteFile(bundled, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveModel(context.Background(), dir, "base.en.bin", "https://example.invalid/must-not-download", nil)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != bundled {
		t.Fatalf("got %q, want bundled %q", got, bundled)
	}
}

// With neither a bundled file nor a URL there is nothing to load: error, not a
// silent empty path (this is the exact packaged-app failure mode being fixed).
func TestResolveModelErrorsWhenNoFileAndNoURL(t *testing.T) {
	dir := t.TempDir()
	if _, err := ResolveModel(context.Background(), dir, "ggml-tiny.en", "", nil); err == nil {
		t.Fatal("expected an error when no bundled file and no model-url")
	}
}

func TestResolveModelErrorsWhenNoDir(t *testing.T) {
	if _, err := ResolveModel(context.Background(), "", "ggml-tiny.en", "", nil); err == nil {
		t.Fatal("expected an error for an empty model dir")
	}
}
