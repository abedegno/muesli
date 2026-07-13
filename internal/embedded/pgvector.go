package embedded

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// InstallPgvector copies pgvector artifacts from bundleDir into the target
// embedded Postgres library and extension directories.
func InstallPgvector(pgLibDir, pgShareExtDir, bundleDir string) error {
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		return fmt.Errorf("read pgvector bundle dir %q: %w", bundleDir, err)
	}

	if err := os.MkdirAll(pgLibDir, 0o755); err != nil {
		return fmt.Errorf("create pgvector lib dir %q: %w", pgLibDir, err)
	}
	if err := os.MkdirAll(pgShareExtDir, 0o755); err != nil {
		return fmt.Errorf("create pgvector extension dir %q: %w", pgShareExtDir, err)
	}

	for _, name := range []string{"vector.so", "vector.dll", "vector.dylib"} {
		if err := copyOptionalFile(filepath.Join(bundleDir, name), filepath.Join(pgLibDir, name)); err != nil {
			return err
		}
	}

	if err := copyRequiredFile(filepath.Join(bundleDir, "vector.control"), filepath.Join(pgShareExtDir, "vector.control")); err != nil {
		return err
	}

	var sqlFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "vector--") && strings.HasSuffix(name, ".sql") {
			sqlFiles = append(sqlFiles, name)
		}
	}
	sort.Strings(sqlFiles)

	for _, name := range sqlFiles {
		if err := copyRequiredFile(filepath.Join(bundleDir, name), filepath.Join(pgShareExtDir, name)); err != nil {
			return err
		}
	}

	return nil
}

// EnsureExtension connects to Postgres and creates the vector extension if it
// is not already present.
func EnsureExtension(ctx context.Context, url string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect to postgres %q: %w", url, err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("create vector extension on %q: %w", url, err)
	}

	return nil
}

func copyOptionalFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat pgvector artifact %q: %w", src, err)
	}
	return copyFileIfPresent(src, dst)
}

func copyRequiredFile(src, dst string) error {
	return copyFileIfPresent(src, dst)
}

func copyFileIfPresent(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat pgvector artifact %q: %w", src, err)
	}
	if info.IsDir() {
		return fmt.Errorf("pgvector artifact %q is a directory", src)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read pgvector artifact %q: %w", src, err)
	}

	perm := info.Mode().Perm()
	if perm == 0 {
		perm = 0o644
	}

	if err := os.WriteFile(dst, data, perm); err != nil {
		return fmt.Errorf("write pgvector artifact %q: %w", dst, err)
	}

	return nil
}
