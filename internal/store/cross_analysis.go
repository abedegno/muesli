package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

// ErrCrossAnalysisTooFewNotes is returned by LoadCrossAnalysisSnapshot when
// fewer than model.CrossAnalysisMinNotes distinct note IDs were requested.
// Maps to 400 at the API boundary.
var ErrCrossAnalysisTooFewNotes = errors.New("cross-analysis: at least two notes are required")

// ErrCrossAnalysisTooManyNotes is returned when more than the caller-supplied
// ceiling (at most model.CrossAnalysisMaxNotes) was requested. Maps to 413 --
// distinct from ErrCrossAnalysisTooFewNotes's 400, per the accepted spec's
// status mapping.
var ErrCrossAnalysisTooManyNotes = errors.New("cross-analysis: too many notes requested")

// ErrCrossAnalysisDuplicateNotes is returned when the requested note ID list
// contains a duplicate. Maps to 400.
var ErrCrossAnalysisDuplicateNotes = errors.New("cross-analysis: duplicate note ids")

// ErrCrossAnalysisNoteNotEligible is returned when a requested note exists
// and is visible to ownerID, but is not in successful ready state, has no
// durable transcript, or is partial. Maps to 400 -- distinct from
// ErrNotFound (404), which covers a missing or invisible/other-owner note
// (collapsed to the same generic sentinel so an invalid id is never an
// existence oracle -- see the accepted spec).
var ErrCrossAnalysisNoteNotEligible = errors.New("cross-analysis: note is not ready, durable, and complete")

// CrossAnalysisDocument is one loaded, eligible, alias-substituted note
// ready to feed internal/execution.PrepareDocuments.
type CrossAnalysisDocument struct {
	NoteID               string
	Title                string
	OccurredAt           *time.Time
	TranscriptGeneration int
	Segments             []model.Segment
}

// CrossAnalysisSnapshot is LoadCrossAnalysisSnapshot's immutable result: the
// requested documents in REQUEST order (never reordered, never sampled) plus
// the captured generation for each, keyed by note ID, for the
// immediately-pre-invocation re-check (VerifyCrossAnalysisGenerations).
type CrossAnalysisSnapshot struct {
	Documents   []CrossAnalysisDocument
	Generations map[string]int
}

