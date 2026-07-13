package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

var allowedSharedNoteKeys = []string{"date", "summary", "title", "transcript"}

var forbiddenSharedNoteKeys = []string{"owner_id", "ownerId", "email", "note_id", "id", "deleted_at"}

func buildSharedNoteFixture(t *testing.T, srv *api.Server, st *store.Store, owner model.User, title, transcriptText, summaryText string) (model.Note, string) {
	t.Helper()

	note, err := st.CreateNote(context.Background(), owner.ID, title)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	startedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := st.Pool().Exec(context.Background(), `UPDATE notes SET started_at=$2 WHERE id=$1`, note.ID, startedAt); err != nil {
		t.Fatalf("set started_at: %v", err)
	}
	if _, err := st.SaveTranscript(context.Background(), model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test-transcriber",
		Model:             "test-model",
		Segments: []model.Segment{{
			StartMS: 0,
			EndMS:   1000,
			Text:    transcriptText,
			Source:  "mic",
		}},
	}); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(context.Background())
	if err != nil {
		t.Fatalf("built-in templates: %v", err)
	}
	if len(tmpls) == 0 {
		t.Fatal("need at least one built-in template")
	}
	summaryID, err := st.CreatePendingSummary(context.Background(), note.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create summary: %v", err)
	}
	if err := st.CompleteSummary(context.Background(), summaryID, "agent", "model", []model.SummarySection{{
		Heading:         "Overview",
		ContentMarkdown: summaryText,
	}}, false); err != nil {
		t.Fatalf("complete summary: %v", err)
	}

	shareRec := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/share", nil, authHeaderForUserID(t, st, owner.ID))
	if shareRec.Code != http.StatusCreated {
		t.Fatalf("create share status %d body %s", shareRec.Code, shareRec.Body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode share response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("expected share token")
	}
	return note, created.Token
}

func decodeSharedNoteKeys(t *testing.T, body []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode shared note keys: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertSharedNoteShape(t *testing.T, body []byte) {
	t.Helper()

	gotKeys := decodeSharedNoteKeys(t, body)
	if !reflect.DeepEqual(gotKeys, allowedSharedNoteKeys) {
		t.Fatalf("shared response keys = %v, want %v", gotKeys, allowedSharedNoteKeys)
	}

	raw := string(body)
	for _, forbidden := range forbiddenSharedNoteKeys {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("shared response leaked forbidden key %q in %s", forbidden, raw)
		}
	}
}

