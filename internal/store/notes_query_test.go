package store

import (
	"strings"
	"testing"
)

func TestGetNoteSelectQueryIncludesRetentionState(t *testing.T) {
	t.Parallel()

	if !strings.Contains(getNoteSelectQuery, "retention_state") {
		t.Fatalf("getNoteSelectQuery missing retention_state: %q", getNoteSelectQuery)
	}
	if !strings.Contains(getNoteSelectQuery, "pinned") {
		t.Fatalf("getNoteSelectQuery missing pinned: %q", getNoteSelectQuery)
	}
	if !strings.Contains(getNoteSelectQuery, "event_id") {
		t.Fatalf("getNoteSelectQuery missing event_id: %q", getNoteSelectQuery)
	}
}

func TestNotesOrderClausePinsFirst(t *testing.T) {
	t.Parallel()

	if got := notesOrderClause(false); got != "n.pinned DESC, n.created_at DESC, n.id" {
		t.Fatalf("notesOrderClause(false) = %q, want pinned-first created-desc", got)
	}
	if got := notesOrderClause(true); got != "n.pinned DESC, nf.position, n.created_at DESC, n.id" {
		t.Fatalf("notesOrderClause(true) = %q, want pinned-first folder-order", got)
	}
}