// LoadCrossAnalysisSnapshot performs the bounded, owner-scoped preflight
// load for a cross-meeting analysis run (issue #765): validates cardinality
// and uniqueness, then loads every requested note's transcript (segments,
// alias-substituted) and generation in a small, fixed number of bulk queries
// (never one round trip per note). A duplicate ID, an ID outside
// [minNotes,maxNotes], a missing/invisible/other-owner note, or a note that
// is not ready, has no durable transcript, or is partial fails the whole
// call -- there is no partial snapshot.
func (s *Store) LoadCrossAnalysisSnapshot(ctx context.Context, ownerID string, noteIDs []string, maxNotes int) (CrossAnalysisSnapshot, error) {
	if len(noteIDs) < model.CrossAnalysisMinNotes {
		return CrossAnalysisSnapshot{}, ErrCrossAnalysisTooFewNotes
	}
	if maxNotes <= 0 || maxNotes > model.CrossAnalysisMaxNotes {
		maxNotes = model.CrossAnalysisMaxNotes
	}
	if len(noteIDs) > maxNotes {
		return CrossAnalysisSnapshot{}, ErrCrossAnalysisTooManyNotes
	}
	seen := make(map[string]struct{}, len(noteIDs))
	for _, id := range noteIDs {
		if _, dup := seen[id]; dup {
			return CrossAnalysisSnapshot{}, ErrCrossAnalysisDuplicateNotes
		}
		seen[id] = struct{}{}
	}

	type noteRow struct {
		id                string
		title             string
		status            string
		startedAt         *time.Time
		createdAt         time.Time
		partialTranscript bool
	}
	notesByID := make(map[string]noteRow, len(noteIDs))
	rows, err := s.pool.Query(ctx,
		`SELECT id, title, status, started_at, created_at, partial_transcript
		   FROM notes
		  WHERE owner_id = $1 AND id = ANY($2) AND deleted_at IS NULL`,
		ownerID, noteIDs)
	if err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	for rows.Next() {
		var r noteRow
		if err := rows.Scan(&r.id, &r.title, &r.status, &r.startedAt, &r.createdAt, &r.partialTranscript); err != nil {
			rows.Close()
			return CrossAnalysisSnapshot{}, err
		}
		notesByID[r.id] = r
	}
	if err := rows.Err(); err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	rows.Close()

	// Every requested id must resolve to an owner-visible note; a missing or
	// other-owner id collapses to the same generic ErrNotFound (never
	// disclosed as "belongs to someone else") so a guessed id is never an
	// existence oracle.
	for _, id := range noteIDs {
		if _, ok := notesByID[id]; !ok {
			return CrossAnalysisSnapshot{}, ErrNotFound
		}
	}

	type transcriptRow struct {
		id         string
		noteID     string
		generation int
	}
	transcriptByNoteID := make(map[string]transcriptRow, len(noteIDs))
	trRows, err := s.pool.Query(ctx,
		`SELECT id, note_id, generation FROM transcripts WHERE note_id = ANY($1)`, noteIDs)
	if err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	for trRows.Next() {
		var t transcriptRow
		if err := trRows.Scan(&t.id, &t.noteID, &t.generation); err != nil {
			trRows.Close()
			return CrossAnalysisSnapshot{}, err
		}
		transcriptByNoteID[t.noteID] = t
	}
	if err := trRows.Err(); err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	trRows.Close()

	// Eligibility: ready status, a durable transcript (present in
	// transcriptByNoteID), and not partial. Checked BEFORE loading segments
	// so an ineligible note never triggers the segment query at all.
	for _, id := range noteIDs {
		n := notesByID[id]
		if n.status != model.NoteReady || n.partialTranscript {
			return CrossAnalysisSnapshot{}, ErrCrossAnalysisNoteNotEligible
		}
		if _, ok := transcriptByNoteID[id]; !ok {
			return CrossAnalysisSnapshot{}, ErrCrossAnalysisNoteNotEligible
		}
	}

	transcriptIDs := make([]string, 0, len(noteIDs))
	transcriptIDToNoteID := make(map[string]string, len(noteIDs))
	for _, id := range noteIDs {
		tr := transcriptByNoteID[id]
		transcriptIDs = append(transcriptIDs, tr.id)
		transcriptIDToNoteID[tr.id] = id
	}

	segmentsByNoteID := make(map[string][]model.Segment, len(noteIDs))
	segRows, err := s.pool.Query(ctx,
		`SELECT transcript_id, id, start_ms, end_ms, text, source, COALESCE(speaker,''), words, confidence,
		        provisional, COALESCE(boundary,'')
		   FROM transcript_segments
		  WHERE transcript_id = ANY($1)
		  ORDER BY transcript_id, start_ms`, transcriptIDs)
	if err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	for segRows.Next() {
		var transcriptID string
		var seg model.Segment
		var wordsJSON []byte
		var confidence *float64
		if err := segRows.Scan(&transcriptID, &seg.ID, &seg.StartMS, &seg.EndMS, &seg.Text, &seg.Source, &seg.Speaker,
			&wordsJSON, &confidence, &seg.Provisional, &seg.Boundary); err != nil {
			segRows.Close()
			return CrossAnalysisSnapshot{}, err
		}
		seg.Confidence = confidence
		if len(wordsJSON) > 0 {
			if err := json.Unmarshal(wordsJSON, &seg.Words); err != nil {
				segRows.Close()
				return CrossAnalysisSnapshot{}, err
			}
		}
		noteID := transcriptIDToNoteID[transcriptID]
		segmentsByNoteID[noteID] = append(segmentsByNoteID[noteID], seg)
	}
	if err := segRows.Err(); err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	segRows.Close()

	documents := make([]CrossAnalysisDocument, 0, len(noteIDs))
	generations := make(map[string]int, len(noteIDs))
	for _, id := range noteIDs {
		n := notesByID[id]
		tr := transcriptByNoteID[id]

		aliases, err := s.SpeakerAliasMap(ctx, ownerID, id)
		if err != nil {
			return CrossAnalysisSnapshot{}, err
		}
		segments, _ := ApplyAliases(segmentsByNoteID[id], aliases)

		occurredAt := n.startedAt
		if occurredAt == nil {
			c := n.createdAt
			occurredAt = &c
		}

		documents = append(documents, CrossAnalysisDocument{
			NoteID:               id,
			Title:                n.title,
			OccurredAt:           occurredAt,
			TranscriptGeneration: tr.generation,
			Segments:             segments,
		})
		generations[id] = tr.generation
	}

	return CrossAnalysisSnapshot{Documents: documents, Generations: generations}, nil
}

