package store_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/testutil"
)

// TestMessageSourcesMigrationShape asserts the message_sources table created
// by the cross_analysis_sources migration (issue #765) has the expected
// columns, primary key, and foreign-key delete behaviour: message_id
// cascades with its assistant message, note_id is nullable and clears
// (ON DELETE SET NULL) rather than cascading when a note is deleted -- so a
// deleted note's citation loses its navigation target but the historical
// assistant text/row survives. testutil.NewPool migrates a fresh schema for
// every test, so this exercises the real migration harness end to end.
func TestMessageSourcesMigrationShape(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	ctx := context.Background()

	cols := map[string]string{}
	rows, err := pool.Query(ctx, `
		SELECT column_name, is_nullable
		FROM information_schema.columns
		WHERE table_name = 'message_sources'`)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	for rows.Next() {
		var name, nullable string
		if err := rows.Scan(&name, &nullable); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = nullable
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	wantNotNull := []string{"message_id", "citation_number", "transcript_generation", "segment_index", "timestamp_ms", "snippet"}
	for _, c := range wantNotNull {
		nullable, ok := cols[c]
		if !ok {
			t.Fatalf("expected column %q to exist, got columns %+v", c, cols)
		}
		if nullable != "NO" {
			t.Fatalf("expected column %q NOT NULL, got nullable=%q", c, nullable)
		}
	}
	if nullable, ok := cols["note_id"]; !ok {
		t.Fatalf("expected column note_id to exist, got columns %+v", cols)
	} else if nullable != "YES" {
		t.Fatalf("expected note_id to be nullable, got nullable=%q", nullable)
	}

	// Primary key is (message_id, citation_number).
	var pkCols []string
	pkRows, err := pool.Query(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.table_name = 'message_sources' AND tc.constraint_type = 'PRIMARY KEY'
		ORDER BY kcu.ordinal_position`)
	if err != nil {
		t.Fatalf("query pk: %v", err)
	}
	for pkRows.Next() {
		var c string
		if err := pkRows.Scan(&c); err != nil {
			t.Fatalf("scan pk col: %v", err)
		}
		pkCols = append(pkCols, c)
	}
	pkRows.Close()
	if len(pkCols) != 2 || pkCols[0] != "message_id" || pkCols[1] != "citation_number" {
		t.Fatalf("expected primary key (message_id, citation_number), got %+v", pkCols)
	}

	// note_id FK is ON DELETE SET NULL (not CASCADE).
	var noteDeleteRule string
	err = pool.QueryRow(ctx, `
		SELECT rc.delete_rule
		FROM information_schema.referential_constraints rc
		JOIN information_schema.key_column_usage kcu
		  ON rc.constraint_name = kcu.constraint_name AND rc.constraint_schema = kcu.table_schema
		WHERE kcu.table_name = 'message_sources' AND kcu.column_name = 'note_id'`).Scan(&noteDeleteRule)
	if err != nil {
		t.Fatalf("query note_id delete rule: %v", err)
	}
	if noteDeleteRule != "SET NULL" {
		t.Fatalf("expected note_id ON DELETE SET NULL, got %q", noteDeleteRule)
	}

	// message_id FK cascades with its assistant message.
	var messageDeleteRule string
	err = pool.QueryRow(ctx, `
		SELECT rc.delete_rule
		FROM information_schema.referential_constraints rc
		JOIN information_schema.key_column_usage kcu
		  ON rc.constraint_name = kcu.constraint_name AND rc.constraint_schema = kcu.table_schema
		WHERE kcu.table_name = 'message_sources' AND kcu.column_name = 'message_id'`).Scan(&messageDeleteRule)
	if err != nil {
		t.Fatalf("query message_id delete rule: %v", err)
	}
	if messageDeleteRule != "CASCADE" {
		t.Fatalf("expected message_id ON DELETE CASCADE, got %q", messageDeleteRule)
	}

	// An index on note_id exists (for reverse lookups / cleanup scans).
	var indexCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_indexes
		WHERE tablename = 'message_sources' AND indexdef ILIKE '%note_id%'`).Scan(&indexCount)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	if indexCount == 0 {
		t.Fatalf("expected an index on message_sources.note_id")
	}
}
