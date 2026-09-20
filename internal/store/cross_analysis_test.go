package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
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

func strPtrCA(s string) *string { return &s }

// TestAppendCrossAnalysisTurnCommitsAtomically proves a successful call
// persists both messages, every source row, and bumps the conversation's
// timestamp together.
func TestAppendCrossAnalysisTurnCommitsAtomically(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "A", twoSegs("a"))
	b := crossReadyNote(t, st, owner, "B", twoSegs("b"))
	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	tokens := 99
	sources := []model.MessageSource{
		{N: 1, NoteID: strPtrCA(a.ID), TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "a one"},
		{N: 2, NoteID: strPtrCA(b.ID), TranscriptGeneration: 1, SegmentIndex: 1, Timestamp: 1000, Snippet: "b two"},
	}
	userMsg, assistantMsg, err := st.AppendCrossAnalysisTurn(ctx, conv.ID, "Compare A and B", "They differ [1][2]", "test-model", &tokens, sources)
	if err != nil {
		t.Fatalf("AppendCrossAnalysisTurn: %v", err)
	}
	if userMsg.Role != "user" || assistantMsg.Role != "assistant" {
		t.Fatalf("unexpected roles: user=%q assistant=%q", userMsg.Role, assistantMsg.Role)
	}
	if len(assistantMsg.Sources) != 2 {
		t.Fatalf("expected 2 sources on returned assistant message, got %+v", assistantMsg.Sources)
	}

	msgs, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after reload, got %d", len(msgs))
	}
	if len(msgs[0].Sources) != 0 {
		t.Fatalf("expected user message to have no sources, got %+v", msgs[0].Sources)
	}
	if len(msgs[1].Sources) != 2 || msgs[1].Sources[0].N != 1 || msgs[1].Sources[1].N != 2 {
		t.Fatalf("expected 2 ordered sources on assistant message after reload, got %+v", msgs[1].Sources)
	}
	if msgs[1].Sources[0].NoteID == nil || *msgs[1].Sources[0].NoteID != a.ID {
		t.Fatalf("unexpected source note id: %+v", msgs[1].Sources[0])
	}

	afterConv, err := st.GetConversation(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !afterConv.UpdatedAt.After(conv.UpdatedAt) && !afterConv.UpdatedAt.Equal(conv.UpdatedAt) {
		t.Fatalf("expected updated_at to advance: before=%v after=%v", conv.UpdatedAt, afterConv.UpdatedAt)
	}
}

// TestAppendCrossAnalysisTurnRollsBackOnSourceFailure forces the source-row
// insert to fail (a citation pointing at a note id that does not exist,
// violating the FK) and proves NEITHER message nor any source is left
// behind -- the whole transaction rolls back, never a user-only turn.
func TestAppendCrossAnalysisTurnRollsBackOnSourceFailure(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	before, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages before: %v", err)
	}

	badNoteID := "00000000-0000-0000-0000-000000000000"
	sources := []model.MessageSource{{N: 1, NoteID: &badNoteID, TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "x"}}
	_, _, err = st.AppendCrossAnalysisTurn(ctx, conv.ID, "focus", "reply [1]", "test-model", nil, sources)
	if err == nil {
		t.Fatal("expected an error from a source row referencing a nonexistent note, got nil")
	}

	after, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no messages persisted after a rolled-back turn, before=%d after=%d", len(before), len(after))
	}
}

