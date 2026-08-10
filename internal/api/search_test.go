package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

type fakeEmbedder struct {
	vec      []float32
	lastText *string
}

func (f fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if f.lastText != nil {
		*f.lastText = text
	}
	return f.vec, nil
}

func (f fakeEmbedder) Dim() int { return len(f.vec) }

func fixedVec() []float32 {
	v := make([]float32, embed.Dim)
	for i := range v {
		v[i] = float32((i % 7) + 1)
	}
	return v
}

const testModel = "test-model"

func newSearchServer(t *testing.T, emb embed.Embedder) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	return api.NewServer(api.Deps{Store: st, Embedder: emb, Config: config.Config{EmbeddingsModel: testModel}}), st
}

func newSearchServerCfg(t *testing.T, emb embed.Embedder, cfg config.Config) (*api.Server, *store.Store) {
	t.Helper()
	if cfg.EmbeddingsModel == "" {
		cfg.EmbeddingsModel = testModel
	}
	st := store.New(testutil.NewPool(t))
	return api.NewServer(api.Deps{Store: st, Embedder: emb, Config: cfg}), st
}

func oneHotVec() []float32 {
	v := make([]float32, embed.Dim)
	v[0] = 1
	return v
}

func authHeader(t *testing.T, srv *api.Server, email string) map[string]string {
	t.Helper()
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return map[string]string{"Authorization": "Bearer " + login.Token}
}

func createNote(t *testing.T, srv *api.Server, hdr map[string]string, title string) string {
	t.Helper()
	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": title}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note %q status %d body %s", title, rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatalf("create note %q: empty id; body %s", title, rec.Body)
	}
	return created.ID
}

func setNoteCreatedAt(t *testing.T, st *store.Store, noteID string, ts time.Time) {
	t.Helper()
	_, err := st.Pool().Exec(context.Background(),
		`UPDATE notes SET created_at=$1, updated_at=$1 WHERE id=$2`, ts, noteID)
	if err != nil {
		t.Fatalf("set note created_at: %v", err)
	}
}