func sharedNoteGetFromIP(t *testing.T, srv *api.Server, path string, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func newRateLimitedSharedTestServer(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	return api.NewServer(api.Deps{
		Store: st,
		Config: config.Config{
			PublicURL:       "https://app.example.com",
			RateSharedRPS:   0.001,
			RateSharedBurst: 1,
		},
	}), st
}

func TestSharedNoteSiblingContentNeverLeaks(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}

	owner, ownerHdr := createAuthedUser(t, st, "shared-leak-owner@example.com")
	noteA, err := st.CreateNote(ctx, owner.ID, "Alpha note")
	if err != nil {
		t.Fatalf("create note a: %v", err)
	}
	noteB, err := st.CreateNote(ctx, owner.ID, "Beta note")
	if err != nil {
		t.Fatalf("create note b: %v", err)
	}

	if _, err := st.Pool().Exec(ctx, `UPDATE notes SET started_at=$2 WHERE id IN ($1, $3)`, noteA.ID, time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), noteB.ID); err != nil {
		t.Fatalf("set started_at: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteA.ID,
		TranscriberPlugin: "test-transcriber",
		Model:             "test-model",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "Alpha transcript", Source: "mic"}},
	}); err != nil {
		t.Fatalf("save transcript a: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteB.ID,
		TranscriberPlugin: "test-transcriber",
		Model:             "test-model",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "Beta transcript", Source: "mic"}},
	}); err != nil {
		t.Fatalf("save transcript b: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil {
		t.Fatalf("built-in templates: %v", err)
	}
	if len(tmpls) < 1 {
		t.Fatal("need at least one built-in template")
	}
	readySummaryID, err := st.CreatePendingSummary(ctx, noteA.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create summary a: %v", err)
	}
	if err := st.CompleteSummary(ctx, readySummaryID, "agent", "model", []model.SummarySection{{Heading: "Overview", ContentMarkdown: "Alpha summary"}}, false); err != nil {
		t.Fatalf("complete summary a: %v", err)
	}
	readySummaryID, err = st.CreatePendingSummary(ctx, noteB.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create summary b: %v", err)
	}
	if err := st.CompleteSummary(ctx, readySummaryID, "agent", "model", []model.SummarySection{{Heading: "Overview", ContentMarkdown: "Beta summary"}}, false); err != nil {
		t.Fatalf("complete summary b: %v", err)
	}

	shareRec := doJSON(t, srv, http.MethodPost, "/api/notes/"+noteA.ID+"/share", nil, ownerHdr)
	if shareRec.Code != http.StatusCreated {
		t.Fatalf("create share status %d body %s", shareRec.Code, shareRec.Body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode share response: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/shared/"+created.Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("shared note status %d body %s", rec.Code, rec.Body)
	}
	assertSharedNoteShape(t, rec.Body.Bytes())

	raw := rec.Body.String()
	for _, unwanted := range []string{
		noteB.Title,
		"Beta transcript",
		"Beta summary",
	} {
		if strings.Contains(raw, unwanted) {
			t.Fatalf("shared response leaked %q in %s", unwanted, raw)
		}
	}
}

func TestSharedNoteNotFoundParity(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}
	owner, ownerHdr := createAuthedUser(t, st, "shared-notfound-owner@example.com")
	note, token := buildSharedNoteFixture(t, srv, st, owner, "Not found note", "Not found transcript", "Not found summary")

	expiredAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	expiredShare, err := st.CreateShare(ctx, owner.ID, note.ID, &expiredAt)
	if err != nil {
		t.Fatalf("create expired share: %v", err)
	}

	if rec := doJSON(t, srv, http.MethodDelete, "/api/shares/"+token, nil, ownerHdr); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke share status %d body %s", rec.Code, rec.Body)
	}

	wantBody := `{"error":"not found"}` + "\n"
	for name, path := range map[string]string{
		"nonexistent": "/api/shared/does-not-exist",
		"revoked":     "/api/shared/" + token,
		"expired":     "/api/shared/" + expiredShare.Token,
	} {
		rec := doJSON(t, srv, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s token status %d body %s", name, rec.Code, rec.Body)
		}
		if rec.Body.String() != wantBody {
			t.Fatalf("%s token body = %q, want %q", name, rec.Body.String(), wantBody)
		}
	}
}

func TestSharedNoteCannotPivotToOwnerRoutes(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}
	owner, ownerHdr := createAuthedUser(t, st, "shared-pivot-owner@example.com")
	note, token := buildSharedNoteFixture(t, srv, st, owner, "Pivot note", "Pivot transcript", "Pivot summary")

	if rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated note fetch status %d body %s", rec.Code, rec.Body)
	}

	for name, req := range map[string]*http.Request{
		"bearer": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			return r
		}(),
		"cookie": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
			r.AddCookie(&http.Cookie{Name: "muesli_session", Value: token})
			return r
		}(),
	} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s pivot status %d body %s", name, rec.Code, rec.Body)
		}
	}

	// Keep the owner header in play so this test also verifies the note share
	// fixture is otherwise valid and revocable.
	if rec := doJSON(t, srv, http.MethodDelete, "/api/shares/"+token, nil, ownerHdr); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke share status %d body %s", rec.Code, rec.Body)
	}
}

func TestSharedNoteRateLimitTrips(t *testing.T) {
	t.Parallel()

	srv, st := newRateLimitedSharedTestServer(t)
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}
	owner, _ := createAuthedUser(t, st, "shared-ratelimit-owner@example.com")
	_, token := buildSharedNoteFixture(t, srv, st, owner, "Rate limit note", "Rate limit transcript", "Rate limit summary")

	first := sharedNoteGetFromIP(t, srv, "/api/shared/"+token, "192.0.2.1:1234")
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("first shared-note request unexpectedly rate-limited: %d body %s", first.Code, first.Body)
	}

	second := sharedNoteGetFromIP(t, srv, "/api/shared/"+token, "192.0.2.1:1234")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second shared-note request status %d body %s", second.Code, second.Body)
	}
}
