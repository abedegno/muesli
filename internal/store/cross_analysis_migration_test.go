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
		WHERE table_name = 'message_sources' AND table_schema = current_schema()`)
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
		  AND tc.table_schema = current_schema()
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
		WHERE kcu.table_name = 'message_sources' AND kcu.column_name = 'note_id'
		  AND kcu.table_schema = current_schema()`).Scan(&noteDeleteRule)
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
		WHERE kcu.table_name = 'message_sources' AND kcu.column_name = 'message_id'
		  AND kcu.table_schema = current_schema()`).Scan(&messageDeleteRule)
	if err != nil {
		t.Fatalf("query message_id delete rule: %v", err)
	}
	if messageDeleteRule != "CASCADE" {
		t.Fatalf("expected message_id ON DELETE CASCADE, got %q", messageDeleteRule)
	}

	// An index on note_id exists (for reverse lookups / cleanup scans).
	//
	// Deliberately NOT `pg_indexes`: that view's indexdef column is
	// computed by calling pg_get_indexdef(indexrelid) for every index in
	// the ENTIRE cluster before this query's own WHERE clause ever
	// filters down to this schema/table -- so under this suite's
	// concurrent per-test schemas (testutil.NewPool creates one schema
	// per test and drops it via t.Cleanup, and CI runs `go test -p 4
	// -parallel 2`), a sibling test's schema/index being dropped in the
	// moment between pg_indexes' internal snapshot and its
	// pg_get_indexdef call for that now-gone relation fails this ENTIRE
	// query with "could not open relation with OID ..." even though that
	// unrelated index would have been filtered out anyway. Querying
	// pg_index/pg_class/pg_namespace/pg_attribute directly instead scopes
	// to this table's oid (via pg_namespace.nspname = current_schema())
	// before ever inspecting indkey, and never opens any relation outside
	// this test's own schema, so it can't be hit by concurrent teardown
	// elsewhere.
	var indexCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(i.indkey)
		WHERE t.relname = 'message_sources' AND n.nspname = current_schema()
		  AND a.attname = 'note_id'`).Scan(&indexCount)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	if indexCount == 0 {
		t.Fatalf("expected an index on message_sources.note_id")
	}
}
