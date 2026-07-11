package store_test

import (
	"context"
	"testing"
)

// vec768 returns a 768-dim vector filled with seed (handy for trivial cases).
func vec768(seed float32) []float32 {
	v := make([]float32, 768)
	for i := range v {
		v[i] = seed
	}
	return v
}

// directional test vectors. query points along axis 0; near is mostly along
// axis 0 (small cosine distance); far points along a different axis entirely
// (larger cosine distance). So cosine distance orders near < far.
func queryVec() []float32 { v := make([]float32, 768); v[0] = 1; return v }
func nearVec() []float32  { v := make([]float32, 768); v[0] = 0.9; v[1] = 0.1; return v }
func farVec() []float32   { v := make([]float32, 768); v[100] = 1; return v }

// dimVec returns an n-dim vector with v[0]=1 (rest zero), for off-768 mixed-dim
// round-trip tests. Its cosine similarity to another v[0]=1 vector of any length is 1.
func dimVec(n int) []float32 { v := make([]float32, n); v[0] = 1; return v }

// the default test model — most tests embed and search under the same model.
const testModel = "test-model"

func TestUpsertAndSearchEmbeddings(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	noteA, err := st.CreateNote(ctx, ownerID, "Alpha")
	if err != nil {
		t.Fatalf("create note A: %v", err)
	}
	noteB, err := st.CreateNote(ctx, ownerID, "Bravo")
	if err != nil {
		t.Fatalf("create note B: %v", err)
	}
	for _, id := range []string{noteA.ID, noteB.ID} {
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
			t.Fatalf("set ready %s: %v", id, err)
		}
	}

	if err := st.UpsertEmbedding(ctx, noteA.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteB.ID, testModel, farVec()); err != nil {
		t.Fatalf("upsert B: %v", err)
	}

	res, err := st.SearchEmbeddings(ctx, ownerID, testModel, queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(res), res)
	}
	if res[0].ID != noteA.ID {
		t.Errorf("ranked first = %s, want noteA %s", res[0].ID, noteA.ID)
	}
	if res[0].Score < res[1].Score {
		t.Errorf("noteA score %v should be >= noteB score %v", res[0].Score, res[1].Score)
	}

	// Re-upsert noteA with a different vector → ON CONFLICT updates in place,
	// no duplicate row, still exactly 2 results.
	if err := st.UpsertEmbedding(ctx, noteA.ID, testModel, vec768(0.5)); err != nil {
		t.Fatalf("re-upsert A: %v", err)
	}
	res2, err := st.SearchEmbeddings(ctx, ownerID, testModel, queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search 2: %v", err)
	}
	if len(res2) != 2 {
		t.Fatalf("after re-upsert got %d results, want 2 (no duplicate)", len(res2))
	}
}

