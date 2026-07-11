package store_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestPurgeNoteCascadesEmbeddings verifies that purging a trashed note also
// removes its note_embeddings row via the ON DELETE CASCADE FK on notes(id).
// No explicit DELETE FROM note_embeddings is needed in PurgeNote.
func TestPurgeNoteCascadesEmbeddings(t *testing.T) {
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	// Create owner and note.
	owner, err := st.CreateUser(ctx, "purge-cascade@example.com", "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "Cascade test")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// Insert a note_embeddings row via UpsertEmbedding (zero-vector, 768 dims).
	zeroVec := make([]float32, 768)
	if err := st.UpsertEmbedding(ctx, note.ID, testModel, zeroVec); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	// Confirm the embedding is present.
	var before int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM note_embeddings WHERE note_id = $1`, note.ID,
	).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if before != 1 {
		t.Fatalf("expected 1 embedding before purge, got %d", before)
	}

	// Trash the note (deleted_at must be set for PurgeNote to accept it).
	if err := st.DeleteNote(ctx, owner.ID, note.ID); err != nil {
		t.Fatalf("delete (trash) note: %v", err)
	}

	// Purge the note — should succeed and cascade-delete the embedding row.
	if _, err := st.PurgeNote(ctx, owner.ID, note.ID); err != nil {
		t.Fatalf("purge note: %v", err)
	}

	// The embedding row must be gone (CASCADE deleted).
	var after int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM note_embeddings WHERE note_id = $1`, note.ID,
	).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 0 {
		t.Errorf("expected 0 embedding rows after purge, got %d (CASCADE did not fire)", after)
	}
}