func createTemplate(t *testing.T, st *store.Store, ownerID string) string {
	t.Helper()
	tmpl, err := st.CreateTemplate(context.Background(), ownerID, "Search template", "after",
		[]model.TemplateSection{{Heading: "Section", Instruction: "Instruction"}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	return tmpl.ID
}

func addTranscript(t *testing.T, st *store.Store, noteID string, segs []model.Segment) string {
	t.Helper()
	tr, err := st.SaveTranscript(context.Background(), model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments:          segs,
	})
	if err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	if len(tr.Segments) == 0 {
		t.Fatalf("SaveTranscript returned no segments")
	}
	return tr.Segments[0].ID
}

func addSummary(t *testing.T, st *store.Store, noteID, templateID string, sections []model.SummarySection) {
	t.Helper()
	sumID, err := st.CreatePendingSummary(context.Background(), noteID, templateID)
	if err != nil {
		t.Fatalf("CreatePendingSummary: %v", err)
	}
	if err := st.CompleteSummary(context.Background(), sumID, "agent", "model", sections, false); err != nil {
		t.Fatalf("CompleteSummary: %v", err)
	}
}

func searchMatches(t *testing.T, srv *api.Server, hdr map[string]string, q string, params map[string]string) []api.SearchMatch {
	t.Helper()
	values := url.Values{}
	values.Set("q", q)
	for k, v := range params {
		values.Set(k, v)
	}
	rec := doJSON(t, srv, http.MethodGet, "/api/search?"+values.Encode(), nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("search q=%q status %d body %s", q, rec.Code, rec.Body)
	}
	var matches []api.SearchMatch
	if err := json.Unmarshal(rec.Body.Bytes(), &matches); err != nil {
		t.Fatalf("search q=%q decode %v body %s", q, err, rec.Body)
	}
	return matches
}

func matchesForNote(matches []api.SearchMatch, noteID string) []api.SearchMatch {
	out := make([]api.SearchMatch, 0, len(matches))
	for _, m := range matches {
		if m.NoteID == noteID {
			out = append(out, m)
		}
	}
	return out
}

func hasMatchType(matches []api.SearchMatch, matchType string) bool {
	for _, m := range matches {
		if m.MatchType == matchType {
			return true
		}
	}
	return false
}

func firstNoteID(matches []api.SearchMatch) string {
	if len(matches) == 0 {
		return ""
	}
	return matches[0].NoteID
}

func userIDByEmail(t *testing.T, st *store.Store, email string) string {
	t.Helper()
	var ownerID string
	err := st.Pool().QueryRow(context.Background(), `SELECT id FROM users WHERE email=$1`, email).Scan(&ownerID)
	if err != nil {
		t.Fatalf("lookup owner id: %v", err)
	}
	return ownerID
}

func createFolder(t *testing.T, st *store.Store, ownerID, name string) string {
	t.Helper()
	folder, err := st.CreateFolder(context.Background(), ownerID, name, nil)
	if err != nil {
		t.Fatalf("CreateFolder(%q): %v", name, err)
	}
	return folder.ID
}

func seedSearchEvent(t *testing.T, st *store.Store, ownerID, noteID, externalID, title, attendeeEmail string) string {
	t.Helper()
	ctx := context.Background()
	source, err := st.CreateSource(ctx, ownerID, "google", "Search source", "{}")
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	ev := calendar.NormalizedEvent{
		ExternalID: externalID,
		Title:      title,
		StartsAt:   time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
		EndsAt:     time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Attendees: []model.Attendee{
			{Email: attendeeEmail},
		},
	}
	if err := st.UpsertEvents(ctx, ownerID, source.ID, []calendar.NormalizedEvent{ev}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	var eventID string
	if err := st.Pool().QueryRow(ctx,
		`SELECT id FROM calendar_events WHERE owner_id=$1 AND source_id=$2 AND external_id=$3`,
		ownerID, source.ID, externalID).Scan(&eventID); err != nil {
		t.Fatalf("lookup event id: %v", err)
	}
	if err := st.SetNoteEvent(ctx, ownerID, noteID, eventID); err != nil {
		t.Fatalf("SetNoteEvent: %v", err)
	}
	return eventID
}

func assertSearchNoteIDs(t *testing.T, matches []api.SearchMatch, want ...string) {
	t.Helper()
	got := map[string]int{}
	for _, m := range matches {
		got[m.NoteID]++
	}
	if len(got) != len(want) {
		t.Fatalf("note ids = %v, want %v", got, want)
	}
	for _, id := range want {
		if got[id] == 0 {
			t.Fatalf("note ids = %v, want %v", got, want)
		}
		got[id]--
	}
	for id, n := range got {
		if n != 0 {
			t.Fatalf("note ids = %v, want %v (extra %s count %d)", got, want, id, n)
		}
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()
	srv, _ := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/search?q=", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty query status %d", rec.Code)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("empty query body = %q, want %q", got, "[]\n")
	}
}

func TestSearchHandlerDoesNotLoadFullNoteList(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("search.go")
	if err != nil {
		t.Fatalf("read search handler: %v", err)
	}
	if strings.Contains(string(source), ".ListNotes(") {
		t.Fatal("search handler must not load the owner's full note list")
	}
}

func TestSearchLexicalOnly(t *testing.T) {
	t.Parallel()
	srv, _ := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")

	acme := createNote(t, srv, hdr, "Acme planning")
	grocery := createNote(t, srv, hdr, "Grocery list")

	matches := searchMatches(t, srv, hdr, "acme", nil)
	if !hasMatchType(matchesForNote(matches, acme), "title") {
		t.Fatalf("lexical search for acme = %+v, want title match for %s", matches, acme)
	}
	if len(matchesForNote(matches, grocery)) != 0 {
		t.Fatalf("lexical search for acme = %+v, must not contain grocery %s", matches, grocery)
	}
}

func TestSearchLexicalTranscriptWithoutEmbedder(t *testing.T) {
	t.Parallel()
	srv, st := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "transcript@example.com")

	transcriptNote := createNote(t, srv, hdr, "Weekly meeting")
	otherNote := createNote(t, srv, hdr, "Unrelated note")
	addTranscript(t, st, transcriptNote, []model.Segment{{
		StartMS: 500, EndMS: 900, Text: "The zephyr launch is scheduled for Tuesday.", Source: "whisper",
	}})

	matches := searchMatches(t, srv, hdr, "zephyr", nil)
	if !hasMatchType(matchesForNote(matches, transcriptNote), "transcript") {
		t.Fatalf("transcript search = %+v, want transcript match for %s", matches, transcriptNote)
	}
	if len(matchesForNote(matches, otherNote)) != 0 {
		t.Fatalf("transcript search = %+v, must not contain unrelated note %s", matches, otherNote)
	}
	if got := searchMatches(t, srv, hdr, "termnotpresent", nil); len(got) != 0 {
		t.Fatalf("absent term search = %+v, want no matches", got)
	}
}

