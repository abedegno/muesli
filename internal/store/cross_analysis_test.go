package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func crossReadyNote(t *testing.T, st *store.Store, ownerID, title string, segs []model.Segment) model.Note {
	t.Helper()
	ctx := context.Background()
	n, err := st.CreateNote(ctx, ownerID, title)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            n.ID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments:          segs,
	}, 0); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, n.ID, model.NoteReady); err != nil {
		t.Fatalf("SetNoteStatus: %v", err)
	}
	return n
}

func twoSegs(prefix string) []model.Segment {
	return []model.Segment{
		{StartMS: 0, EndMS: 1000, Text: prefix + " one"},
		{StartMS: 1000, EndMS: 2000, Text: prefix + " two"},
	}
}

// TestLoadCrossAnalysisSnapshotOrderAndGenerations proves the snapshot
// preserves REQUEST order (not creation or DB order) and captures each
// document's current transcript generation.
func TestLoadCrossAnalysisSnapshotOrderAndGenerations(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "Alpha", twoSegs("alpha"))
	b := crossReadyNote(t, st, owner, "Beta", twoSegs("beta"))

	// Request order deliberately reversed from creation order.
	snap, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{b.ID, a.ID}, 40)
	if err != nil {
		t.Fatalf("LoadCrossAnalysisSnapshot: %v", err)
	}
	if len(snap.Documents) != 2 || snap.Documents[0].NoteID != b.ID || snap.Documents[1].NoteID != a.ID {
		t.Fatalf("expected request order preserved, got %+v", snap.Documents)
	}
	if snap.Generations[a.ID] != 1 || snap.Generations[b.ID] != 1 {
		t.Fatalf("expected generation 1 for both fresh transcripts, got %+v", snap.Generations)
	}
	if len(snap.Documents[0].Segments) != 2 || len(snap.Documents[1].Segments) != 2 {
		t.Fatalf("expected 2 segments per document, got %+v", snap.Documents)
	}
}

func TestLoadCrossAnalysisSnapshotCardinality(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "Solo", twoSegs("solo"))

	// 1 note: below the minimum.
	if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{a.ID}, 40); err != store.ErrCrossAnalysisTooFewNotes {
		t.Fatalf("1 note: got %v, want ErrCrossAnalysisTooFewNotes", err)
	}

	// 41 notes: above the default maximum (40).
	ids := make([]string, 0, 41)
	for i := 0; i < 41; i++ {
		n := crossReadyNote(t, st, owner, fmt.Sprintf("Note %d", i), twoSegs(fmt.Sprintf("n%d", i)))
		ids = append(ids, n.ID)
	}
	if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, ids, 40); err != store.ErrCrossAnalysisTooManyNotes {
		t.Fatalf("41 notes: got %v, want ErrCrossAnalysisTooManyNotes", err)
	}

	// A configured ceiling below the default (413 territory) also rejects.
	twoIDs := ids[:2]
	if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, append(twoIDs, ids[2]), 2); err != store.ErrCrossAnalysisTooManyNotes {
		t.Fatalf("configured ceiling: got %v, want ErrCrossAnalysisTooManyNotes", err)
	}
}

func TestLoadCrossAnalysisSnapshotDuplicates(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	a := crossReadyNote(t, st, owner, "Dup", twoSegs("dup"))

	if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{a.ID, a.ID}, 40); err != store.ErrCrossAnalysisDuplicateNotes {
		t.Fatalf("got %v, want ErrCrossAnalysisDuplicateNotes", err)
	}
}

// TestLoadCrossAnalysisSnapshotInvisibleCollapsesToNotFound proves a
// missing id and another owner's note both collapse to the same generic
// ErrNotFound -- never distinguishable from one another.
func TestLoadCrossAnalysisSnapshotInvisibleCollapsesToNotFound(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	other := addUser(t, st)

	mine := crossReadyNote(t, st, owner, "Mine", twoSegs("mine"))
	theirs := crossReadyNote(t, st, other, "Theirs", twoSegs("theirs"))

	if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{mine.ID, theirs.ID}, 40); err != store.ErrNotFound {
		t.Fatalf("other-owner note: got %v, want ErrNotFound", err)
	}
	if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{mine.ID, "00000000-0000-0000-0000-000000000000"}, 40); err != store.ErrNotFound {
		t.Fatalf("nonexistent note: got %v, want ErrNotFound", err)
	}
}