// VerifyCrossAnalysisGenerations compares the captured generation map
// (Snapshot.Generations, taken at LoadCrossAnalysisSnapshot time) against
// each note's CURRENT transcript generation in one query, immediately before
// invocation. Returns ErrGenerationMismatch if any of them changed --
// including a transcript that no longer exists (replaced by, e.g., a
// re-transcription that briefly drops the row). A caller with no captured
// generations trivially succeeds (nothing to check).
func (s *Store) VerifyCrossAnalysisGenerations(ctx context.Context, generations map[string]int) error {
	if len(generations) == 0 {
		return nil
	}
	noteIDs := make([]string, 0, len(generations))
	for id := range generations {
		noteIDs = append(noteIDs, id)
	}

	current := make(map[string]int, len(noteIDs))
	rows, err := s.pool.Query(ctx,
		`SELECT note_id, generation FROM transcripts WHERE note_id = ANY($1)`, noteIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var noteID string
		var generation int
		if err := rows.Scan(&noteID, &generation); err != nil {
			return err
		}
		current[noteID] = generation
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for id, captured := range generations {
		got, ok := current[id]
		if !ok || got != captured {
			return ErrGenerationMismatch
		}
	}
	return nil
}

// crossAnalysisSnippetMaxRunes bounds a persisted citation snippet's length
// (the "bounded display snippet" the accepted spec's Persistence section
// requires) -- protects row/response size regardless of how long the
// originating transcript segment's text is.
const crossAnalysisSnippetMaxRunes = 500

func truncateRunesEllipsis(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// crossAnalysisFaultInjectionKey is the unexported context key a test uses
// to scope a fault-injection point to its OWN AppendCrossAnalysisTurn call
// only -- see crossAnalysisFaultInjection below and
// export_test.go's ContextWithCrossAnalysisFaultInjection. Deliberately NOT
// a package-level mutable hook variable: this file's tests run with
// t.Parallel(), and a global hook read unconditionally by every call would
// let one test's injected failure fire inside a concurrently-running
// sibling test's call, a real cross-test race. Threading the injection
// through the call's own context instead means a call made with a plain
// context.Background() (every sibling test, and every production caller)
// never observes another call's injection: ctx.Value simply returns nil.
type crossAnalysisFaultInjectionKey struct{}

// crossAnalysisFaultInjection holds the three optional fault-injection
// points for AppendCrossAnalysisTurn's transactional steps, reachable only
// through the specific context a test built with
// ContextWithCrossAnalysisFaultInjection. Each field, when set, is invoked
// with the transaction's own cancel func immediately before the named step,
// letting a test force that EXACT step -- and only that step, and only for
// that one call -- to fail deterministically (via cancel()), without racing
// real timing or needing a step-specific data-driven constraint violation
// (unlike the source-row insert, whose failure the existing rollback test
// already triggers genuinely via a source citing a nonexistent note id).
type crossAnalysisFaultInjection struct {
	beforeAssistantMessageInsert      func(cancel context.CancelFunc)
	beforeConversationTimestampUpdate func(cancel context.CancelFunc)
	beforeCommit                      func(cancel context.CancelFunc)
}

// AppendCrossAnalysisTurn atomically persists the complete cross-meeting
// analysis turn (issue #765): the user message, the assistant message, and
// every one of its source rows, then bumps the conversation's updated_at --
// all in ONE transaction. Both messages and every source must already be
// fully constructed by the caller (see internal/execution.Result and the API
// layer's conversion to model.MessageSource); an error at any statement or
// at commit rolls back everything, so a reader never observes a user-only
// turn or a message whose citations are missing after reload.
//
// This never falls back to independent AppendMessage calls -- see the
// accepted spec's "Persistence" section.
func (s *Store) AppendCrossAnalysisTurn(ctx context.Context, conversationID, userContent, assistantContent, assistantModel string, tokensUsed *int, sources []model.MessageSource) (userMsg model.Message, assistantMsg model.Message, err error) {
	// fi is nil for every production call and every test that did not build
	// its context via ContextWithCrossAnalysisFaultInjection -- see that
	// type's doc comment for why this is call-scoped rather than a global.
	fi, _ := ctx.Value(crossAnalysisFaultInjectionKey{}).(*crossAnalysisFaultInjection)

	// A cancelable child context lets fi's hooks (if any) force one exact
	// subsequent statement to fail. cancel is always called via defer
	// regardless, which is a no-op once the transaction has already
	// committed or been rolled back.
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tx, err := s.pool.Begin(queryCtx)
	if err != nil {
		return model.Message{}, model.Message{}, err
	}
	// Rolled back on a fresh, never-canceled context: a fault-injection hook
	// (or a real caller-context cancellation) may have already canceled
	// queryCtx by the time this deferred rollback runs, and that must never
	// stop the transaction from actually rolling back.
	defer tx.Rollback(context.Background())

	err = tx.QueryRow(queryCtx,
		`INSERT INTO messages (conversation_id, role, content, model, tokens_used)
		 VALUES ($1,'user',$2,'',NULL)
		 RETURNING id, conversation_id, role, content, model, tokens_used, created_at`,
		conversationID, userContent).
		Scan(&userMsg.ID, &userMsg.ConversationID, &userMsg.Role, &userMsg.Content, &userMsg.Model, &userMsg.TokensUsed, &userMsg.CreatedAt)
	if err != nil {
		return model.Message{}, model.Message{}, err
	}

	if fi != nil && fi.beforeAssistantMessageInsert != nil {
		fi.beforeAssistantMessageInsert(cancel)
	}
	err = tx.QueryRow(queryCtx,
		`INSERT INTO messages (conversation_id, role, content, model, tokens_used)
		 VALUES ($1,'assistant',$2,$3,$4)
		 RETURNING id, conversation_id, role, content, model, tokens_used, created_at`,
		conversationID, assistantContent, assistantModel, tokensUsed).
		Scan(&assistantMsg.ID, &assistantMsg.ConversationID, &assistantMsg.Role, &assistantMsg.Content, &assistantMsg.Model, &assistantMsg.TokensUsed, &assistantMsg.CreatedAt)
	if err != nil {
		return model.Message{}, model.Message{}, err
	}

	for _, src := range sources {
		if _, err := tx.Exec(queryCtx,
			`INSERT INTO message_sources (message_id, citation_number, note_id, transcript_generation, segment_index, timestamp_ms, snippet)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			assistantMsg.ID, src.N, src.NoteID, src.TranscriptGeneration, src.SegmentIndex, src.Timestamp,
			truncateRunesEllipsis(src.Snippet, crossAnalysisSnippetMaxRunes)); err != nil {
			return model.Message{}, model.Message{}, err
		}
	}

	if fi != nil && fi.beforeConversationTimestampUpdate != nil {
		fi.beforeConversationTimestampUpdate(cancel)
	}
	if _, err := tx.Exec(queryCtx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, conversationID); err != nil {
		return model.Message{}, model.Message{}, err
	}

	if fi != nil && fi.beforeCommit != nil {
		fi.beforeCommit(cancel)
	}
	if err := tx.Commit(queryCtx); err != nil {
		return model.Message{}, model.Message{}, err
	}

	assistantMsg.Sources = sources
	return userMsg, assistantMsg, nil
}
