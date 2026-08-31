package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

// failingDeleteProvider wraps a real storage.Provider so Delete always fails
// with a transient-looking error while every other operation — including the
// object's real presence on disk — is untouched. This is what lets the test
// distinguish "delete failed" from "delete never really ran": Verify below
// reads the real underlying object through the same wrapped provider.
type failingDeleteProvider struct {
	storage.Provider
}

func (failingDeleteProvider) Delete(string) error {
	return errors.New("injected transient delete failure")
}

// TestAudioDiscardOrderingSurvivesADeleteFailure is the round-1 regression
// test. applyAudioRetention's discard branch (via
// SetRetentionStateDiscardedIfCurrent) must call storage.Delete BEFORE ever
// writing retention_state="discarded", and under one lock, so that a delete
// failure (a transient storage error) leaves retention_state untouched
// instead of writing "discarded" anyway. If that ordering regresses,
// retention_state flips to "discarded" even though the audio object is still
// on disk — and retranscribeConflictReason (internal/api/notes.go)
// PERMANENTLY refuses retranscription once retention_state=="discarded",
// for audio that was never actually deleted.
func TestAudioDiscardOrderingSurvivesADeleteFailure(t *testing.T) {
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}

	root := t.TempDir()
	realProv, err := storage.NewLocal(root, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	prov := failingDeleteProvider{Provider: realProv}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-discard-ordering", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: plainResumeSegments, Language: "en", Model: "stub-discard", DurationMS: 3000,
		})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: trSrv.URL, Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	_ = st.SetDefaultPlugin(ctx, tp.ID)
	_ = st.SetDefaultPlugin(ctx, ap.ID)

	u, err := st.CreateUser(ctx, "discard-order@example.com", "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Discard ordering")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	key := "notes/" + n.ID + "/audio/a.webm"
	writeStorageObject(t, root, key, []byte("audio-bytes-still-here"))
	if err := st.SetNoteAudio(ctx, u.ID, n.ID, key); err != nil {
		t.Fatalf("set note audio: %v", err)
	}

	proc := worker.NewProcessor(st, cr, prov, config.Config{AudioRetention: "discard"}, nil)

	if _, err := st.EnqueueJob(ctx, n.ID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":0}`)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	drain(t, proc, st)

	note, err := st.GetNoteByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if note.RetentionState == "discarded" {
		t.Fatal(`retention_state = "discarded" even though storage.Delete failed — audio was never actually deleted`)
	}
	if note.Status != model.NoteReady && note.Status != model.NoteFailed {
		t.Fatalf("note status = %q after drain, want a terminal status (ready or failed)", note.Status)
	}

	exists, _, err := realProv.Verify(key)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !exists {
		t.Fatal("audio object no longer exists on disk, but Delete was made to fail — test setup is broken")
	}

	// retranscribeConflictReason's eligibility rule (internal/api/notes.go):
	// AudioObjectKey set and RetentionState != "discarded".
	if note.AudioObjectKey == "" {
		t.Fatal("AudioObjectKey cleared unexpectedly — note would wrongly appear to have no stored audio")
	}
	if note.RetentionState == "discarded" {
		t.Fatal("RetentionState == \"discarded\" — note would be wrongly, permanently refused retranscription")
	}
}
