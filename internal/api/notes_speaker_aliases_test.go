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

func setupAliasPersonTest(t *testing.T) (srv *api.Server, st *store.Store, ownerID string, hdr map[string]string, noteID string) {
	t.Helper()
	srv, st = newTestServer(t)

	ctx := context.Background()
	owner, err := st.CreateUser(ctx, "alias-person-owner@example.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(ctx, owner.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	hdr = map[string]string{"Authorization": "Bearer " + raw}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Meeting"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note status=%d body=%s", noteRec.Code, noteRec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)
	return srv, st, owner.ID, hdr, note.ID
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

func TestSpeakerAliasPersonSetAndClear(t *testing.T) {
	t.Parallel()
	srv, st, ownerID, hdr, noteID := setupAliasPersonTest(t)

	person, err := st.UpsertPerson(context.Background(), ownerID, "speaker@example.com", "Speaker", nil)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}

	put := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alice"}, hdr)
	if put.Code != http.StatusOK {
		t.Fatalf("create alias status=%d body=%s", put.Code, put.Body)
	}

	set := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00/person",
		map[string]string{"person_id": person.ID}, hdr)
	if set.Code != http.StatusOK {
		t.Fatalf("set person status=%d body=%s", set.Code, set.Body)
	}
	var setResp struct {
		NoteID       string  `json:"note_id"`
		SpeakerLabel string  `json:"speaker_label"`
		PersonID     *string `json:"person_id"`
	}
	if err := json.Unmarshal(set.Body.Bytes(), &setResp); err != nil {
		t.Fatalf("decode set response: %v", err)
	}
	if setResp.NoteID != noteID || setResp.SpeakerLabel != "SPEAKER_00" || setResp.PersonID == nil || *setResp.PersonID != person.ID {
		t.Fatalf("unexpected set response: %+v", setResp)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/speaker-aliases", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get aliases status=%d body=%s", rec.Code, rec.Body)
	}
	var aliases struct {
		Aliases []model.SpeakerAlias `json:"aliases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &aliases); err != nil {
		t.Fatalf("decode aliases response: %v", err)
	}
	if len(aliases.Aliases) != 1 || aliases.Aliases[0].PersonID == nil || *aliases.Aliases[0].PersonID != person.ID {
		t.Fatalf("expected linked person in list, got %+v", aliases.Aliases)
	}

	clear := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00/person",
		map[string]any{"person_id": nil}, hdr)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear person status=%d body=%s", clear.Code, clear.Body)
	}
	var clearResp struct {
		NoteID       string  `json:"note_id"`
		SpeakerLabel string  `json:"speaker_label"`
		PersonID     *string `json:"person_id"`
	}
	if err := json.Unmarshal(clear.Body.Bytes(), &clearResp); err != nil {
		t.Fatalf("decode clear response: %v", err)
	}
	if clearResp.PersonID != nil {
		t.Fatalf("expected cleared link, got %+v", clearResp)
	}
}

func TestSpeakerAliasPersonReturns404ForMissingAliasAndForeignPerson(t *testing.T) {
	t.Parallel()
	srv, st, ownerID, hdr, noteID := setupAliasPersonTest(t)

	other, err := st.CreateUser(context.Background(), "alias-person-other@example.com", "password123")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherPerson, err := st.UpsertPerson(context.Background(), other.ID, "other@example.com", "Other", nil)
	if err != nil {
		t.Fatalf("upsert other person: %v", err)
	}
	ownedPerson, err := st.UpsertPerson(context.Background(), ownerID, "owned@example.com", "Owned", nil)
	if err != nil {
		t.Fatalf("upsert owned person: %v", err)
	}

	put := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alice"}, hdr)
	if put.Code != http.StatusOK {
		t.Fatalf("create alias status=%d body=%s", put.Code, put.Body)
	}

	otherHdr := map[string]string{"Authorization": "Bearer " + func() string {
		raw, hash, err := auth.GenerateToken()
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		if err := st.CreateToken(context.Background(), other.ID, "session", hash, "session"); err != nil {
			t.Fatalf("create token: %v", err)
		}
		return raw
	}()}

	missingAlias := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_99/person",
		map[string]string{"person_id": ownedPerson.ID}, hdr)
	if missingAlias.Code != http.StatusNotFound {
		t.Fatalf("missing alias status=%d body=%s", missingAlias.Code, missingAlias.Body)
	}

	foreignPerson := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00/person",
		map[string]string{"person_id": otherPerson.ID}, hdr)
	if foreignPerson.Code != http.StatusNotFound {
		t.Fatalf("foreign person status=%d body=%s", foreignPerson.Code, foreignPerson.Body)
	}

	crossOwner := doJSON(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00/person",
		map[string]string{"person_id": ownedPerson.ID}, otherHdr)
	if crossOwner.Code != http.StatusNotFound {
		t.Fatalf("cross-owner alias status=%d body=%s", crossOwner.Code, crossOwner.Body)
	}
}

func TestSpeakerAliasPersonRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	srv, _, _, hdr, noteID := setupAliasPersonTest(t)

	put := doRaw(t, srv, http.MethodPut, "/api/notes/"+noteID+"/speaker-aliases/SPEAKER_00/person", "{", hdr)
	if put.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status=%d body=%s", put.Code, put.Body)
	}
}
