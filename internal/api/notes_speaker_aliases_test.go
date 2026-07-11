package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// setupAliasTest creates a test server, registers a user, logs in, creates a
// note, and returns the server, the store, an auth header, and the note ID.
func setupAliasTest(t *testing.T) (srv *api.Server, st *store.Store, hdr map[string]string, noteID string) {
	t.Helper()
	srv, st = newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "alias@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "alias@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr = map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Meeting"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)
	noteID = note.ID
	return
}

func TestSpeakerAliasesGetEmpty(t *testing.T) {
	t.Parallel()
	srv, _, hdr, noteID := setupAliasTest(t)

	rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET aliases status=%d body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Aliases []model.SpeakerAlias `json:"aliases"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Aliases) != 0 {
		t.Errorf("expected empty aliases, got %+v", resp.Aliases)
	}
}

func TestSpeakerAliasesPutAndGet(t *testing.T) {
	t.Parallel()
	srv, _, hdr, noteID := setupAliasTest(t)

	// PUT creates alias.
	put := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alice"}, hdr)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT alias status=%d body=%s", put.Code, put.Body)
	}

	// GET returns it.
	rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET aliases status=%d body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Aliases []model.SpeakerAlias `json:"aliases"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d: %+v", len(resp.Aliases), resp.Aliases)
	}
	if resp.Aliases[0].SpeakerLabel != "SPEAKER_00" || resp.Aliases[0].AliasName != "Alice" {
		t.Errorf("unexpected alias: %+v", resp.Aliases[0])
	}
}

func TestSpeakerAliasesPutIdempotent(t *testing.T) {
	t.Parallel()
	srv, _, hdr, noteID := setupAliasTest(t)

	doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alice"}, hdr)

	// Update alias_name via another PUT.
	put2 := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alicia"}, hdr)
	if put2.Code != http.StatusOK {
		t.Fatalf("PUT update status=%d body=%s", put2.Code, put2.Body)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, hdr)
	var resp struct {
		Aliases []model.SpeakerAlias `json:"aliases"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Aliases) != 1 || resp.Aliases[0].AliasName != "Alicia" {
		t.Errorf("after update: %+v", resp.Aliases)
	}
}

func TestSpeakerAliasesDelete(t *testing.T) {
	t.Parallel()
	srv, _, hdr, noteID := setupAliasTest(t)

	doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alice"}, hdr)

	// DELETE removes alias.
	del := doJSON(t, srv, http.MethodDelete, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00", nil, hdr)
	if del.Code != http.StatusNoContent {
		t.Fatalf("DELETE alias status=%d body=%s", del.Code, del.Body)
	}

	// GET shows it gone.
	rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, hdr)
	var resp struct {
		Aliases []model.SpeakerAlias `json:"aliases"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Aliases) != 0 {
		t.Errorf("after delete: got %+v", resp.Aliases)
	}
}

func TestSpeakerAliasesDeleteNotFound(t *testing.T) {
	t.Parallel()
	srv, _, hdr, noteID := setupAliasTest(t)

	del := doJSON(t, srv, http.MethodDelete, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00", nil, hdr)
	if del.Code != http.StatusNotFound {
		t.Fatalf("DELETE non-existent: status=%d body=%s", del.Code, del.Body)
	}
}

func TestSpeakerAliasesAuthRequired(t *testing.T) {
	t.Parallel()
	srv, _, _, noteID := setupAliasTest(t)

	rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET: status=%d, want 401", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "X"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT: status=%d, want 401", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated DELETE: status=%d, want 401", rec.Code)
	}
}

func TestSpeakerAliasesCrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, st, _, noteID := setupAliasTest(t)

	// Create a second user via store and mint a session token.
	other, _ := st.CreateUser(ctx, "other-alias@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}

	// Other user tries to PUT on owner's note → 404 (note not found for them).
	rec := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Hack"}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner PUT: status=%d, want 404; body=%s", rec.Code, rec.Body)
	}

	// Other user's GET also returns 404 (note not owned by them).
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner GET: status=%d, want 404; body=%s", rec.Code, rec.Body)
	}
}