func TestSearchMatchClassification(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	hdr := authHeader(t, srv, "o@example.com")
	ctx := context.Background()

	titleNote := createNote(t, srv, hdr, "Alpha meeting notes")
	transcriptNote := createNote(t, srv, hdr, "Transcript note")
	summaryNote := createNote(t, srv, hdr, "Summary note")
	if err := st.UpsertEmbedding(ctx, titleNote, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding titleNote: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, transcriptNote, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding transcriptNote: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, summaryNote, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding summaryNote: %v", err)
	}

	segID := addTranscript(t, st, transcriptNote, []model.Segment{
		{StartMS: 1100, EndMS: 1600, Text: "We discussed alpha rollout and risks.", Source: "whisper"},
	})
	tmplID := createTemplate(t, st, userIDByEmail(t, st, "o@example.com"))
	addSummary(t, st, summaryNote, tmplID, []model.SummarySection{
		{Heading: "Alpha plan", ContentMarkdown: "The rollout has two stages."},
	})

	matches := searchMatches(t, srv, hdr, "alpha", nil)
	if got := matchesForNote(matches, titleNote); len(got) != 1 || got[0].MatchType != "title" {
		t.Fatalf("title note matches = %+v, want one title hit", got)
	}
	if got := matchesForNote(matches, transcriptNote); len(got) != 1 || got[0].MatchType != "transcript" {
		t.Fatalf("transcript note matches = %+v, want one transcript hit", got)
	}
	if got := matchesForNote(matches, summaryNote); len(got) != 1 || got[0].MatchType != "summary" {
		t.Fatalf("summary note matches = %+v, want one summary hit", got)
	}
	if got := matchesForNote(matches, transcriptNote); got[0].SegmentID != segID || got[0].StartMS != 1100 {
		t.Fatalf("transcript match metadata = %+v, want segment %s start_ms 1100", got, segID)
	}
}

func TestSearchSummaryMultiLocation(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	hdr := authHeader(t, srv, "o@example.com")
	ownerID := userIDByEmail(t, st, "o@example.com")
	ctx := context.Background()

	note := createNote(t, srv, hdr, "Summary multi-location")
	if err := st.UpsertEmbedding(ctx, note, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding note: %v", err)
	}
	tmplID := createTemplate(t, st, ownerID)
	addSummary(t, st, note, tmplID, []model.SummarySection{
		{Heading: "Beta heading", ContentMarkdown: "The beta body also mentions beta."},
	})

	matches := searchMatches(t, srv, hdr, "beta", nil)
	got := matchesForNote(matches, note)
	if len(got) != 2 {
		t.Fatalf("summary multi-location matches = %+v, want 2 entries", got)
	}
	if got[0].Snippet == "" || got[1].Snippet == "" {
		t.Fatalf("summary multi-location snippets = %+v, want both snippets populated", got)
	}
}

func TestSearchTranscriptEntryIncludesMetadata(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	hdr := authHeader(t, srv, "o@example.com")
	ctx := context.Background()

	note := createNote(t, srv, hdr, "Transcript metadata")
	if err := st.UpsertEmbedding(ctx, note, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding note: %v", err)
	}
	segID := addTranscript(t, st, note, []model.Segment{
		{StartMS: 1234, EndMS: 2345, Text: "Gamma response discussed here.", Source: "whisper"},
	})

	matches := searchMatches(t, srv, hdr, "gamma", nil)
	got := matchesForNote(matches, note)
	if len(got) != 1 || got[0].MatchType != "transcript" {
		t.Fatalf("transcript metadata matches = %+v, want one transcript hit", got)
	}
	if got[0].SegmentID != segID {
		t.Fatalf("segment_id = %q, want %q", got[0].SegmentID, segID)
	}
	if got[0].StartMS != 1234 {
		t.Fatalf("start_ms = %d, want 1234", got[0].StartMS)
	}
	if !strings.Contains(strings.ToLower(got[0].Snippet), "gamma") {
		t.Fatalf("snippet = %q, want query text", got[0].Snippet)
	}
}

