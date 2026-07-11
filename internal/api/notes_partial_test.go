package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestPartialTranscriptAPIContract pins the client-facing contract for the
// partial_transcript flag: it is ALWAYS serialised (non-omitempty) on both the
// notes list and the full-note response, so after a successful retry clears it
// clients see an explicit "partial_transcript":false — never an ambiguous
// absent field.
func TestPartialTranscriptAPIContract(t *testing.T) {
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

	// Simulate a partial transcription (chunk failure) then a successful retry
	// clearing it, exactly as the worker does via SetNotePartialTranscript.
	if err := st.SetNotePartialTranscript(ctx, note.ID, true); err != nil {
		t.Fatalf("set partial: %v", err)
	}

	// While partial: both list and full must serialise partial_transcript:true.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if !strings.Contains(rec.Body.String(), `"partial_transcript":true`) {
		t.Fatalf("list while partial: want explicit \"partial_transcript\":true, got %s", rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, hdr)
	if !strings.Contains(rec.Body.String(), `"partial_transcript":true`) {
		t.Fatalf("full while partial: want explicit \"partial_transcript\":true, got %s", rec.Body)
	}

	// Successful retry clears the flag.
	if err := st.SetNotePartialTranscript(ctx, note.ID, false); err != nil {
		t.Fatalf("clear partial: %v", err)
	}

	// After the clear: the field must be present AND explicitly false on both
	// responses (absent would be ambiguous — the exact contract this pins).
	rec = doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if !strings.Contains(rec.Body.String(), `"partial_transcript":false`) {
		t.Fatalf("list after retry: want explicit \"partial_transcript\":false, got %s", rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, hdr)
	if !strings.Contains(rec.Body.String(), `"partial_transcript":false`) {
		t.Fatalf("full after retry: want explicit \"partial_transcript\":false, got %s", rec.Body)
	}
}
