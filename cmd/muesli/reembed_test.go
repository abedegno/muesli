package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// embedCfg returns a minimal config with embeddings enabled so execReembed
// does not bail on the "embeddings disabled" guard.
func embedCfg(modelName string) config.Config {
	return config.Config{
		EmbeddingsURL:   "http://localhost:11434",
		EmbeddingsModel: modelName,
	}
}

func TestExecReembed_DryRun(t *testing.T) {
	pool := testutil.NewPool(t)
	ctx := context.Background()
	st := store.New(pool)
	cfg := embedCfg("text-embedding-3-small")

	// Seed 3 notes with status='ready'.
	user, err := st.CreateUser(ctx, "dryrun@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i := 0; i < 3; i++ {
		note, err := st.CreateNote(ctx, user.ID, fmt.Sprintf("Note %d", i+1))
		if err != nil {
			t.Fatalf("create note %d: %v", i+1, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE notes SET status=$1 WHERE id=$2`,
			model.NoteReady, note.ID); err != nil {
			t.Fatalf("set note %d ready: %v", i+1, err)
		}
	}

	// Pre-condition: 3 notes missing embedding.
	before, err := st.NotesMissingEmbedding(ctx, cfg.EmbeddingsModel, 768, 100000)
	if err != nil {
		t.Fatalf("NotesMissingEmbedding before: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("want 3 notes missing embedding, got %d", len(before))
	}

	var buf bytes.Buffer
	if err := execReembed(ctx, st, cfg, true, &buf); err != nil {
		t.Fatalf("execReembed dry-run: %v", err)
	}

	out := buf.String()
	t.Logf("stdout: %q", out)

	// Assert the expected output format.
	if !strings.Contains(out, "Dry run") {
		t.Errorf("stdout missing 'Dry run': got %q", out)
	}
	if !strings.Contains(out, "3 note(s)") {
		t.Errorf("stdout missing '3 note(s)': got %q", out)
	}
	if !strings.Contains(out, cfg.EmbeddingsModel) {
		t.Errorf("stdout missing model name %q: got %q", cfg.EmbeddingsModel, out)
	}
	if !strings.Contains(out, "No changes made") {
		t.Errorf("stdout missing 'No changes made': got %q", out)
	}

	// NotesMissingEmbedding must return the same count (dry-run made no changes).
	after, err := st.NotesMissingEmbedding(ctx, cfg.EmbeddingsModel, 768, 100000)
	if err != nil {
		t.Fatalf("NotesMissingEmbedding after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("dry-run changed state: before=%d after=%d", len(before), len(after))
	}

	// No embed jobs must have been enqueued.
	jobs, err := st.ListJobs(ctx, model.JobPending)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	for _, j := range jobs {
		if j.Type == model.JobEmbed {
			t.Errorf("dry-run unexpectedly enqueued embed job (id=%s note_id=%s)", j.ID, j.NoteID)
		}
	}
}

func TestExecReembed_Normal(t *testing.T) {
	pool := testutil.NewPool(t)
	ctx := context.Background()
	st := store.New(pool)
	cfg := embedCfg("text-embedding-3-small")

	// Seed 2 notes with status='ready'.
	user, err := st.CreateUser(ctx, "normal@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i := 0; i < 2; i++ {
		note, err := st.CreateNote(ctx, user.ID, fmt.Sprintf("Note %d", i+1))
		if err != nil {
			t.Fatalf("create note %d: %v", i+1, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE notes SET status=$1 WHERE id=$2`,
			model.NoteReady, note.ID); err != nil {
			t.Fatalf("set note %d ready: %v", i+1, err)
		}
	}

	var buf bytes.Buffer
	if err := execReembed(ctx, st, cfg, false, &buf); err != nil {
		t.Fatalf("execReembed normal: %v", err)
	}

	out := buf.String()
	t.Logf("stdout: %q", out)

	if !strings.Contains(out, "Enqueued 2 note(s)") {
		t.Errorf("stdout missing 'Enqueued 2 note(s)': got %q", out)
	}
	if !strings.Contains(out, cfg.EmbeddingsModel) {
		t.Errorf("stdout missing model name %q: got %q", cfg.EmbeddingsModel, out)
	}

	// Exactly 2 pending embed jobs must have been enqueued.
	jobs, err := st.ListJobs(ctx, model.JobPending)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	var embedCount int
	for _, j := range jobs {
		if j.Type == model.JobEmbed {
			embedCount++
		}
	}
	if embedCount != 2 {
		t.Errorf("want 2 pending embed jobs, got %d", embedCount)
	}
}

func TestExecReembed_DisabledEmbeddings(t *testing.T) {
	pool := testutil.NewPool(t)
	ctx := context.Background()
	st := store.New(pool)

	cfg := config.Config{
		EmbeddingsURL:   "", // disabled
		EmbeddingsModel: "nomic-embed-text",
	}

	var buf bytes.Buffer
	err := execReembed(ctx, st, cfg, true, &buf)
	if err == nil {
		t.Fatal("expected error when embeddings are disabled, got nil")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected 'disabled' in error, got %q", err.Error())
	}
}
