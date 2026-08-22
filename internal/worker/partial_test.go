package worker_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
)

// TestPartialPluginResponse verifies that when the plugin returns Partial=true
// with segments, the pipeline: saves the segments, sets partial_transcript=true
// on the note, and transitions the note to failed (non-retryable terminal failure).
func TestPartialPluginResponse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := plugintest.NewPartialTranscriber("gpu oom")
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", tr)

	drain(t, proc, st)

	// Note must be failed (handleTerminalFailure ran).
	got, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if got.Status != model.NoteFailed {
		t.Errorf("note status = %q, want %q", got.Status, model.NoteFailed)
	}
	if !got.PartialTranscript {
		t.Error("PartialTranscript should be true after a partial result")
	}

	// Transcript must have been saved with the 2 partial segments.
	tr2, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(tr2.Segments) != 2 {
		t.Errorf("saved %d segments, want 2", len(tr2.Segments))
	}
}

// TestRetryNoteClearsPartialTranscript verifies that RetryNote atomically clears
// partial_transcript=false along with resetting the note status to uploaded.
func TestRetryNoteClearsPartialTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Use a plain transcriber; we don't need to run the pipeline.
	tr := plugintest.NewTranscriber()
	_, st, noteID, _, _ := pipelineFixtureWith(t, "keep", tr)

	// Directly set partial_transcript=true to simulate a prior partial run.
	if err := st.SetNotePartialTranscript(ctx, noteID, true); err != nil {
		t.Fatalf("SetNotePartialTranscript: %v", err)
	}
	// Confirm it's set.
	got, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after set: %v", err)
	}
	if !got.PartialTranscript {
		t.Fatal("expected PartialTranscript=true after direct set")
	}

	// Call RetryNote to simulate user retry action.
	if err := st.RetryNote(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("RetryNote: %v", err)
	}

	// partial_transcript must be cleared and status must be uploaded.
	after, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after retry: %v", err)
	}
	if after.PartialTranscript {
		t.Error("PartialTranscript should be false after RetryNote")
	}
	if after.Status != model.NoteUploaded {
		t.Errorf("note status after RetryNote = %q, want %q", after.Status, model.NoteUploaded)
	}
}

// TestFirstChunkFailureNoPartialFlag verifies that a clean HTTP 500 from the
// plugin (no segments at all) does NOT set partial_transcript.
func TestFirstChunkFailureNoPartialFlag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := plugintest.NewTranscriber()
	tr.FailNext(999) // always fail → terminal after MaxJobAttempts
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", tr)

	drain(t, proc, st)

	got, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if got.Status != model.NoteFailed {
		t.Errorf("note status = %q, want %q", got.Status, model.NoteFailed)
	}
	if got.PartialTranscript {
		t.Error("PartialTranscript should be false for a plain HTTP-500 failure (no segments)")
	}
}

// TestFullSuccessAfterPartialRetry verifies the end-to-end retry path:
// partial run → RetryNote (clears partial flag) → successful full run → note ready.
func TestFullSuccessAfterPartialRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Step 1: run a partial transcription.
	partialTr := plugintest.NewPartialTranscriber("gpu oom")
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", partialTr)
	drain(t, proc, st)

	got, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after partial: %v", err)
	}
	if !got.PartialTranscript {
		t.Fatal("expected PartialTranscript=true after partial run")
	}
	if got.Status != model.NoteFailed {
		t.Fatalf("expected failed status after partial, got %q", got.Status)
	}

	// Step 2: call RetryNote to reset status + clear partial flag.
	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID for audio key: %v", err)
	}
	// The failed run's own transcribe job already partially saved a transcript
	// (generation 1) before failing, so the retry must carry the generation
	// observed now, not 0 — mirroring what handleRetryNote does in production.
	expectedGeneration, err := st.CurrentTranscriptGeneration(ctx, noteID)
	if err != nil {
		t.Fatalf("CurrentTranscriptGeneration: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"audio_key": n.AudioObjectKey, "expected_generation": expectedGeneration})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := st.RetryNote(ctx, noteID, model.JobTranscribe, payload); err != nil {
		t.Fatalf("RetryNote: %v", err)
	}

	// Confirm RetryNote cleared partial_transcript.
	after, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after RetryNote: %v", err)
	}
	if after.PartialTranscript {
		t.Error("PartialTranscript should be false after RetryNote")
	}
	if after.Status != model.NoteUploaded {
		t.Errorf("status after RetryNote = %q, want %q", after.Status, model.NoteUploaded)
	}

	// Step 3: switch the plugin to a success transcriber and drain.
	successTr := plugintest.NewTranscriber()
	t.Cleanup(successTr.Close)
	cr := testCrypto(t)
	plug, err := st.DefaultPlugin(ctx, cr, model.PluginTranscriber)
	if err != nil {
		t.Fatalf("DefaultPlugin: %v", err)
	}
	plug.EndpointURL = successTr.URL()
	if err := st.UpdatePlugin(ctx, cr, plug); err != nil {
		t.Fatalf("UpdatePlugin: %v", err)
	}

	drain(t, proc, st)

	// Note must be ready, partial_transcript=false.
	final, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID final: %v", err)
	}
	if final.PartialTranscript {
		t.Error("PartialTranscript should be false after successful retry")
	}
	if final.Status != model.NoteReady {
		t.Errorf("note status after full retry = %q, want %q", final.Status, model.NoteReady)
	}
}
