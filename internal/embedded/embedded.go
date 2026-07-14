package embedded

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Start boots the embedded Postgres stack and returns its database URL plus a
// shutdown hook for the caller to run during process teardown.
func Start(ctx context.Context, reporter *Reporter) (databaseURL string, stop func(context.Context) error, err error) {
	appDataDir, err := AppDataDir()
	if err != nil {
		return "", nil, fmt.Errorf("resolve app data dir: %w", err)
	}

	reporter.Advance(PhaseDBInit, "starting embedded postgres")

	port, err := FreeLoopbackPort()
	if err != nil {
		return "", nil, fmt.Errorf("find free loopback port: %w", err)
	}

	dataDir := filepath.Join(appDataDir, "postgres", "data")
	pg, err := StartPostgres(ctx, dataDir, port)
	if err != nil {
		return "", nil, fmt.Errorf("start embedded postgres: %w", err)
	}

	cleanup := func(cleanupCtx context.Context) error {
		return pg.Stop(cleanupCtx)
	}

	if bundleDir := strings.TrimSpace(os.Getenv("MUESLI_EMBEDDED_PGVECTOR_DIR")); bundleDir != "" {
		reporter.Advance(PhasePgvector, "installing pgvector")
		targetRoot := pg.installRoot()
		libDir := filepath.Join(targetRoot, "lib")
		shareExtDir := filepath.Join(targetRoot, "share", "extension")

		installed, err := pgvectorControlInstalled(shareExtDir)
		if err != nil {
			if stopErr := cleanup(context.Background()); stopErr != nil {
				return "", nil, fmt.Errorf("check embedded pgvector: %w (cleanup stop failed: %v)", err, stopErr)
			}
			return "", nil, fmt.Errorf("check embedded pgvector: %w", err)
		}

		if !installed {
			if err := InstallPgvector(libDir, shareExtDir, bundleDir); err != nil {
				if stopErr := cleanup(context.Background()); stopErr != nil {
					return "", nil, fmt.Errorf("install embedded pgvector: %w (cleanup stop failed: %v)", err, stopErr)
				}
				return "", nil, fmt.Errorf("install embedded pgvector: %w", err)
			}
		}

		if err := EnsureExtension(ctx, pg.URL()); err != nil {
			if stopErr := cleanup(context.Background()); stopErr != nil {
				return "", nil, fmt.Errorf("ensure embedded pgvector extension: %w (cleanup stop failed: %v)", err, stopErr)
			}
			return "", nil, fmt.Errorf("ensure embedded pgvector extension: %w", err)
		}
	}

	return pg.URL(), cleanup, nil
}