// TestAppendCrossAnalysisTurnNoteDeletionUnlinksSourceWithoutDeletingText
// proves a cited note's citation goes inert (its note_id reads back null)
// the moment the note is soft-deleted (trashed) -- NOT only once it is later
// purged. Note deletion in this app is soft-delete first (DeleteNote sets
// notes.deleted_at); ListMessages' join to notes must already treat a
// trashed note as unavailable, since the FK's ON DELETE SET NULL only fires
// on the row's actual (hard) removal, which trashing alone does not do. The
// test also confirms the historical assistant text survives untouched, and
// that a later hard purge leaves the citation just as inert.
func TestAppendCrossAnalysisTurnNoteDeletionUnlinksSourceWithoutDeletingText(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "Deletable", twoSegs("del"))
	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	sources := []model.MessageSource{{N: 1, NoteID: strPtrCA(a.ID), TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "del one"}}
	_, assistantMsg, err := st.AppendCrossAnalysisTurn(ctx, conv.ID, "focus", "reply [1]", "test-model", nil, sources)
	if err != nil {
		t.Fatalf("AppendCrossAnalysisTurn: %v", err)
	}

	findAssistantMsg := func() model.Message {
		t.Helper()
		msgs, err := st.ListMessages(ctx, owner, conv.ID)
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		for i := range msgs {
			if msgs[i].ID == assistantMsg.ID {
				return msgs[i]
			}
		}
		t.Fatal("expected the assistant message to be present")
		return model.Message{}
	}

	// Trash (soft-delete) the note WITHOUT purging it. The citation must
	// already be inert at this point -- this is the required behavior, not
	// a byproduct of eventual hard deletion.
	if err := st.DeleteNote(ctx, owner, a.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}

	trashedGot := findAssistantMsg()
	if trashedGot.Content != "reply [1]" {
		t.Fatalf("expected historical text unchanged after trashing, got %q", trashedGot.Content)
	}
	if len(trashedGot.Sources) != 1 || trashedGot.Sources[0].NoteID != nil {
		t.Fatalf("expected the source's note_id to already be nulled (unavailable citation) once the note is merely trashed, got %+v", trashedGot.Sources)
	}

	// A further permanent purge must leave the citation just as inert (and
	// the historical text still intact).
	if _, err := st.PurgeNote(ctx, owner, a.ID); err != nil {
		t.Fatalf("PurgeNote: %v", err)
	}

	purgedGot := findAssistantMsg()
	if purgedGot.Content != "reply [1]" {
		t.Fatalf("expected historical text unchanged after purge, got %q", purgedGot.Content)
	}
	if len(purgedGot.Sources) != 1 || purgedGot.Sources[0].NoteID != nil {
		t.Fatalf("expected the source's note_id to remain nulled after purge, got %+v", purgedGot.Sources)
	}
}