func TestSearchRuneAwareSnippet(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	hdr := authHeader(t, srv, "o@example.com")
	ctx := context.Background()

	note := createNote(t, srv, hdr, "Unicode transcript")
	if err := st.UpsertEmbedding(ctx, note, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding note: %v", err)
	}
	addTranscript(t, st, note, []model.Segment{
		{StartMS: 100, EndMS: 200, Text: "naïve café résumé", Source: "whisper"},
	})

	matches := searchMatches(t, srv, hdr, "café", nil)
	got := matchesForNote(matches, note)
	if len(got) != 1 || got[0].MatchType != "transcript" {
		t.Fatalf("unicode snippet matches = %+v, want one transcript hit", got)
	}
	if !utf8.ValidString(got[0].Snippet) {
		t.Fatalf("unicode snippet is not valid UTF-8: %q", got[0].Snippet)
	}
	if !strings.Contains(got[0].Snippet, "naïve") || !strings.Contains(got[0].Snippet, "café") || !strings.Contains(got[0].Snippet, "résumé") {
		t.Fatalf("unicode snippet = %q, want intact non-ASCII content", got[0].Snippet)
	}
}

func TestSearchDateFiltering(t *testing.T) {
	t.Parallel()
	srv, st := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")

	jan1 := createNote(t, srv, hdr, "Daily meeting one")
	jan2 := createNote(t, srv, hdr, "Daily meeting two")
	jan2late := createNote(t, srv, hdr, "Daily meeting three")

	setNoteCreatedAt(t, st, jan1, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	setNoteCreatedAt(t, st, jan2, time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC))
	setNoteCreatedAt(t, st, jan2late, time.Date(2026, 1, 2, 23, 59, 59, 999999999, time.UTC))

	base := searchMatches(t, srv, hdr, "daily", nil)
	if got := len(base); got != 3 {
		t.Fatalf("baseline search = %+v, want 3 notes", base)
	}

	inRange := searchMatches(t, srv, hdr, "daily", map[string]string{"from": "2026-01-02", "to": "2026-01-02"})
	if got := len(inRange); got != 2 {
		t.Fatalf("date-filtered search = %+v, want 2 notes on 2026-01-02", inRange)
	}

	outOfRange := searchMatches(t, srv, hdr, "daily", map[string]string{"from": "2026-01-03"})
	if got := len(outOfRange); got != 0 {
		t.Fatalf("out-of-range search = %+v, want 0 notes", outOfRange)
	}
}

func TestSearchTagFolderFilters(t *testing.T) {
	t.Parallel()
	srv, st := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")
	ownerID := userIDByEmail(t, st, "o@example.com")
	folderID := createFolder(t, st, ownerID, "Projects")

	both := createNote(t, srv, hdr, "Alpha both")
	tagOnly := createNote(t, srv, hdr, "Alpha tag only")
	folderOnly := createNote(t, srv, hdr, "Alpha folder only")
	untouched := createNote(t, srv, hdr, "Beta unrelated")

	if _, err := st.AddNoteTag(context.Background(), ownerID, both, "work"); err != nil {
		t.Fatalf("AddNoteTag both: %v", err)
	}
	if _, err := st.AddNoteTag(context.Background(), ownerID, tagOnly, "work"); err != nil {
		t.Fatalf("AddNoteTag tagOnly: %v", err)
	}
	if err := st.AddNoteFolder(context.Background(), ownerID, both, folderID); err != nil {
		t.Fatalf("AddNoteFolder both: %v", err)
	}
	if err := st.AddNoteFolder(context.Background(), ownerID, folderOnly, folderID); err != nil {
		t.Fatalf("AddNoteFolder folderOnly: %v", err)
	}

	base := searchMatches(t, srv, hdr, "alpha", nil)
	assertSearchNoteIDs(t, base, both, tagOnly, folderOnly)

	tagged := searchMatches(t, srv, hdr, "alpha", map[string]string{"tag": "work"})
	assertSearchNoteIDs(t, tagged, both, tagOnly)

	foldered := searchMatches(t, srv, hdr, "alpha", map[string]string{"folder_id": folderID})
	assertSearchNoteIDs(t, foldered, both, folderOnly)

	combined := searchMatches(t, srv, hdr, "alpha", map[string]string{"tag": "work", "folder_id": folderID})
	assertSearchNoteIDs(t, combined, both)

	if len(matchesForNote(base, untouched)) != 0 {
		t.Fatalf("baseline search unexpectedly included untouched note %s", untouched)
	}
}