func TestSearchEmbeddingsOwnerScopedAndExcludesTrashed(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	noteA, err := st.CreateNote(ctx, ownerID, "Mine")
	if err != nil {
		t.Fatalf("create note A: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", noteA.ID); err != nil {
		t.Fatalf("set ready A: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteA.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert A: %v", err)
	}

	other := addUser(t, st)
	noteO, err := st.CreateNote(ctx, other, "Theirs")
	if err != nil {
		t.Fatalf("create other note: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", noteO.ID); err != nil {
		t.Fatalf("set ready O: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteO.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert O: %v", err)
	}

	res, err := st.SearchEmbeddings(ctx, ownerID, testModel, queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 || res[0].ID != noteA.ID {
		t.Fatalf("owner-scoped search = %+v, want only noteA %s", res, noteA.ID)
	}

	// Soft-delete noteA → it drops out of results.
	if _, err := pool.Exec(ctx, "UPDATE notes SET deleted_at=now() WHERE id=$1", noteA.ID); err != nil {
		t.Fatalf("soft-delete A: %v", err)
	}
	res2, err := st.SearchEmbeddings(ctx, ownerID, testModel, queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(res2) != 0 {
		t.Fatalf("after soft-delete got %+v, want []", res2)
	}
}

func TestNotesMissingEmbedding(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	// Ready, no embedding → should appear.
	missing, err := st.CreateNote(ctx, ownerID, "Missing")
	if err != nil {
		t.Fatalf("create missing: %v", err)
	}
	// Ready, WITH embedding → should be absent.
	has, err := st.CreateNote(ctx, ownerID, "Has")
	if err != nil {
		t.Fatalf("create has: %v", err)
	}
	// Non-ready note → should be absent.
	pending, err := st.CreateNote(ctx, ownerID, "Pending")
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}

	for _, id := range []string{missing.ID, has.ID} {
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
			t.Fatalf("set ready %s: %v", id, err)
		}
	}
	if err := st.UpsertEmbedding(ctx, has.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert has: %v", err)
	}

	ids, err := st.NotesMissingEmbedding(ctx, testModel, 768, 100)
	if err != nil {
		t.Fatalf("missing: %v", err)
	}

	got := make(map[string]bool, len(ids))
	for _, id := range ids {
		got[id] = true
	}
	if !got[missing.ID] {
		t.Errorf("expected missing note %s in list, got %v", missing.ID, ids)
	}
	if got[has.ID] {
		t.Errorf("note with embedding %s should be absent, got %v", has.ID, ids)
	}
	if got[pending.ID] {
		t.Errorf("non-ready note %s should be absent, got %v", pending.ID, ids)
	}
}

// TestSearchEmbeddingsFiltersByModel proves that searching as one model only
// returns notes embedded under that same model — keeping <=> comparisons within
// one dimension and excluding stale-model rows after a model switch.
func TestSearchEmbeddingsFiltersByModel(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	noteA, err := st.CreateNote(ctx, ownerID, "Alpha")
	if err != nil {
		t.Fatalf("create note A: %v", err)
	}
	noteB, err := st.CreateNote(ctx, ownerID, "Bravo")
	if err != nil {
		t.Fatalf("create note B: %v", err)
	}
	for _, id := range []string{noteA.ID, noteB.ID} {
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
			t.Fatalf("set ready %s: %v", id, err)
		}
	}

	if err := st.UpsertEmbedding(ctx, noteA.ID, "modelA", nearVec()); err != nil {
		t.Fatalf("upsert A under modelA: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteB.ID, "modelB", nearVec()); err != nil {
		t.Fatalf("upsert B under modelB: %v", err)
	}

	res, err := st.SearchEmbeddings(ctx, ownerID, "modelA", queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search modelA: %v", err)
	}
	if len(res) != 1 || res[0].ID != noteA.ID {
		t.Fatalf("search as modelA = %+v, want only noteA %s", res, noteA.ID)
	}
}

// TestNotesMissingEmbeddingPerModel proves a note embedded only under modelA is
// reported missing for modelB (so switching models re-embeds everything).
func TestNotesMissingEmbeddingPerModel(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, "OnlyModelA")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
		t.Fatalf("set ready: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, note.ID, "modelA", nearVec()); err != nil {
		t.Fatalf("upsert under modelA: %v", err)
	}

	// Missing for modelB (has only a modelA embedding).
	idsB, err := st.NotesMissingEmbedding(ctx, "modelB", 768, 100)
	if err != nil {
		t.Fatalf("missing modelB: %v", err)
	}
	if !containsID(idsB, note.ID) {
		t.Errorf("note %s should be missing under modelB, got %v", note.ID, idsB)
	}

	// Present for modelA (already embedded).
	idsA, err := st.NotesMissingEmbedding(ctx, "modelA", 768, 100)
	if err != nil {
		t.Fatalf("missing modelA: %v", err)
	}
	if containsID(idsA, note.ID) {
		t.Errorf("note %s should NOT be missing under modelA, got %v", note.ID, idsA)
	}
}

// TestDeleteEmbeddingsForModel proves the delete only removes the target model's
// rows and returns the count.
func TestDeleteEmbeddingsForModel(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	noteA, err := st.CreateNote(ctx, ownerID, "Alpha")
	if err != nil {
		t.Fatalf("create note A: %v", err)
	}
	noteB, err := st.CreateNote(ctx, ownerID, "Bravo")
	if err != nil {
		t.Fatalf("create note B: %v", err)
	}
	for _, id := range []string{noteA.ID, noteB.ID} {
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
			t.Fatalf("set ready %s: %v", id, err)
		}
	}
	if err := st.UpsertEmbedding(ctx, noteA.ID, "modelA", nearVec()); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteB.ID, "modelB", nearVec()); err != nil {
		t.Fatalf("upsert B: %v", err)
	}

	n, err := st.DeleteEmbeddingsForModel(ctx, "modelA", 768)
	if err != nil {
		t.Fatalf("delete modelA: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d rows, want 1", n)
	}

	// modelA's note now searches empty; modelB's is untouched.
	resA, err := st.SearchEmbeddings(ctx, ownerID, "modelA", queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search modelA: %v", err)
	}
	if len(resA) != 0 {
		t.Fatalf("after delete, modelA search = %+v, want []", resA)
	}
	resB, err := st.SearchEmbeddings(ctx, ownerID, "modelB", queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search modelB: %v", err)
	}
	if len(resB) != 1 || resB[0].ID != noteB.ID {
		t.Fatalf("after delete, modelB search = %+v, want only noteB %s", resB, noteB.ID)
	}
}

