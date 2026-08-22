package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

var enqueueTestUserCounter atomic.Int64

// enqueueTestServer builds a server with local storage plus an authenticated
// owner, without going through /api/setup, so each test owns its fixtures.
func enqueueTestServer(t *testing.T) (*api.Server, *store.Store, storage.Provider, string, map[string]string) {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov})

	owner, err := st.CreateUser(ctx, fmt.Sprintf("enq%d@example.com", enqueueTestUserCounter.Add(1)), "password123")
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateToken(ctx, owner.ID, "session", hash, "session"); err != nil {
		t.Fatal(err)
	}
	return srv, st, prov, owner.ID, map[string]string{"Authorization": "Bearer " + raw}
}

// enqueuedTranscribeGeneration returns the expected_generation carried by the
// note's single pending transcribe job. It reads the row the handler actually
// wrote, so it cannot pass by reproducing the handler's own logic.
func enqueuedTranscribeGeneration(t *testing.T, st *store.Store, noteID string) int {
	t.Helper()
	rows, err := st.Pool().Query(context.Background(),
		`SELECT payload FROM jobs WHERE note_id=$1 AND type=$2`, noteID, model.JobTranscribe)
	if err != nil {
		t.Fatalf("read transcribe jobs: %v", err)
	}
	defer rows.Close()
	var payloads [][]byte
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan payload: %v", err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate payloads: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("transcribe jobs enqueued = %d, want exactly 1", len(payloads))
	}
	var decoded struct {
		ExpectedGeneration *int `json:"expected_generation"`
	}
	if err := json.Unmarshal(payloads[0], &decoded); err != nil {
		t.Fatalf("decode payload %s: %v", payloads[0], err)
	}
	if decoded.ExpectedGeneration == nil {
		t.Fatalf("payload %s carries no expected_generation", payloads[0])
	}
	return *decoded.ExpectedGeneration
}

// seedTranscriptGeneration replaces the note's transcript n times so its
// generation ends at n. Two is the useful minimum: it separates "read the
// current generation" from a hardcoded 0 AND from a hardcoded 1.
func seedTranscriptGeneration(t *testing.T, st *store.Store, noteID string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if _, err := st.SaveTranscript(ctx, model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: fmt.Sprintf("take %d", i), Source: "mic"}},
		}, i); err != nil {
			t.Fatalf("seed transcript %d: %v", i, err)
		}
	}
}

// TestAudioUploadedEnqueuesCurrentGeneration drives the real upload-completion
// handler — the flagship production path, which had no test at all: hardcoding
// its generation read to 0 left the WHOLE REPO green.
//
// A regression here is silent and permanent. An upload after a live stream
// would enqueue expected 0 against generation 1, the worker would discard the
// job as stale AND report success, and the note would sit at "uploaded" forever
// with no failure and nothing to retry.
func TestAudioUploadedEnqueuesCurrentGeneration(t *testing.T) {
	t.Parallel()
	srv, st, _, _, hdr := enqueueTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Uploaded"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	seedTranscriptGeneration(t, st, note.ID, 2)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/audio-upload-url", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("presign: %d %s", rec.Code, rec.Body)
	}
	var grant struct{ URL, Key string }
	_ = json.Unmarshal(rec.Body.Bytes(), &grant)
	put := doRaw(t, srv, http.MethodPut, strings.TrimPrefix(grant.URL, "http://example.test"), "audio-bytes",
		map[string]string{"Content-Type": "audio/webm"})
	if put.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", put.Code, put.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/audio-uploaded",
		map[string]string{"key": grant.Key}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("audio-uploaded: %d %s", rec.Code, rec.Body)
	}

	if got := enqueuedTranscribeGeneration(t, st, note.ID); got != 2 {
		t.Fatalf("enqueued expected_generation = %d, want 2 (the note's current generation)", got)
	}
}

// TestRetranscribeEnqueuesCurrentGeneration drives the real retranscribe
// handler. Hardcoding its generation read to 0 also left the whole repo green:
// the existing retranscribe tests assert status codes and conflict rules, never
// the payload.
func TestRetranscribeEnqueuesCurrentGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, st, _, ownerID, hdr := enqueueTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Retranscribe"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	seedTranscriptGeneration(t, st, note.ID, 2)
	if err := st.SetNoteAudio(ctx, ownerID, note.ID, "notes/"+note.ID+"/audio/a.webm"); err != nil {
		t.Fatalf("set audio: %v", err)
	}
	if err := st.SetNoteStatus(ctx, note.ID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/retranscribe", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retranscribe: %d %s", rec.Code, rec.Body)
	}

	if got := enqueuedTranscribeGeneration(t, st, note.ID); got != 2 {
		t.Fatalf("enqueued expected_generation = %d, want 2 (the note's current generation)", got)
	}
}