func TestLoadCrossAnalysisSnapshotEligibility(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	ready := crossReadyNote(t, st, owner, "Ready", twoSegs("ready"))

	t.Run("not ready", func(t *testing.T) {
		notReady, err := st.CreateNote(ctx, owner, "Not ready")
		if err != nil {
			t.Fatalf("CreateNote: %v", err)
		}
		if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{ready.ID, notReady.ID}, 40); err != store.ErrCrossAnalysisNoteNotEligible {
			t.Fatalf("got %v, want ErrCrossAnalysisNoteNotEligible", err)
		}
	})

	t.Run("transcript-less", func(t *testing.T) {
		noTranscript, err := st.CreateNote(ctx, owner, "No transcript")
		if err != nil {
			t.Fatalf("CreateNote: %v", err)
		}
		if err := st.SetNoteStatus(ctx, noTranscript.ID, model.NoteReady); err != nil {
			t.Fatalf("SetNoteStatus: %v", err)
		}
		if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{ready.ID, noTranscript.ID}, 40); err != store.ErrCrossAnalysisNoteNotEligible {
			t.Fatalf("got %v, want ErrCrossAnalysisNoteNotEligible", err)
		}
	})

	t.Run("partial", func(t *testing.T) {
		partial := crossReadyNote(t, st, owner, "Partial", twoSegs("partial"))
		if err := st.SetNotePartialTranscript(ctx, partial.ID, true); err != nil {
			t.Fatalf("SetNotePartialTranscript: %v", err)
		}
		if _, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{ready.ID, partial.ID}, 40); err != store.ErrCrossAnalysisNoteNotEligible {
			t.Fatalf("got %v, want ErrCrossAnalysisNoteNotEligible", err)
		}
	})
}

// TestLoadCrossAnalysisSnapshotAppliesAliasesNonMutating proves aliases are
// applied to snapshot segments without a persisted transcript_segments row
// changing (read-time substitution only, mirroring the note-scoped chat
// path).
func TestLoadCrossAnalysisSnapshotAppliesAliasesNonMutating(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	segs := []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hello", Speaker: "SPEAKER_00"}}
	a := crossReadyNote(t, st, owner, "Aliased", segs)
	b := crossReadyNote(t, st, owner, "Other", twoSegs("other"))
	if err := st.UpsertSpeakerAlias(ctx, owner, a.ID, "SPEAKER_00", "Alice"); err != nil {
		t.Fatalf("UpsertSpeakerAlias: %v", err)
	}

	snap, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{a.ID, b.ID}, 40)
	if err != nil {
		t.Fatalf("LoadCrossAnalysisSnapshot: %v", err)
	}
	if snap.Documents[0].Segments[0].Speaker != "Alice" {
		t.Fatalf("expected alias applied, got %+v", snap.Documents[0].Segments[0])
	}

	// The stored transcript segment itself is untouched.
	raw, err := st.GetTranscript(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if raw.Segments[0].Speaker != "SPEAKER_00" {
		t.Fatalf("expected stored segment unchanged, got %+v", raw.Segments[0])
	}
}

// TestVerifyCrossAnalysisGenerations proves a generation captured at
// snapshot time that has since changed (a retranscription) is detected in
// one query, and an unchanged set passes.
func TestVerifyCrossAnalysisGenerations(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "A", twoSegs("a"))
	b := crossReadyNote(t, st, owner, "B", twoSegs("b"))

	snap, err := st.LoadCrossAnalysisSnapshot(ctx, owner, []string{a.ID, b.ID}, 40)
	if err != nil {
		t.Fatalf("LoadCrossAnalysisSnapshot: %v", err)
	}
	if err := st.VerifyCrossAnalysisGenerations(ctx, snap.Generations); err != nil {
		t.Fatalf("expected unchanged generations to verify, got %v", err)
	}

	// Replace A's transcript (bumps its generation) between snapshot and
	// verification -- the immediately-pre-invocation check must catch it.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID: a.ID, TranscriberPlugin: "whisper", Model: "base", Segments: twoSegs("a-v2"),
	}, 1); err != nil {
		t.Fatalf("re-save transcript: %v", err)
	}

	if err := st.VerifyCrossAnalysisGenerations(ctx, snap.Generations); err != store.ErrGenerationMismatch {
		t.Fatalf("got %v, want ErrGenerationMismatch", err)
	}
}
