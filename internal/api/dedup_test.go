package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestAudioDedupCheck(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	st := store.New(pool)
	srv := api.NewServer(api.Deps{Store: st})
	ctx := context.Background()

	// Register user1 and log in.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "dedup@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "dedup@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	t.Run("both_empty_returns_400", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]string{}, hdr)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("unauthenticated_returns_401", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]string{"audio_hash": "abc"}, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", rec.Code)
		}
	})

	t.Run("no_match_returns_empty_array", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]string{"audio_hash": "nonexistent-hash-xyz"}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body)
		}
		var resp struct {
			Matches []map[string]any `json:"matches"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Matches == nil {
			t.Fatal("matches should be an empty array, not null")
		}
		if len(resp.Matches) != 0 {
			t.Fatalf("want 0 matches, got %d", len(resp.Matches))
		}
	})

	// Create a note for user1 and inject hashes directly via SQL.
	createRec := doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Dedup test note"}, hdr)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", createRec.Code, createRec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(createRec.Body.Bytes(), &note)

	const testAudioHash = "sha256:abc123def456"
	const testNormHash = "sha256:norm789ghi"

	if _, err := pool.Exec(ctx,
		"UPDATE notes SET audio_hash=$1, normalized_audio_hash=$2 WHERE id=$3",
		testAudioHash, testNormHash, note.ID); err != nil {
		t.Fatalf("setting hashes: %v", err)
	}

	t.Run("match_on_audio_hash", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]string{"audio_hash": testAudioHash}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body)
		}
		var resp struct {
			Matches []struct {
				NoteID string `json:"note_id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"matches"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Matches) != 1 {
			t.Fatalf("want 1 match, got %d: %s", len(resp.Matches), rec.Body)
		}
		if resp.Matches[0].NoteID != note.ID {
			t.Fatalf("want note_id=%s, got %s", note.ID, resp.Matches[0].NoteID)
		}
		if resp.Matches[0].Title != "Dedup test note" {
			t.Fatalf("want title 'Dedup test note', got %q", resp.Matches[0].Title)
		}
	})

	t.Run("match_on_normalized_audio_hash", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]string{"normalized_audio_hash": testNormHash}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body)
		}
		var resp struct {
			Matches []struct {
				NoteID string `json:"note_id"`
			} `json:"matches"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Matches) != 1 {
			t.Fatalf("want 1 match, got %d: %s", len(resp.Matches), rec.Body)
		}
		if resp.Matches[0].NoteID != note.ID {
			t.Fatalf("want note_id=%s, got %s", note.ID, resp.Matches[0].NoteID)
		}
	})

	t.Run("match_on_both_hashes_no_duplicate", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]any{
				"audio_hash":            testAudioHash,
				"normalized_audio_hash": testNormHash,
			}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body)
		}
		var resp struct {
			Matches []struct {
				NoteID string `json:"note_id"`
			} `json:"matches"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Matches) != 1 {
			t.Fatalf("want exactly 1 match (no duplicate), got %d: %s", len(resp.Matches), rec.Body)
		}
	})

	t.Run("owner_isolation", func(t *testing.T) {
		// Create a second user in the same DB schema via the store directly.
		other, err := st.CreateUser(ctx, "other-dedup@example.com", "h")
		if err != nil {
			t.Fatalf("creating second user: %v", err)
		}
		raw, hash, _ := auth.GenerateToken()
		_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
		otherHdr := map[string]string{"Authorization": "Bearer " + raw}

		// The other user queries with user1's hash — should get no results.
		rec := doJSON(t, srv, http.MethodPost, "/api/audio/dedup-check",
			map[string]string{"audio_hash": testAudioHash}, otherHdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body)
		}
		var resp struct {
			Matches []map[string]any `json:"matches"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Matches) != 0 {
			t.Fatalf("owner isolation: want 0 matches for other user, got %d", len(resp.Matches))
		}
	})
}
