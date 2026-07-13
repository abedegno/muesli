package api_test

// This file is DB-backed and CI-only: it uses testutil.NewPool via the store
// to exercise the note-share handlers end to end.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func newShareTestServer(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{
		Store: st,
		Config: config.Config{
			PublicURL: "https://app.example.com",
		},
	})
	return srv, st
}

func authHeaderForUserID(t *testing.T, st *store.Store, userID string) map[string]string {
	t.Helper()

	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(context.Background(), userID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + raw}
}

func createAuthedUser(t *testing.T, st *store.Store, email string) (model.User, map[string]string) {
	t.Helper()

	u, err := st.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u, authHeaderForUserID(t, st, u.ID)
}

func TestCreateAndListShare(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	owner, ownerHdr := createAuthedUser(t, st, "shares-owner@example.com")
	note, err := st.CreateNote(context.Background(), owner.ID, "Shared note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/share", nil, ownerHdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create share status %d body %s", rec.Code, rec.Body)
	}
	var created struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("expected token in create response")
	}
	wantURL := "https://app.example.com/shared/" + created.Token
	if created.URL != wantURL {
		t.Fatalf("create url = %q, want %q", created.URL, wantURL)
	}

	list := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/shares", nil, ownerHdr)
	if list.Code != http.StatusOK {
		t.Fatalf("list shares status %d body %s", list.Code, list.Body)
	}
	var shares []model.Share
	if err := json.Unmarshal(list.Body.Bytes(), &shares); err != nil {
		t.Fatalf("decode shares list: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("shares len = %d, want 1", len(shares))
	}
	if shares[0].Token != created.Token || shares[0].NoteID != note.ID || shares[0].OwnerID != owner.ID {
		t.Fatalf("share = %+v, want token %q note %q owner %q", shares[0], created.Token, note.ID, owner.ID)
	}
}

func TestShareOwnerScope(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	owner, ownerHdr := createAuthedUser(t, st, "shares-scope-owner@example.com")
	other, otherHdr := createAuthedUser(t, st, "shares-scope-other@example.com")
	ownedNote, err := st.CreateNote(context.Background(), owner.ID, "Owned")
	if err != nil {
		t.Fatalf("create owned note: %v", err)
	}
	foreignNote, err := st.CreateNote(context.Background(), other.ID, "Foreign")
	if err != nil {
		t.Fatalf("create foreign note: %v", err)
	}

	if rec := doJSON(t, srv, http.MethodPost, "/api/notes/"+foreignNote.ID+"/share", nil, ownerHdr); rec.Code != http.StatusNotFound {
		t.Fatalf("create share on foreign note status %d body %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+foreignNote.ID+"/shares", nil, ownerHdr); rec.Code != http.StatusNotFound {
		t.Fatalf("list foreign note shares status %d body %s", rec.Code, rec.Body)
	}

	shareRec := doJSON(t, srv, http.MethodPost, "/api/notes/"+ownedNote.ID+"/share", nil, ownerHdr)
	if shareRec.Code != http.StatusCreated {
		t.Fatalf("create owned share status %d body %s", shareRec.Code, shareRec.Body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode share response: %v", err)
	}

	if rec := doJSON(t, srv, http.MethodDelete, "/api/shares/"+created.Token, nil, otherHdr); rec.Code != http.StatusNotFound {
		t.Fatalf("delete foreign token status %d body %s", rec.Code, rec.Body)
	}
}

func TestRevokeShareByTokenMarksInactive(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	owner, ownerHdr := createAuthedUser(t, st, "shares-revoke-owner@example.com")
	note, err := st.CreateNote(context.Background(), owner.ID, "Revocable")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	shareRec := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/share", nil, ownerHdr)
	if shareRec.Code != http.StatusCreated {
		t.Fatalf("create share status %d body %s", shareRec.Code, shareRec.Body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode share response: %v", err)
	}

	del := doJSON(t, srv, http.MethodDelete, "/api/shares/"+created.Token, nil, ownerHdr)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete share status %d body %s", del.Code, del.Body)
	}

	list := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/shares", nil, ownerHdr)
	if list.Code != http.StatusOK {
		t.Fatalf("list shares status %d body %s", list.Code, list.Body)
	}
	var shares []model.Share
	if err := json.Unmarshal(list.Body.Bytes(), &shares); err != nil {
		t.Fatalf("decode shares list: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("shares len = %d, want 1", len(shares))
	}
	if shares[0].RevokedAt == nil {
		t.Fatalf("revoked share missing revoked_at: %+v", shares[0])
	}
	if shares[0].Token != created.Token {
		t.Fatalf("revoked share token = %q, want %q", shares[0].Token, created.Token)
	}

	got, err := st.GetActiveShare(context.Background(), created.Token)
	if !errors.Is(err, store.ErrNotFound) || got != nil {
		t.Fatalf("GetActiveShare after revoke = (%+v, %v), want (nil, ErrNotFound)", got, err)
	}
}