// TestMixedDimensionRoundTrip stores a 768-dim vector under one model and a
// 384-dim vector under another in the same (unsized) column, then searches each
// model with a matching-dim query — no dimension-mismatch error, correct results.
func TestMixedDimensionRoundTrip(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	noteBig, err := st.CreateNote(ctx, ownerID, "Big768")
	if err != nil {
		t.Fatalf("create big: %v", err)
	}
	noteSmall, err := st.CreateNote(ctx, ownerID, "Small384")
	if err != nil {
		t.Fatalf("create small: %v", err)
	}
	for _, id := range []string{noteBig.ID, noteSmall.ID} {
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
			t.Fatalf("set ready %s: %v", id, err)
		}
	}

	if err := st.UpsertEmbedding(ctx, noteBig.ID, "model768", dimVec(768)); err != nil {
		t.Fatalf("upsert 768-dim: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteSmall.ID, "model384", dimVec(384)); err != nil {
		t.Fatalf("upsert 384-dim: %v", err)
	}

	// Search each model with a matching-dim query: no dim error, correct note.
	resBig, err := st.SearchEmbeddings(ctx, ownerID, "model768", dimVec(768), 768, 10)
	if err != nil {
		t.Fatalf("search 768: %v", err)
	}
	if len(resBig) != 1 || resBig[0].ID != noteBig.ID {
		t.Fatalf("768 search = %+v, want only noteBig %s", resBig, noteBig.ID)
	}
	resSmall, err := st.SearchEmbeddings(ctx, ownerID, "model384", dimVec(384), 384, 10)
	if err != nil {
		t.Fatalf("search 384: %v", err)
	}
	if len(resSmall) != 1 || resSmall[0].ID != noteSmall.ID {
		t.Fatalf("384 search = %+v, want only noteSmall %s", resSmall, noteSmall.ID)
	}
}

