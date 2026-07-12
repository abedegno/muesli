package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func authHeaderForUser(t *testing.T, st *store.Store, email string) (map[string]string, string) {
	t.Helper()
	u, err := st.CreateUser(context.Background(), email, "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(context.Background(), u.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + raw}, u.ID
}

func decodeActionItemsResponse(t *testing.T, rec *http.Response) struct {
	ActionItems []model.ActionItem `json:"action_items"`
	Decisions   []model.Decision   `json:"decisions"`
} {
	t.Helper()
	var out struct {
		ActionItems []model.ActionItem `json:"action_items"`
		Decisions   []model.Decision   `json:"decisions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode action items response: %v body=%s", err, rec.Body)
	}
	return out
}

func TestActionItemsAPI(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Planning"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note=%d body=%s", noteRec.Code, noteRec.Body)
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	ownerID, err := st.NoteOwnerID(context.Background(), note.ID)
	if err != nil {
		t.Fatalf("note owner id: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(context.Background(), ownerID, note.ID,
		[]model.ActionItem{
			{Text: "Ship the doc", DueHint: "Friday"},
			{Text: "Book the room", DueHint: "Monday"},
		},
		[]model.Decision{{Text: "Use the weekly cadence"}},
	); err != nil {
		t.Fatalf("seed action items: %v", err)
	}

	otherHdr, otherOwnerID := authHeaderForUser(t, st, "other@example.com")
	ownerPerson, err := st.UpsertPerson(context.Background(), ownerID, "assignee@example.com", "Assignee", nil)
	if err != nil {
		t.Fatalf("create owner person: %v", err)
	}
	foreignPerson, err := st.UpsertPerson(context.Background(), otherOwnerID, "foreign@example.com", "Foreign", nil)
	if err != nil {
		t.Fatalf("create foreign person: %v", err)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/action-items", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get note action items=%d body=%s", rec.Code, rec.Body)
	}
	noteItems := decodeActionItemsResponse(t, rec.Result())
	if len(noteItems.ActionItems) != 2 || len(noteItems.Decisions) != 1 {
		t.Fatalf("unexpected note items: %+v", noteItems)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/action-items", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list action items=%d body=%s", rec.Code, rec.Body)
	}
	var allItems []model.ActionItem
	if err := json.NewDecoder(rec.Body).Decode(&allItems); err != nil {
		t.Fatalf("decode list response: %v body=%s", err, rec.Body)
	}
	if len(allItems) != 2 {
		t.Fatalf("list len=%d want 2", len(allItems))
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/action-items?status=open", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list open=%d body=%s", rec.Code, rec.Body)
	}
	var openItems []model.ActionItem
	if err := json.NewDecoder(rec.Body).Decode(&openItems); err != nil {
		t.Fatalf("decode open list: %v", err)
	}
	if len(openItems) != 2 {
		t.Fatalf("open len=%d want 2", len(openItems))
	}

	patchRec := doJSON(t, srv, http.MethodPatch, "/api/action-items/"+allItems[0].ID, map[string]string{"status": "done"}, hdr)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchRec.Code, patchRec.Body)
	}
	var patched model.ActionItem
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if patched.Status != model.ActionItemDone {
		t.Fatalf("patched status=%q want done", patched.Status)
	}

	patchRec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]string{"text": "Ship the updated doc"}, hdr)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch text=%d body=%s", patchRec.Code, patchRec.Body)
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatalf("decode text patch response: %v", err)
	}
	if patched.Text != "Ship the updated doc" || patched.Status != model.ActionItemDone || patched.OwnerPersonID != nil {
		t.Fatalf("text patch = %+v", patched)
	}

	patchRec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]any{"owner_person_id": ownerPerson.ID}, hdr)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("assign owner=%d body=%s", patchRec.Code, patchRec.Body)
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatalf("decode owner patch response: %v", err)
	}
	if patched.OwnerPersonID == nil || *patched.OwnerPersonID != ownerPerson.ID {
		t.Fatalf("assign owner = %+v", patched)
	}

	patchRec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]any{"owner_person_id": nil}, hdr)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("clear owner=%d body=%s", patchRec.Code, patchRec.Body)
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatalf("decode clear owner response: %v", err)
	}
	if patched.OwnerPersonID != nil {
		t.Fatalf("clear owner = %+v", patched)
	}

	patchRec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]any{"owner_person_id": foreignPerson.ID}, hdr)
	if patchRec.Code != http.StatusBadRequest {
		t.Fatalf("foreign owner status=%d body=%s", patchRec.Code, patchRec.Body)
	}
	reloaded, _, err := st.ListForNote(context.Background(), ownerID, note.ID)
	if err != nil {
		t.Fatalf("reload after foreign owner: %v", err)
	}
	var reloadedItem *model.ActionItem
	for i := range reloaded {
		if reloaded[i].ID == patched.ID {
			reloadedItem = &reloaded[i]
			break
		}
	}
	if reloadedItem == nil {
		t.Fatalf("reloaded item %s not found", patched.ID)
	}
	if reloadedItem.OwnerPersonID != nil {
		t.Fatalf("foreign owner patch should not change item: %+v", *reloadedItem)
	}

	patchRec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]any{
		"text":            "Finalize slides",
		"status":          "done",
		"owner_person_id": ownerPerson.ID,
	}, hdr)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("combined patch=%d body=%s", patchRec.Code, patchRec.Body)
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatalf("decode combined patch response: %v", err)
	}
	if patched.Text != "Finalize slides" || patched.Status != model.ActionItemDone || patched.OwnerPersonID == nil || *patched.OwnerPersonID != ownerPerson.ID {
		t.Fatalf("combined patch = %+v", patched)
	}

	otherSeed := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Other seeded"}, otherHdr)
	if otherSeed.Code != http.StatusCreated {
		t.Fatalf("create other note=%d body=%s", otherSeed.Code, otherSeed.Body)
	}
	var otherNote struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(otherSeed.Body.Bytes(), &otherNote); err != nil {
		t.Fatalf("decode other note: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(context.Background(), otherOwnerID, otherNote.ID, []model.ActionItem{{Text: "Other owner's item"}}, nil); err != nil {
		t.Fatalf("seed other action item: %v", err)
	}
	otherItems, _, err := st.ListForNote(context.Background(), otherOwnerID, otherNote.ID)
	if err != nil {
		t.Fatalf("list other note: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+otherItems[0].ID, map[string]string{"status": "open"}, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner patch status=%d body=%s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/action-items?status=done", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list done=%d body=%s", rec.Code, rec.Body)
	}
	var doneItems []model.ActionItem
	if err := json.NewDecoder(rec.Body).Decode(&doneItems); err != nil {
		t.Fatalf("decode done list: %v", err)
	}
	if len(doneItems) != 1 || doneItems[0].ID != patched.ID {
		t.Fatalf("done items=%+v want [%s]", doneItems, patched.ID)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/notes/00000000-0000-0000-0000-000000000000/action-items", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing note action items status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/action-items", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner note action items status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/action-items?status=bogus", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid list status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]string{}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing patch status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]string{"foo": "bar"}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown patch keys status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/action-items/"+patched.ID, map[string]string{"status": "bogus"}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid patch status=%d body=%s", rec.Code, rec.Body)
	}
}