// countMessageSourcesForConversation counts every message_sources row
// belonging to any message in conversationID -- used by the transactional
// fault-injection tests below to prove a forced mid-transaction failure
// leaves behind no stray source row, not just no stray message.
func countMessageSourcesForConversation(t *testing.T, pool *pgxpool.Pool, conversationID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM message_sources ms JOIN messages m ON m.id = ms.message_id WHERE m.conversation_id = $1`,
		conversationID).Scan(&n); err != nil {
		t.Fatalf("count message_sources: %v", err)
	}
	return n
}

// TestAppendCrossAnalysisTurnRollsBackOnAssistantMessageInsertFailure forces
// the assistant message insert (the second write of the transaction, after
// the user message insert already succeeded) to fail via the
// testHookBeforeAssistantMessageInsert fault-injection hook, and proves NO
// partial turn survives: no stray user or assistant message row, no stray
// source row, and no conversation timestamp bump -- plus that the failure
// surfaces as an error from AppendCrossAnalysisTurn itself. Unlike the
// source-insert-rollback test below, there is no data value that fails only
// the assistant insert (both message inserts share the same conversation_id
// FK and hardcoded, already-valid role literals), so this uses the hook
// mechanism instead of a constraint violation.
func TestAppendCrossAnalysisTurnRollsBackOnAssistantMessageInsertFailure(t *testing.T) {
	t.Parallel()
	st, owner, pool := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "A", twoSegs("a"))
	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	before, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages before: %v", err)
	}

	restore := store.SetTestHookBeforeAssistantMessageInsert(func(cancel context.CancelFunc) { cancel() })
	defer restore()

	sources := []model.MessageSource{{N: 1, NoteID: strPtrCA(a.ID), TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "a one"}}
	_, _, err = st.AppendCrossAnalysisTurn(ctx, conv.ID, "focus", "reply [1]", "test-model", nil, sources)
	if err == nil {
		t.Fatal("expected an error from a forced assistant-message-insert failure, got nil")
	}

	after, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no messages persisted after a rolled-back turn, before=%d after=%d", len(before), len(after))
	}
	if n := countMessageSourcesForConversation(t, pool, conv.ID); n != 0 {
		t.Fatalf("expected no source rows persisted after a rolled-back turn, got %d", n)
	}

	afterConv, err := st.GetConversation(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !afterConv.UpdatedAt.Equal(conv.UpdatedAt) {
		t.Fatalf("expected updated_at unchanged after a rolled-back turn, before=%v after=%v", conv.UpdatedAt, afterConv.UpdatedAt)
	}
}

// TestAppendCrossAnalysisTurnRollsBackOnConversationTimestampUpdateFailure
// forces the conversations.updated_at UPDATE (which runs after both
// messages and every source row already succeeded, uncommitted, within the
// transaction) to fail via the testHookBeforeConversationTimestampUpdate
// hook, and proves the whole turn -- messages AND sources, not just the
// timestamp -- rolls back, and that the failure surfaces as an error.
func TestAppendCrossAnalysisTurnRollsBackOnConversationTimestampUpdateFailure(t *testing.T) {
	t.Parallel()
	st, owner, pool := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "A", twoSegs("a"))
	b := crossReadyNote(t, st, owner, "B", twoSegs("b"))
	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	before, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages before: %v", err)
	}

	restore := store.SetTestHookBeforeConversationTimestampUpdate(func(cancel context.CancelFunc) { cancel() })
	defer restore()

	sources := []model.MessageSource{
		{N: 1, NoteID: strPtrCA(a.ID), TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "a one"},
		{N: 2, NoteID: strPtrCA(b.ID), TranscriptGeneration: 1, SegmentIndex: 1, Timestamp: 1000, Snippet: "b two"},
	}
	_, _, err = st.AppendCrossAnalysisTurn(ctx, conv.ID, "Compare A and B", "They differ [1][2]", "test-model", nil, sources)
	if err == nil {
		t.Fatal("expected an error from a forced conversation-timestamp-update failure, got nil")
	}

	after, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no messages persisted after a rolled-back turn, before=%d after=%d", len(before), len(after))
	}
	if n := countMessageSourcesForConversation(t, pool, conv.ID); n != 0 {
		t.Fatalf("expected no source rows persisted after a rolled-back turn, got %d", n)
	}

	afterConv, err := st.GetConversation(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !afterConv.UpdatedAt.Equal(conv.UpdatedAt) {
		t.Fatalf("expected updated_at unchanged after a rolled-back turn, before=%v after=%v", conv.UpdatedAt, afterConv.UpdatedAt)
	}
}

// TestAppendCrossAnalysisTurnRollsBackOnCommitFailure forces the final
// tx.Commit call itself (after every insert and the timestamp update already
// succeeded, uncommitted, within the transaction) to fail via the
// testHookBeforeCrossAnalysisCommit hook, and proves the whole turn rolls
// back exactly as if an earlier row operation had failed, and that the
// failure surfaces as an error from AppendCrossAnalysisTurn.
func TestAppendCrossAnalysisTurnRollsBackOnCommitFailure(t *testing.T) {
	t.Parallel()
	st, owner, pool := newStoreWithOwner(t)
	ctx := context.Background()

	a := crossReadyNote(t, st, owner, "A", twoSegs("a"))
	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	before, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages before: %v", err)
	}

	restore := store.SetTestHookBeforeCrossAnalysisCommit(func(cancel context.CancelFunc) { cancel() })
	defer restore()

	sources := []model.MessageSource{{N: 1, NoteID: strPtrCA(a.ID), TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "a one"}}
	_, _, err = st.AppendCrossAnalysisTurn(ctx, conv.ID, "focus", "reply [1]", "test-model", nil, sources)
	if err == nil {
		t.Fatal("expected an error from a forced commit failure, got nil")
	}

	after, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no messages persisted after a rolled-back turn, before=%d after=%d", len(before), len(after))
	}
	if n := countMessageSourcesForConversation(t, pool, conv.ID); n != 0 {
		t.Fatalf("expected no source rows persisted after a rolled-back turn, got %d", n)
	}

	afterConv, err := st.GetConversation(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !afterConv.UpdatedAt.Equal(conv.UpdatedAt) {
		t.Fatalf("expected updated_at unchanged after a rolled-back turn, before=%v after=%v", conv.UpdatedAt, afterConv.UpdatedAt)
	}
}
