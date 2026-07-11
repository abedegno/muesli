package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/db"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/worker"
)

// runReembed implements the `muesli reembed` subcommand. It parses args for
// --dry-run, connects to the DB, and delegates to execReembed. args should be
// os.Args[2:] (everything after "reembed"). stdout is where progress lines are
// written; use os.Stdout in production.
func runReembed(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reembed", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "count notes that would be re-embedded without making changes")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("reembed: parse flags: %w", err)
	}

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer pool.Close()

	return execReembed(ctx, store.New(pool), cfg, *dryRun, stdout)
}

// execReembed is the testable core of the reembed operation. It takes an
// already-constructed store so tests can inject a pool directly without
// needing config.DatabaseURL to be set.
//
//   - dryRun=true: counts notes that would be re-embedded (no changes).
//   - dryRun=false: deletes current-model embeddings and enqueues fresh jobs.
func execReembed(ctx context.Context, st *store.Store, cfg config.Config, dryRun bool, stdout io.Writer) error {
	// Refuse when embeddings are disabled — otherwise we'd wipe existing vectors
	// and enqueue jobs the worker would only no-op (silent data loss).
	embedder := embed.New(cfg)
	if embedder == nil {
		return fmt.Errorf("reembed: embeddings are disabled (set MUESLI_EMBEDDINGS_URL); nothing done")
	}
	dim := embedder.Dim()

	if dryRun {
		ids, err := st.NotesMissingEmbedding(ctx, cfg.EmbeddingsModel, dim, 100000)
		if err != nil {
			return fmt.Errorf("count missing embeddings: %w", err)
		}
		fmt.Fprintf(stdout, "Dry run: %d note(s) would be re-embedded using model %q (dim %d). No changes made.\n",
			len(ids), cfg.EmbeddingsModel, dim)
		return nil
	}

	cleared, err := st.DeleteEmbeddingsForModel(ctx, cfg.EmbeddingsModel, dim)
	if err != nil {
		return fmt.Errorf("delete embeddings for %q (dim %d): %w", cfg.EmbeddingsModel, dim, err)
	}
	// All current-(model, dim) rows are gone, so the backfill enqueues every ready note.
	enqueued, err := worker.EnqueueBackfillEmbeds(ctx, st, cfg.EmbeddingsModel, dim, 100000)
	if err != nil {
		return fmt.Errorf("enqueue backfill: %w", err)
	}
	slog.InfoContext(ctx, "reembed", "model", cfg.EmbeddingsModel, "dim", dim, "cleared", cleared, "enqueued", enqueued)
	fmt.Fprintf(stdout, "Enqueued %d note(s) for re-embedding using model %q (dim %d).\n", enqueued, cfg.EmbeddingsModel, dim)
	return nil
}