func TestSearchPersonFilterAndDateRange(t *testing.T) {
	t.Parallel()
	srv, st := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")
	ownerID := userIDByEmail(t, st, "o@example.com")
	ctx := context.Background()

	person, err := st.UpsertPerson(ctx, ownerID, "speaker@example.com", "Speaker", nil)
	if err != nil {
		t.Fatalf("UpsertPerson: %v", err)
	}

	attendeeNote := createNote(t, srv, hdr, "Alpha attendee note")
	aliasNote := createNote(t, srv, hdr, "Alpha alias note")
	otherNote := createNote(t, srv, hdr, "Alpha unrelated note")

	seedSearchEvent(t, st, ownerID, attendeeNote, "event-attendee", "Meeting", person.PrimaryEmail)
	if err := st.UpsertSpeakerAlias(ctx, ownerID, aliasNote, "speaker", "Speaker"); err != nil {
		t.Fatalf("UpsertSpeakerAlias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, aliasNote, "speaker", &person.ID); err != nil {
		t.Fatalf("SetSpeakerAliasPerson: %v", err)
	}

	setNoteCreatedAt(t, st, attendeeNote, time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC))
	setNoteCreatedAt(t, st, aliasNote, time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC))
	setNoteCreatedAt(t, st, otherNote, time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC))

	personMatches := searchMatches(t, srv, hdr, "alpha", map[string]string{"person_id": person.ID})
	assertSearchNoteIDs(t, personMatches, attendeeNote, aliasNote)

	ranged := searchMatches(t, srv, hdr, "alpha", map[string]string{
		"person_id": person.ID,
		"from":      "2026-01-02",
		"to":        "2026-01-02",
	})
	assertSearchNoteIDs(t, ranged, attendeeNote)

	if len(matchesForNote(personMatches, otherNote)) != 0 {
		t.Fatalf("person search unexpectedly included unrelated note %s", otherNote)
	}
}

func TestSearchInvalidFilterIDs(t *testing.T) {
	t.Parallel()
	srv, _ := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/search?q=alpha&folder_id=not-a-uuid", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid folder_id status = %d, want 400", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/search?q=alpha&person_id=not-a-uuid", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid person_id status = %d, want 400", rec.Code)
	}
}

func TestSearchInvalidDate(t *testing.T) {
	t.Parallel()
	srv, _ := newSearchServer(t, nil)
	hdr := authHeader(t, srv, "o@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/search?q=daily&from=not-a-date", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid from status = %d, want 400", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/search?q=daily&to=also-not-a-date", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid to status = %d, want 400", rec.Code)
	}
}

