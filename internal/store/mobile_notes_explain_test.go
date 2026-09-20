// Opt-in scale/EXPLAIN proof for issue #767's mobile notes pagination.
//
// This test is DB-backed (TEST_DATABASE_URL) AND opt-in behind
// MOBILE_NOTES_SCALE_TEST=1: it bulk-inserts 100,000+ notes rows, which is
// too slow/heavy to run on every CI invocation or ever locally in this
// sandbox (no test DB here at all — see the repo's DB-test rule). Run it
// deliberately against a real Postgres:
//
//	TEST_DATABASE_URL=... MOBILE_NOTES_SCALE_TEST=1 go test ./internal/store/... -run TestMobileNotesScaleAndExplain -v -timeout 10m
package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// explainNode is the subset of a Postgres EXPLAIN (FORMAT JSON) plan node
// this test inspects.
type explainNode struct {
	NodeType     string        `json:"Node Type"`
	RelationName string        `json:"Relation Name"`
	IndexName    string        `json:"Index Name"`
	ActualRows   float64       `json:"Actual Rows"`
	Plans        []explainNode `json:"Plans"`
}

type explainRoot struct {
	Plan explainNode `json:"Plan"`
}

// walk calls fn for every node in the plan tree (pre-order).
func (n explainNode) walk(fn func(explainNode)) {
	fn(n)
	for _, c := range n.Plans {
		c.walk(fn)
	}
}

// mobileNoteListFirstPageSQL and mobileNoteListCursorPageSQL mirror
// mobile_notes.go's ListMobileNotes query text exactly (this package can't
// reach that unexported string from the external store_test package). Any
// future edit to ListMobileNotes's SQL must be mirrored here, or this test's
// EXPLAIN evidence stops proving anything about the real query.
const mobileNoteListFirstPageSQL = `SELECT n.id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
	       n.created_at, n.updated_at, COALESCE(nb.content, '')
	FROM notes n
	LEFT JOIN note_bodies nb ON nb.note_id = n.id
	WHERE n.owner_id = $1 AND n.deleted_at IS NULL
	ORDER BY n.pinned DESC, n.created_at DESC, n.id DESC LIMIT $2`

const mobileNoteListCursorPageSQL = `SELECT n.id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
	       n.created_at, n.updated_at, COALESCE(nb.content, '')
	FROM notes n
	LEFT JOIN note_bodies nb ON nb.note_id = n.id
	WHERE n.owner_id = $1 AND n.deleted_at IS NULL
	AND (
		n.pinned < $2
		OR (n.pinned = $2 AND n.created_at < $3)
		OR (n.pinned = $2 AND n.created_at = $3 AND n.id < $4)
	)
	ORDER BY n.pinned DESC, n.created_at DESC, n.id DESC LIMIT $5`

func explainPlan(t *testing.T, ctx context.Context, pool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, query string, args ...any) explainNode {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var roots []explainRoot
	if err := json.Unmarshal(raw, &roots); err != nil {
		t.Fatalf("parse explain json: %v\nraw=%s", err, raw)
	}
	if len(roots) != 1 {
		t.Fatalf("expected one plan root, got %d", len(roots))
	}
	return roots[0].Plan
}

// assertUsesPartialIndexNoSeqScan walks the plan and fails the test unless
// (a) no node is a sequential scan of the notes table, and (b) some node
// uses idx_notes_owner_pagination.
func assertUsesPartialIndexNoSeqScan(t *testing.T, plan explainNode) {
	t.Helper()
	usedIndex := false
	plan.walk(func(n explainNode) {
		if n.NodeType == "Seq Scan" && n.RelationName == "notes" {
			t.Fatalf("sequential scan of notes table in plan: %+v", n)
		}
		if n.IndexName == "idx_notes_owner_pagination" {
			usedIndex = true
		}
	})
	if !usedIndex {
		t.Fatalf("plan never used idx_notes_owner_pagination")
	}
}

// rootActualRows returns the root node's Actual Rows — the number of rows
// the query actually produced, which for a query ending in LIMIT n must be
// <= n regardless of how many rows exist in the table.
func rootActualRows(plan explainNode) float64 { return plan.ActualRows }

