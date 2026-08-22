package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
)

// TestSummarizeTemplateAPI covers POST /api/notes/{id}/templates/{templateID}/summarize:
// happy path (regenerates exactly one template's summary, leaves the others alone,
// enqueues exactly one job), unknown/foreign template id -> 404, cross-owner note ->
// 404, no transcript -> 409, and invalid ids -> 404.
func TestSummarizeTemplateAPI(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "tpl01@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "tpl01@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note via the API.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Retro"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatalf("no note id; body %s", rec.Body)
	}

	ownerID, err := st.NoteOwnerID(ctx, note.ID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	targetTmpl, err := st.CreateTemplate(ctx, ownerID, "Manual only", "after",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil || len(tmpls) < 1 {
		t.Fatalf("BuiltInTemplates: %v (%d templates)", err, len(tmpls))
	}
	otherTmpl := tmpls[0]

	// Without a transcript -> 409.
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/"+note.ID+"/templates/"+targetTmpl.ID+"/summarize", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("summarize-template without transcript status %d, want 409; body %s", rec.Code, rec.Body)
	}

	// Unknown/foreign template id on a note that ALSO has no transcript yet must
	// still 404 (template-visibility precedes the transcript check).
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/"+note.ID+"/templates/00000000-0000-0000-0000-000000000000/summarize", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown template on a no-transcript note status %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Give it a transcript, plus pre-existing summaries for both templates.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test",
		Model:             "m",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	staleID, err := st.CreatePendingSummary(ctx, note.ID, targetTmpl.ID)
	if err != nil {
		t.Fatalf("CreatePendingSummary target: %v", err)
	}
	otherID, err := st.CreatePendingSummary(ctx, note.ID, otherTmpl.ID)
	if err != nil {
		t.Fatalf("CreatePendingSummary other: %v", err)
	}
	if err := st.CompleteSummary(ctx, otherID, "plugin", "model", []model.SummarySection{{Heading: "H", ContentMarkdown: "c"}}, false); err != nil {
		t.Fatalf("CompleteSummary other: %v", err)
	}

	// Unknown/foreign template id -> 404 (no state changed).
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/"+note.ID+"/templates/00000000-0000-0000-0000-000000000000/summarize", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown template status %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Happy path -> 202, regenerates exactly the target template's summary.
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/"+note.ID+"/templates/"+targetTmpl.ID+"/summarize", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("summarize-template status %d, want 202; body %s", rec.Code, rec.Body)
	}
	var resp struct{ Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "summarizing" {
		t.Fatalf("summarize-template body status = %q, want summarizing", resp.Status)
	}

	sums, err := st.GetSummaries(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(sums) != 2 {
		t.Fatalf("summaries after summarize-template = %d, want 2", len(sums))
	}
	var sawFreshTarget, sawOtherUntouched bool
	for _, s := range sums {
		if s.ID == staleID {
			t.Fatalf("stale summary %s for target template should have been deleted", staleID)
		}
		if s.TemplateID == targetTmpl.ID {
			sawFreshTarget = true
			if s.Status != model.SummaryPending {
				t.Fatalf("fresh target summary status = %q, want pending", s.Status)
			}
		}
		if s.ID == otherID {
			sawOtherUntouched = true
			if s.Status != model.SummaryReady {
				t.Fatalf("other template's summary was touched: status = %q, want ready", s.Status)
			}
		}
	}
	if !sawFreshTarget {
		t.Fatalf("expected a fresh pending summary for target template")
	}
	if !sawOtherUntouched {
		t.Fatalf("other template's existing summary should be untouched")
	}

	// Exactly one active summarize job was enqueued.
	activeJobs, err := st.CountActiveSummarizeJobs(ctx, note.ID)
	if err != nil {
		t.Fatalf("CountActiveSummarizeJobs: %v", err)
	}
	if activeJobs != 1 {
		t.Fatalf("active summarize jobs = %d, want 1", activeJobs)
	}

	// Cross-owner -> 404.
	other, _ := st.CreateUser(ctx, "other-tpl01@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/"+note.ID+"/templates/"+targetTmpl.ID+"/summarize", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner summarize-template status %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Invalid note id -> 404.
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/not-a-uuid/templates/"+targetTmpl.ID+"/summarize", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invalid note id status %d, want 404", rec.Code)
	}

	// Invalid template id -> 404.
	rec = doJSON(t, srv, http.MethodPost,
		"/api/notes/"+note.ID+"/templates/not-a-uuid/summarize", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invalid template id status %d, want 404", rec.Code)
	}
}
