package embedded

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPgvector(t *testing.T) {
	t.Run("copies artifacts and is idempotent", func(t *testing.T) {
		bundleDir := t.TempDir()
		libDir := t.TempDir()
		shareExtDir := t.TempDir()

		writeFixture(t, filepath.Join(bundleDir, "vector.so"), []byte("so-bytes"))
		writeFixture(t, filepath.Join(bundleDir, "vector.control"), []byte("control-bytes"))
		writeFixture(t, filepath.Join(bundleDir, "vector--0.5.0.sql"), []byte("sql-050"))
		writeFixture(t, filepath.Join(bundleDir, "vector--0.4.0--0.5.0.sql"), []byte("sql-045-050"))

		if err := InstallPgvector(libDir, shareExtDir, bundleDir); err != nil {
			t.Fatalf("first InstallPgvector() error: %v", err)
		}
		if err := InstallPgvector(libDir, shareExtDir, bundleDir); err != nil {
			t.Fatalf("second InstallPgvector() error: %v", err)
		}

		assertFileContent(t, filepath.Join(libDir, "vector.so"), []byte("so-bytes"))
		assertFileContent(t, filepath.Join(shareExtDir, "vector.control"), []byte("control-bytes"))
		assertFileContent(t, filepath.Join(shareExtDir, "vector--0.5.0.sql"), []byte("sql-050"))
		assertFileContent(t, filepath.Join(shareExtDir, "vector--0.4.0--0.5.0.sql"), []byte("sql-045-050"))
	})

	t.Run("partial bundle copies only present artifacts", func(t *testing.T) {
		bundleDir := t.TempDir()
		libDir := t.TempDir()
		shareExtDir := t.TempDir()

		writeFixture(t, filepath.Join(bundleDir, "vector.so"), []byte("so-bytes"))
		writeFixture(t, filepath.Join(bundleDir, "vector.control"), []byte("control-bytes"))

		if err := InstallPgvector(libDir, shareExtDir, bundleDir); err != nil {
			t.Fatalf("InstallPgvector() error: %v", err)
		}

		assertFileContent(t, filepath.Join(libDir, "vector.so"), []byte("so-bytes"))
		assertFileContent(t, filepath.Join(shareExtDir, "vector.control"), []byte("control-bytes"))

		if _, err := os.Stat(filepath.Join(libDir, "vector.dll")); !os.IsNotExist(err) {
			t.Fatalf("vector.dll present in target lib dir, stat err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(libDir, "vector.dylib")); !os.IsNotExist(err) {
			t.Fatalf("vector.dylib present in target lib dir, stat err=%v", err)
		}
		if matches, err := filepath.Glob(filepath.Join(shareExtDir, "vector--*.sql")); err != nil {
			t.Fatalf("Glob() error: %v", err)
		} else if len(matches) != 0 {
			t.Fatalf("unexpected sql files copied: %v", matches)
		}
	})

	t.Run("missing bundle errors", func(t *testing.T) {
		libDir := t.TempDir()
		shareExtDir := t.TempDir()
		missingBundleDir := filepath.Join(t.TempDir(), "missing")

		if err := InstallPgvector(libDir, shareExtDir, missingBundleDir); err == nil {
			t.Fatal("InstallPgvector() error = nil, want non-nil")
		}
	})
}

func writeFixture(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file %q = %q, want %q", path, got, want)
	}
}