// containsID reports whether ids contains want.
func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestSearchEmbeddingsDimSegregation proves that search filters by BOTH model
// AND dim: a same-model, different-dim row is excluded from results (the required
// dim-mismatch handling test coverage from the backlog item).
func TestSearchEmbeddingsDimSegregation(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	// Note A embedded with dim 768.
	noteA, err := st.CreateNote(ctx, ownerID, "A-768")
	if err != nil {
		t.Fatalf("create note A: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", noteA.ID); err != nil {
		t.Fatalf("set ready A: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteA.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert A (768): %v", err)
	}

	// Note B embedded with the SAME model but dim 384 (simulating a model/config change).
	noteB, err := st.CreateNote(ctx, ownerID, "B-384")
	if err != nil {
		t.Fatalf("create note B: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", noteB.ID); err != nil {
		t.Fatalf("set ready B: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, noteB.ID, testModel, dimVec(384)); err != nil {
		t.Fatalf("upsert B (384): %v", err)
	}

	// Search with model + dim 768 must return ONLY noteA.
	res768, err := st.SearchEmbeddings(ctx, ownerID, testModel, queryVec(), 768, 10)
	if err != nil {
		t.Fatalf("search 768: %v", err)
	}
	if len(res768) != 1 || res768[0].ID != noteA.ID {
		t.Fatalf("search(768) = %+v, want only noteA %s", res768, noteA.ID)
	}

	// Search with model + dim 384 must return ONLY noteB.
	res384, err := st.SearchEmbeddings(ctx, ownerID, testModel, dimVec(384), 384, 10)
	if err != nil {
		t.Fatalf("search 384: %v", err)
	}
	if len(res384) != 1 || res384[0].ID != noteB.ID {
		t.Fatalf("search(384) = %+v, want only noteB %s", res384, noteB.ID)
	}
}

// TestUpsertEmbeddingStoresDim proves that the dim is stored and a round-trip
// UpsertEmbedding -> manual SELECT returns the correct dimension.
func TestUpsertEmbeddingStoresDim(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, "Test")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
		t.Fatalf("set ready: %v", err)
	}

	// Upsert a 1536-dim embedding.
	if err := st.UpsertEmbedding(ctx, note.ID, "custom-model", dimVec(1536)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Round-trip: read the stored dim.
	var storedDim int
	if err := pool.QueryRow(ctx, "SELECT dim FROM note_embeddings WHERE note_id=$1", note.ID).Scan(&storedDim); err != nil {
		t.Fatalf("select dim: %v", err)
	}
	if storedDim != 1536 {
		t.Fatalf("stored dim = %d, want 1536", storedDim)
	}
}

// TestNotesMissingEmbeddingSameModelDifferentDim proves that NotesMissingEmbedding
// treats a same-model, different-dim row as "missing" for the currently-configured dim.
// This ensures a dimension change triggers full re-backfill (per the contract).
func TestNotesMissingEmbeddingSameModelDifferentDim(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	// Create a ready note.
	note, err := st.CreateNote(ctx, ownerID, "Test")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
		t.Fatalf("set ready: %v", err)
	}

	// Upsert an embedding for this note at dim=384 under model "test-model".
	if err := st.UpsertEmbedding(ctx, note.ID, "test-model", dimVec(384)); err != nil {
		t.Fatalf("upsert 384-dim: %v", err)
	}

	// Query NotesMissingEmbedding for the SAME model but at dim=768.
	// The note should be reported as "missing" because the stored dim (384) doesn't match.
	missing, err := st.NotesMissingEmbedding(ctx, "test-model", 768, 100)
	if err != nil {
		t.Fatalf("NotesMissingEmbedding: %v", err)
	}

	// The note should be in the "missing" list (dim mismatch).
	found := false
	for _, id := range missing {
		if id == note.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("note %s not in missing list for (test-model, dim=768); got %v", note.ID, missing)
	}

	// Query NotesMissingEmbedding for the SAME model at the CORRECT dim=384.
	// The note should NOT be in the missing list.
	missing384, err := st.NotesMissingEmbedding(ctx, "test-model", 384, 100)
	if err != nil {
		t.Fatalf("NotesMissingEmbedding (384): %v", err)
	}
	for _, id := range missing384 {
		if id == note.ID {
			t.Fatalf("note %s incorrectly reported missing for (test-model, dim=384)", note.ID)
		}
	}
}

// TestEmbeddingStatus table-drives EmbeddingStatus over the scenarios called
// out in the EMB02 acceptance contract: no notes, mixed done/missing,
// non-ready notes excluded, model/dim mismatches reporting done=0, and a
// reset to 0 after DeleteEmbeddingsForModel.
func TestEmbeddingStatus(t *testing.T) {
	t.Parallel()

	t.Run("no notes", func(t *testing.T) {
		t.Parallel()
		st, _, _ := newStoreWithOwner(t)
		ctx := context.Background()

		done, total, err := st.EmbeddingStatus(ctx, testModel, 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus: %v", err)
		}
		if done != 0 || total != 0 {
			t.Fatalf("got done=%d total=%d, want 0,0", done, total)
		}
	})

	t.Run("mixed done and missing", func(t *testing.T) {
		t.Parallel()
		st, ownerID, pool := newStoreWithOwner(t)
		ctx := context.Background()

		noteA, err := st.CreateNote(ctx, ownerID, "Alpha")
		if err != nil {
			t.Fatalf("create note A: %v", err)
		}
		noteB, err := st.CreateNote(ctx, ownerID, "Bravo")
		if err != nil {
			t.Fatalf("create note B: %v", err)
		}
		for _, id := range []string{noteA.ID, noteB.ID} {
			if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
				t.Fatalf("set ready %s: %v", id, err)
			}
		}
		if err := st.UpsertEmbedding(ctx, noteA.ID, testModel, vec768(0.1)); err != nil {
			t.Fatalf("upsert A: %v", err)
		}

		done, total, err := st.EmbeddingStatus(ctx, testModel, 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus: %v", err)
		}
		if total != 2 {
			t.Errorf("total = %d, want 2", total)
		}
		if done != 1 {
			t.Errorf("done = %d, want 1", done)
		}
	})

	t.Run("non-ready notes excluded", func(t *testing.T) {
		t.Parallel()
		st, ownerID, pool := newStoreWithOwner(t)
		ctx := context.Background()

		ready, err := st.CreateNote(ctx, ownerID, "Ready")
		if err != nil {
			t.Fatalf("create ready note: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", ready.ID); err != nil {
			t.Fatalf("set ready: %v", err)
		}
		if err := st.UpsertEmbedding(ctx, ready.ID, testModel, vec768(0.1)); err != nil {
			t.Fatalf("upsert ready: %v", err)
		}

		// Left at the default (non-ready) status; should not count toward total.
		if _, err := st.CreateNote(ctx, ownerID, "NotReady"); err != nil {
			t.Fatalf("create not-ready note: %v", err)
		}

		done, total, err := st.EmbeddingStatus(ctx, testModel, 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus: %v", err)
		}
		if total != 1 {
			t.Errorf("total = %d, want 1 (non-ready excluded)", total)
		}
		if done != 1 {
			t.Errorf("done = %d, want 1", done)
		}
	})

	t.Run("different model reports done=0", func(t *testing.T) {
		t.Parallel()
		st, ownerID, pool := newStoreWithOwner(t)
		ctx := context.Background()

		note, err := st.CreateNote(ctx, ownerID, "Test")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
			t.Fatalf("set ready: %v", err)
		}
		if err := st.UpsertEmbedding(ctx, note.ID, testModel, vec768(0.1)); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		done, total, err := st.EmbeddingStatus(ctx, "some-other-model", 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus: %v", err)
		}
		if total != 1 {
			t.Errorf("total = %d, want 1", total)
		}
		if done != 0 {
			t.Errorf("done = %d, want 0 (different model)", done)
		}
	})

	t.Run("different dim reports done=0", func(t *testing.T) {
		t.Parallel()
		st, ownerID, pool := newStoreWithOwner(t)
		ctx := context.Background()

		note, err := st.CreateNote(ctx, ownerID, "Test")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
			t.Fatalf("set ready: %v", err)
		}
		// Embed at dim=384 under the same model name.
		if err := st.UpsertEmbedding(ctx, note.ID, testModel, dimVec(384)); err != nil {
			t.Fatalf("upsert 384-dim: %v", err)
		}

		done, total, err := st.EmbeddingStatus(ctx, testModel, 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus: %v", err)
		}
		if total != 1 {
			t.Errorf("total = %d, want 1", total)
		}
		if done != 0 {
			t.Errorf("done = %d, want 0 (stale dim must not count as done)", done)
		}
	})

	t.Run("resets to 0 after DeleteEmbeddingsForModel", func(t *testing.T) {
		t.Parallel()
		st, ownerID, pool := newStoreWithOwner(t)
		ctx := context.Background()

		note, err := st.CreateNote(ctx, ownerID, "Test")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
			t.Fatalf("set ready: %v", err)
		}
		if err := st.UpsertEmbedding(ctx, note.ID, testModel, vec768(0.1)); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		done, total, err := st.EmbeddingStatus(ctx, testModel, 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus (before delete): %v", err)
		}
		if done != 1 || total != 1 {
			t.Fatalf("before delete: done=%d total=%d, want 1,1", done, total)
		}

		if _, err := st.DeleteEmbeddingsForModel(ctx, testModel, 768); err != nil {
			t.Fatalf("DeleteEmbeddingsForModel: %v", err)
		}

		done, total, err = st.EmbeddingStatus(ctx, testModel, 768)
		if err != nil {
			t.Fatalf("EmbeddingStatus (after delete): %v", err)
		}
		if done != 0 {
			t.Errorf("done = %d, want 0 after delete", done)
		}
		if total != 1 {
			t.Errorf("total = %d, want 1 (unchanged; note still exists)", total)
		}
	})
}
