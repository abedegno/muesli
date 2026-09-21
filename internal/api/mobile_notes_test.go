// This file is DB-backed and CI-only: testutil.NewPool (via newTestServer)
// skips when TEST_DATABASE_URL is unset, so it must not be run on the local
// runner.
package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type mobileNoteItemDTO struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Pinned    bool       `json:"pinned"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Snippet   string     `json:"snippet"`
	Tags      []string   `json:"tags"`
}

type mobileNotesListDTO struct {
	Items      []mobileNoteItemDTO `json:"items"`
	NextCursor string              `json:"next_cursor"`
}

type mobileNoteDetailDTO struct {
	Note struct {
		ID     string   `json:"id"`
		Title  string   `json:"title"`
		Status string   `json:"status"`
		Pinned bool     `json:"pinned"`
		Tags   []string `json:"tags"`
	} `json:"note"`
	BodyMarkdown string `json:"body_markdown"`
	Summaries    []struct {
		ID           string `json:"id"`
		TemplateName string `json:"template_name"`
		Status       string `json:"status"`
		Truncated    bool   `json:"truncated"`
		Sections     []struct {
			Heading         string `json:"heading"`
			ContentMarkdown string `json:"content_markdown"`
		} `json:"sections"`
	} `json:"summaries"`
}

func TestMobileNotesListErrors(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	hdr, _ := authHeaderForUser(t, st, "mobile-list-errors@example.com")

	for _, limit := range []string{"0", "-1", "101", "abc", "1.5"} {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes?limit="+limit, nil, hdr)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit=%s: status %d body %s", limit, rec.Code, rec.Body)
		}
		var errResp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp["error"] != "invalid limit" {
			t.Fatalf("limit=%s: unexpected error body %v", limit, errResp)
		}
	}

	for _, cursor := range []string{"not-base64!!", "###", "AAAA"} {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes?cursor="+cursor, nil, hdr)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("cursor=%s: status %d body %s", cursor, rec.Code, rec.Body)
		}
		var errResp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp["error"] != "invalid cursor" {
			t.Fatalf("cursor=%s: unexpected error body %v", cursor, errResp)
		}
	}

	// Unauthenticated.
	rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d", rec.Code)
	}
}

func TestMobileNotesListPaginationAndFields(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	hdr, uid := authHeaderForUser(t, st, "mobile-list@example.com")
	otherHdr, _ := authHeaderForUser(t, st, "mobile-list-other@example.com")

	// Empty first page.
	rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list status %d body %s", rec.Code, rec.Body)
	}
	var empty mobileNotesListDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &empty)
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("expected empty non-nil items, got %+v", empty)
	}
	if empty.NextCursor != "" {
		t.Fatalf("expected no cursor on empty page")
	}

	// Create 4 notes for uid via the real handler responses, one with tags.
	var created []string
	for i := 0; i < 4; i++ {
		rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Note"}, hdr)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create note: status %d body %s", rec.Code, rec.Body)
		}
		var n struct{ ID string }
		_ = json.Unmarshal(rec.Body.Bytes(), &n)
		created = append(created, n.ID)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+created[0]+"/tags", map[string]string{"name": "planning"}, hdr)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("add tag status %d body %s", rec.Code, rec.Body)
	}

	// A note for the other user must never leak.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Other's note"}, otherHdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create other note status %d", rec.Code)
	}

	// Page through with limit=1: 4 pages, 4 unique ids, final page has no cursor.
	var pages []mobileNotesListDTO
	cursor := ""
	for i := 0; i < 10; i++ {
		url := "/api/mobile/v1/notes?limit=1"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		rec := doJSON(t, srv, http.MethodGet, url, nil, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d status %d body %s", i, rec.Code, rec.Body)
		}
		var page mobileNotesListDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("page %d decode: %v", i, err)
		}
		pages = append(pages, page)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	var allIDs []string
	for _, p := range pages {
		if len(p.Items) != 1 {
			t.Fatalf("expected exactly 1 item per page with limit=1, got %+v", p)
		}
		allIDs = append(allIDs, p.Items[0].ID)
	}
	if len(allIDs) != 4 {
		t.Fatalf("expected 4 notes across pages, got %d: %v", len(allIDs), allIDs)
	}
	seen := map[string]bool{}
	for _, id := range allIDs {
		if seen[id] {
			t.Fatalf("duplicate id %s across pages", id)
		}
		seen[id] = true
	}
	if pages[len(pages)-1].NextCursor != "" {
		t.Fatalf("final page must omit next_cursor")
	}

	// Field minimization + non-null tags on the item carrying the tag.
	var tagged mobileNoteItemDTO
	for _, p := range pages {
		if p.Items[0].ID == created[0] {
			tagged = p.Items[0]
		}
	}
	if len(tagged.Tags) != 1 || tagged.Tags[0] != "planning" {
		t.Fatalf("expected tag on note, got %+v", tagged)
	}

	// Owner isolation: other user's list never contains uid's notes.
	rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes", nil, otherHdr)
	var otherList mobileNotesListDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &otherList)
	for _, item := range otherList.Items {
		for _, id := range created {
			if item.ID == id {
				t.Fatalf("owner isolation violated: %s leaked to other user", id)
			}
		}
	}
	_ = uid
}

func TestMobileNoteDetail(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	hdr, _ := authHeaderForUser(t, st, "mobile-detail@example.com")
	otherHdr, _ := authHeaderForUser(t, st, "mobile-detail-other@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Detail note"}, hdr)
	var n struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n)

	rec = doJSON(t, srv, http.MethodPut, "/api/notes/"+n.ID+"/body", map[string]string{"content": "# Heading\nSome *markdown* body."}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("update body status %d body %s", rec.Code, rec.Body)
	}

	t.Run("success", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+n.ID, nil, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("detail status %d body %s", rec.Code, rec.Body)
		}
		var detail mobileNoteDetailDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if detail.Note.ID != n.ID || detail.Note.Title != "Detail note" {
			t.Fatalf("unexpected note: %+v", detail.Note)
		}
		if detail.BodyMarkdown == "" {
			t.Fatalf("expected body markdown")
		}
		if detail.Note.Tags == nil {
			t.Fatalf("tags must be non-null")
		}
		if detail.Summaries == nil {
			t.Fatalf("summaries must be non-null")
		}
	})

	t.Run("other owner sees 404", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+n.ID, nil, otherHdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
		var errResp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp["error"] != "not found" {
			t.Fatalf("unexpected error body %v", errResp)
		}
	})

	t.Run("invalid uuid is indistinguishable 404", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/not-a-uuid", nil, hdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
	})

	t.Run("absent note is 404", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/00000000-0000-0000-0000-000000000000", nil, hdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
	})

	t.Run("deleted note is 404", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodDelete, "/api/notes/"+n.ID, nil, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("delete status %d body %s", rec.Code, rec.Body)
		}
		rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+n.ID, nil, hdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
	})

	// Unauthenticated.
	rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+n.ID, nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d", rec.Code)
	}
}

// TestMobileNotes500 mirrors TestDBErrorReturns500: an unexpected store
// failure must surface as a generic 500 with no internals leaked, for both
// mobile routes.
func TestMobileNotes500(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	hdr, _ := authHeaderForUser(t, st, "mobile-500@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Note"}, hdr)
	var n struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n)

	// The auth middleware only touches app_tokens, so breaking note_bodies
	// (joined by both mobile queries) still lets auth succeed but breaks the
	// store call itself with a genuine pgx error. Dropping the column the
	// query actually selects (nb.content) rather than the whole table keeps
	// this immune to Postgres's search_path fallback to the always-present,
	// always-empty public.note_bodies: the table itself still exists in this
	// test's own schema, so unqualified references keep resolving to it
	// (never falling through to public), and the missing column produces a
	// genuine "column does not exist" error on both mobile routes.
	if _, err := st.Pool().Exec(t.Context(), "ALTER TABLE note_bodies DROP COLUMN content"); err != nil {
		t.Fatalf("alter table: %v", err)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes", nil, hdr)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list: want 500, got %d; body=%s", rec.Code, rec.Body.String())
	}
	var listErr map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &listErr)
	if listErr["error"] != "internal error" {
		t.Fatalf("list: want generic error, got %q (internals leaked?)", listErr["error"])
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+n.ID, nil, hdr)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("detail: want 500, got %d; body=%s", rec.Code, rec.Body.String())
	}
	var detailErr map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &detailErr)
	if detailErr["error"] != "internal error" {
		t.Fatalf("detail: want generic error, got %q (internals leaked?)", detailErr["error"])
	}
}

// TestMobileNotesFieldMinimization decodes a real handler response into a
// raw map (rather than a typed DTO) so an accidentally-added field (owner_id,
// folder_ids, event_id, audio metadata, transcript, summary bodies) fails
// the test instead of silently round-tripping through Go's "unknown field
// ignored" JSON decoding.
func TestMobileNotesFieldMinimization(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	hdr, _ := authHeaderForUser(t, st, "mobile-fields@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Field check"}, hdr)
	var n struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n)
	_ = doJSON(t, srv, http.MethodPut, "/api/notes/"+n.ID+"/body", map[string]string{"content": "body"}, hdr)

	rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes", nil, hdr)
	var listRaw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listRaw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listRaw.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", listRaw.Items)
	}
	wantListKeys := map[string]bool{
		"id": true, "title": true, "status": true, "pinned": true,
		"created_at": true, "updated_at": true, "snippet": true, "tags": true,
		// started_at/ended_at are omitempty and legitimately absent here.
	}
	for k := range listRaw.Items[0] {
		if !wantListKeys[k] {
			t.Fatalf("unexpected field %q in mobile list item: %+v", k, listRaw.Items[0])
		}
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+n.ID, nil, hdr)
	var detailRaw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detailRaw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantTopKeys := map[string]bool{"note": true, "body_markdown": true, "summaries": true}
	for k := range detailRaw {
		if !wantTopKeys[k] {
			t.Fatalf("unexpected top-level field %q in mobile detail: %+v", k, detailRaw)
		}
	}
	noteRaw, _ := detailRaw["note"].(map[string]any)
	wantNoteKeys := map[string]bool{
		"id": true, "title": true, "status": true, "pinned": true,
		"created_at": true, "updated_at": true, "tags": true,
	}
	for k := range noteRaw {
		if !wantNoteKeys[k] {
			t.Fatalf("unexpected field %q in mobile detail note (snippet must be excluded): %+v", k, noteRaw)
		}
	}
}