// TestSearchNonASCIISnippet proves snippet building is rune-aware, not
// byte-offset-based. PR #221 built snippets using len(q) BYTE-length offsets
// derived from a case-folded (strings.ToLower) substring search, which is
// wrong once byte length diverges from rune length after case folding —
// slicing at a mismatched offset can land on a non-rune boundary, panicking
// or corrupting multi-byte characters. Here the transcript segment is padded
// with plenty of multi-byte (non-ASCII) text before the match so a
// byte/rune-offset mismatch would very likely misalign the slice, and the
// query itself is non-ASCII with a differently-cased match in the text — this
// must not panic, and the returned snippet must be valid UTF-8 containing the
// exact matched text.
func TestSearchNonASCIISnippet(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	hdr := authHeader(t, srv, "o@example.com")
	ctx := context.Background()

	note := createNote(t, srv, hdr, "日本語のノート")
	if err := st.UpsertEmbedding(ctx, note, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}
	const text = "日本語の会議概要です。プロジェクトの進捗について話しました。CAFÉ project update 会議室にて。"
	segID := addTranscript(t, st, note, []model.Segment{
		{StartMS: 500, EndMS: 900, Text: text, Source: "whisper"},
	})

	// Query is lower-case and accented differently in case than the text's
	// "CAFÉ" — must still match case-insensitively, rune-aware.
	matches := searchMatches(t, srv, hdr, "café", nil)
	got := matchesForNote(matches, note)
	if len(got) != 1 || got[0].MatchType != "transcript" {
		t.Fatalf("non-ASCII search matches = %+v, want one transcript hit", got)
	}
	if got[0].SegmentID != segID {
		t.Fatalf("segment_id = %q, want %q", got[0].SegmentID, segID)
	}
	if !utf8.ValidString(got[0].Snippet) {
		t.Fatalf("snippet = %q is not valid UTF-8", got[0].Snippet)
	}
	if !strings.Contains(got[0].Snippet, "CAFÉ") {
		t.Fatalf("snippet = %q, want it to contain the matched text %q", got[0].Snippet, "CAFÉ")
	}
}

func TestSearchQueryPrefix(t *testing.T) {
	t.Parallel()
	var captured string
	emb := fakeEmbedder{vec: fixedVec(), lastText: &captured}
	srv, _ := newSearchServerCfg(t, emb, config.Config{EmbeddingsQueryPrefix: "search_query: "})
	hdr := authHeader(t, srv, "o@example.com")

	_ = searchMatches(t, srv, hdr, "roadmap", nil)

	if want := "search_query: roadmap"; captured != want {
		t.Fatalf("embedded query = %q, want %q", captured, want)
	}
}

func TestSearchOwnerScoped(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	ctx := context.Background()
	ownerHdr := authHeader(t, srv, "owner@example.com")

	mine := createNote(t, srv, ownerHdr, "shared keyword note")

	other, _ := st.CreateUser(ctx, "other@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	theirs := createNote(t, srv, otherHdr, "shared keyword note")
	if err := st.UpsertEmbedding(ctx, theirs, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	matches := searchMatches(t, srv, ownerHdr, "shared", nil)
	if !hasMatchType(matchesForNote(matches, mine), "title") {
		t.Fatalf("owner search = %+v, want own note %s", matches, mine)
	}
	if len(matchesForNote(matches, theirs)) != 0 {
		t.Fatalf("owner search = %+v, must not contain other user's note %s", matches, theirs)
	}
}

func TestSearchHybridPlaceholderAndTitleBoost(t *testing.T) {
	t.Parallel()
	vec := fixedVec()
	srv, st := newSearchServer(t, fakeEmbedder{vec: vec})
	ctx := context.Background()
	hdr := authHeader(t, srv, "o@example.com")

	titleMatch := createNote(t, srv, hdr, "Rocket science overview")
	if err := st.UpsertEmbedding(ctx, titleMatch, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding titleMatch: %v", err)
	}

	bodyOnly := createNote(t, srv, hdr, "Weekly standup notes")
	if _, err := st.Pool().Exec(context.Background(), `UPDATE note_bodies SET content=$1 WHERE note_id=$2`, "rocket launch details", bodyOnly); err != nil {
		t.Fatalf("update note body: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, bodyOnly, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding bodyOnly: %v", err)
	}

	matches := searchMatches(t, srv, hdr, "rocket", nil)
	if firstNoteID(matches) != titleMatch {
		t.Fatalf("hybrid search order = %+v, want boosted title-match note first", matches)
	}
	if got := matchesForNote(matches, bodyOnly); len(got) != 1 || got[0].MatchType != "title" || got[0].Snippet != "" {
		t.Fatalf("body-only fallback = %+v, want one placeholder title match", got)
	}
	if got := matchesForNote(matches, titleMatch); len(got) != 1 || got[0].MatchType != "title" {
		t.Fatalf("title-match fallback = %+v, want one title hit", got)
	}
}
