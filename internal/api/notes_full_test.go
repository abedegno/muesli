package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestGetNoteFull(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr})
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Empty note: transcript null, summaries empty, status draft.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("full status %d body %s", rec.Code, rec.Body)
	}
	var full struct {
		Note         map[string]any   `json:"note"`
		BodyMarkdown string           `json:"body_markdown"`
		Transcript   *json.RawMessage `json:"transcript"`
		Summaries    []map[string]any `json:"summaries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &full)
	if full.Note["status"] != "draft" {
		t.Fatalf("status = %v", full.Note["status"])
	}
	if full.Transcript != nil {
		t.Fatalf("expected null transcript, got %s", *full.Transcript)
	}
	if len(full.Summaries) != 0 {
		t.Fatalf("expected no summaries, got %d", len(full.Summaries))
	}

	// Populate transcript + a ready summary, then re-fetch.
	_ = st.SeedBuiltInTemplates(ctx)
	tmpls, _ := st.BuiltInTemplates(ctx)
	_, _ = st.SaveTranscript(ctx, model.Transcript{
		NoteID: note.ID, TranscriberPlugin: "stub", Model: "m",
		Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic"}},
	}, 0)
	sumID, _ := st.CreatePendingSummary(ctx, note.ID, tmpls[0].ID)
	_ = st.CompleteSummary(ctx, sumID, "ollama", "llama3", []model.SummarySection{
		{Heading: "Overview", ContentMarkdown: "It happened."},
	}, false)

	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, hdr)
	var full2 struct {
		Transcript struct {
			Segments []map[string]any `json:"segments"`
		} `json:"transcript"`
		Summaries []struct {
			TemplateName string           `json:"template_name"`
			Status       string           `json:"status"`
			Sections     []map[string]any `json:"sections"`
		} `json:"summaries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &full2)
	if len(full2.Transcript.Segments) != 1 {
		t.Fatalf("segments = %d", len(full2.Transcript.Segments))
	}
	if len(full2.Summaries) != 1 || full2.Summaries[0].Status != "ready" || full2.Summaries[0].TemplateName == "" {
		t.Fatalf("summaries wrong: %+v", full2.Summaries)
	}

	// Cross-owner / unknown note → 404.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/00000000-0000-0000-0000-000000000000/full", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown note status %d, want 404", rec.Code)
	}
}

func TestGetNoteFullSpeakerAliasSubstitution(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr})
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "speaker@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "speaker@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Speaker Test"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Insert transcript with SPEAKER_00.
	_, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "stub",
		Model:             "m",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "Hello", Source: "mic", Speaker: "SPEAKER_00"},
		},
	}, 0)
	if err != nil {
		t.Fatalf("save transcript: %v", err)
	}

	// Create alias SPEAKER_00 → Alice via API.
	put := doJSON(t, srv, http.MethodPut, "/api/notes/"+note.ID+"/speaker-aliases/SPEAKER_00",
		map[string]string{"alias_name": "Alice"}, hdr)
	if put.Code != http.StatusOK {
		t.Fatalf("upsert alias: status=%d body=%s", put.Code, put.Body)
	}

	// GET /full: returned segment should have speaker = "Alice".
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get full: status=%d body=%s", rec.Code, rec.Body)
	}
	var full struct {
		Transcript struct {
			Segments []struct {
				Speaker string `json:"speaker"`
			} `json:"segments"`
		} `json:"transcript"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &full)
	if len(full.Transcript.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(full.Transcript.Segments))
	}
	if full.Transcript.Segments[0].Speaker != "Alice" {
		t.Errorf("response segment speaker = %q, want Alice", full.Transcript.Segments[0].Speaker)
	}

	// The transcript_segments table must still have SPEAKER_00, not Alice.
	rows, err := st.Pool().Query(ctx,
		`SELECT speaker FROM transcript_segments ts
		 JOIN transcripts t ON t.id = ts.transcript_id
		 WHERE t.note_id = $1`, note.ID)
	if err != nil {
		t.Fatalf("query transcript_segments: %v", err)
	}
	defer rows.Close()
	var dbSpeakers []string
	for rows.Next() {
		var sp string
		if err := rows.Scan(&sp); err != nil {
			t.Fatalf("scan: %v", err)
		}
		dbSpeakers = append(dbSpeakers, sp)
	}
	if len(dbSpeakers) != 1 || dbSpeakers[0] != "SPEAKER_00" {
		t.Errorf("transcript_segments speaker = %v, want [SPEAKER_00] (must not be mutated)", dbSpeakers)
	}
}