func TestMobileNotesScaleAndExplain(t *testing.T) {
	// Deliberately a plain t.Skip, NOT testsupport.RequireDependency: that
	// helper turns a missing dependency into a hard t.Fatalf whenever CI=true
	// (the normal case for GitHub Actions), which is exactly right for
	// TEST_DATABASE_URL (CI always provisions it) but wrong here — this
	// 100k+ row scale test must stay opt-in even inside `server (go)` CI, so
	// its absence must always skip, never fail the build.
	if os.Getenv("MOBILE_NOTES_SCALE_TEST") != "1" {
		t.Skip("opt-in: set MOBILE_NOTES_SCALE_TEST=1 to run the 100k+ row scale/EXPLAIN proof (issue #767)")
	}

	pool := testutil.NewPoolWithMaxConns(t, 5)
	st := store.New(pool)
	ctx := context.Background()

	const (
		targetTotal     = 20500 // >= 20,000 required; load-bearing figure.
		targetDeleted   = 2050  // ~10% mixed deleted/non-deleted.
		otherOwners     = 40
		notesPerOther   = 2000 // 40 * 2000 = 80,000
		sampleDeletedOf = 3    // first N other owners also get a deleted mix.
	)

	target, err := st.CreateUser(ctx, "scale-target-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create target owner: %v", err)
	}

	type row struct {
		id, ownerID, title, status string
		pinned                     bool
		createdAt                  time.Time
		deletedAt                  *time.Time
	}
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	var rows []row

	for i := 0; i < targetTotal; i++ {
		var deletedAt *time.Time
		if i < targetDeleted {
			d := base.Add(time.Duration(i) * time.Second).Add(48 * time.Hour)
			deletedAt = &d
		}
		rows = append(rows, row{
			id:        uuid.NewString(),
			ownerID:   target.ID,
			title:     "Scale note",
			status:    model.NoteReady,
			pinned:    i%500 == 0, // a small, sparse set of pinned rows.
			createdAt: base.Add(time.Duration(i) * time.Second),
			deletedAt: deletedAt,
		})
	}

	otherOwnerIDs := make([]string, otherOwners)
	for o := 0; o < otherOwners; o++ {
		u, err := st.CreateUser(ctx, "scale-other-owner-"+uuid.NewString()+"@example.com", "h")
		if err != nil {
			t.Fatalf("create other owner %d: %v", o, err)
		}
		otherOwnerIDs[o] = u.ID
		deletedMix := o < sampleDeletedOf
		for i := 0; i < notesPerOther; i++ {
			var deletedAt *time.Time
			if deletedMix && i%10 == 0 {
				d := base.Add(time.Duration(i) * time.Second).Add(48 * time.Hour)
				deletedAt = &d
			}
			rows = append(rows, row{
				id:        uuid.NewString(),
				ownerID:   u.ID,
				title:     "Other's scale note",
				status:    model.NoteReady,
				pinned:    i%1000 == 0,
				createdAt: base.Add(time.Duration(i) * time.Second),
				deletedAt: deletedAt,
			})
		}
	}

	t.Logf("bulk-inserting %d notes (%d target owner, %d other owners x %d)", len(rows), targetTotal, otherOwners, notesPerOther)
	n, err := pool.CopyFrom(ctx,
		pgx.Identifier{"notes"},
		[]string{"id", "owner_id", "title", "status", "pinned", "created_at", "updated_at", "deleted_at"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{r.id, r.ownerID, r.title, r.status, r.pinned, r.createdAt, r.createdAt, r.deletedAt}, nil
		}))
	if err != nil {
		t.Fatalf("copy from: %v", err)
	}
	if int(n) != len(rows) {
		t.Fatalf("copied %d rows, want %d", n, len(rows))
	}
	if _, err := pool.Exec(ctx, "ANALYZE notes"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	targetLiveCount := targetTotal - targetDeleted

	t.Run("first page uses the partial index, not a seq scan, and is row-bounded", func(t *testing.T) {
		plan := explainPlan(t, ctx, pool, mobileNoteListFirstPageSQL, target.ID, 101)
		assertUsesPartialIndexNoSeqScan(t, plan)
		if got := rootActualRows(plan); got > 101 {
			t.Fatalf("first page examined %v rows, want <= 101", got)
		}
	})

	t.Run("mid-traversal cursor page also uses the partial index and stays row-bounded", func(t *testing.T) {
		// Establish a real mid-traversal cursor by asking Postgres for the
		// ordering tuple ~half way through the target owner's live rows.
		// (OFFSET is used only to seed this test's fixture cursor — the
		// production query path never uses OFFSET.)
		var pinned bool
		var createdAt time.Time
		var id string
		err := pool.QueryRow(ctx,
			`SELECT pinned, created_at, id FROM notes
			 WHERE owner_id=$1 AND deleted_at IS NULL
			 ORDER BY pinned DESC, created_at DESC, id DESC
			 OFFSET $2 LIMIT 1`, target.ID, targetLiveCount/2).
			Scan(&pinned, &createdAt, &id)
		if err != nil {
			t.Fatalf("seed cursor: %v", err)
		}

		plan := explainPlan(t, ctx, pool, mobileNoteListCursorPageSQL, target.ID, pinned, createdAt, id, 101)
		assertUsesPartialIndexNoSeqScan(t, plan)
		if got := rootActualRows(plan); got > 101 {
			t.Fatalf("mid-traversal page examined %v rows, want <= 101", got)
		}

		// Cross-check against the real store call: same cursor, same bound.
		got, hasMore, err := st.ListMobileNotes(ctx, target.ID, 100, &store.MobileNoteCursor{Pinned: pinned, CreatedAt: createdAt, ID: id})
		if err != nil {
			t.Fatalf("list mobile notes: %v", err)
		}
		if len(got) > 100 {
			t.Fatalf("page returned %d rows, want <= 100", len(got))
		}
		_ = hasMore
	})

	t.Run("full traversal returns exactly the live rows once each, deleted rows never appear", func(t *testing.T) {
		const pageLimit = 100
		seen := map[string]bool{}
		var cursor *store.MobileNoteCursor
		pages := 0
		for {
			pages++
			if pages > (targetLiveCount/pageLimit)+5 {
				t.Fatalf("traversal did not terminate within expected page count")
			}
			got, hasMore, err := st.ListMobileNotes(ctx, target.ID, pageLimit, cursor)
			if err != nil {
				t.Fatalf("list page %d: %v", pages, err)
			}
			// Every page — regardless of total table size — costs exactly
			// two bounded queries in ListMobileNotes: one keyset page fetch
			// (LIMIT pageLimit+1) and one bulk tag lookup keyed by this
			// page's ids. That query count is constant per page and never
			// scales with the 100,000+ row table; this traversal proves the
			// per-page row bound (len(got) <= pageLimit) holds at every
			// depth, including deep into a 20,000+ row single-owner
			// partition.
			if len(got) > pageLimit {
				t.Fatalf("page %d returned %d rows, want <= %d", pages, len(got), pageLimit)
			}
			for _, item := range got {
				if seen[item.ID] {
					t.Fatalf("duplicate id %s at page %d", item.ID, pages)
				}
				seen[item.ID] = true
			}
			if !hasMore {
				break
			}
			last := got[len(got)-1]
			cursor = &store.MobileNoteCursor{Pinned: last.Pinned, CreatedAt: last.CreatedAt, ID: last.ID}
		}
		if len(seen) != targetLiveCount {
			t.Fatalf("traversal saw %d live notes, want exactly %d", len(seen), targetLiveCount)
		}
		expectedPages := targetLiveCount / pageLimit
		if targetLiveCount%pageLimit != 0 {
			expectedPages++
		}
		if pages != expectedPages {
			t.Fatalf("traversal took %d pages, want exactly %d", pages, expectedPages)
		}
	})

	t.Run("migration rollback drops and restores the index without breaking the query", func(t *testing.T) {
		downSQL, err := os.ReadFile("../db/migrations/20260920234920_notes_owner_pagination.down.sql")
		if err != nil {
			t.Fatalf("read down migration: %v", err)
		}
		upSQL, err := os.ReadFile("../db/migrations/20260920234920_notes_owner_pagination.up.sql")
		if err != nil {
			t.Fatalf("read up migration: %v", err)
		}

		if _, err := pool.Exec(ctx, string(downSQL)); err != nil {
			t.Fatalf("apply down migration: %v", err)
		}
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='idx_notes_owner_pagination')`).Scan(&exists); err != nil {
			t.Fatalf("check index absent: %v", err)
		}
		if exists {
			t.Fatalf("index still present after rollback")
		}
		// The query must still work (just slower/without the index) —
		// rollback must not break the feature, only its performance.
		if _, _, err := st.ListMobileNotes(ctx, target.ID, 10, nil); err != nil {
			t.Fatalf("list mobile notes after rollback: %v", err)
		}

		if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
			t.Fatalf("re-apply up migration: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='idx_notes_owner_pagination')`).Scan(&exists); err != nil {
			t.Fatalf("check index restored: %v", err)
		}
		if !exists {
			t.Fatalf("index missing after re-applying up migration")
		}
		if _, err := pool.Exec(ctx, "ANALYZE notes"); err != nil {
			t.Fatalf("re-analyze: %v", err)
		}
		plan := explainPlan(t, ctx, pool, mobileNoteListFirstPageSQL, target.ID, 101)
		assertUsesPartialIndexNoSeqScan(t, plan)
	})
}
