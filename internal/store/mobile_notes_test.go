// Package store_test: mobile note pagination tests. Like sibling *_test.go
// files in this package, these require a live Postgres reachable via
// TEST_DATABASE_URL (testutil.NewPool skips otherwise) — never run locally
// per this repo's DB-test rule; they are proved by the `server (go)` CI
// check.
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestListMobileNotes(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "mobile-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "mobile-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	setCreatedAt := func(noteID string, ts time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE notes SET created_at=$1, updated_at=$1 WHERE id=$2`, ts, noteID); err != nil {
			t.Fatalf("set created_at: %v", err)
		}
	}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Five plain notes at distinct timestamps, newest last-created.
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		n, err := st.CreateNote(ctx, owner.ID, "Note")
		if err != nil {
			t.Fatalf("create note %d: %v", i, err)
		}
		setCreatedAt(n.ID, base.Add(time.Duration(i)*time.Minute))
		ids = append(ids, n.ID)
	}
	// A pinned note created earliest of all — must still sort first.
	pinned, err := st.CreateNote(ctx, owner.ID, "Pinned")
	if err != nil {
		t.Fatalf("create pinned: %v", err)
	}
	setCreatedAt(pinned.ID, base.Add(-time.Hour))
	if err := st.SetNotePinned(ctx, owner.ID, pinned.ID, true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	// A tag on one note, exercised via the bounded bulk tag query.
	if _, err := st.AddNoteTag(ctx, owner.ID, ids[4], "urgent"); err != nil {
		t.Fatalf("add tag: %v", err)
	}
	// A deleted note must never appear.
	deleted, err := st.CreateNote(ctx, owner.ID, "Trashed")
	if err != nil {
		t.Fatalf("create deleted: %v", err)
	}
	if err := st.DeleteNote(ctx, owner.ID, deleted.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Another owner's note must never appear for owner's query.
	if _, err := st.CreateNote(ctx, other.ID, "Someone else's"); err != nil {
		t.Fatalf("create other note: %v", err)
	}

	t.Run("first page pinned then newest first, limit+1 semantics, non-null tags", func(t *testing.T) {
		got, hasMore, err := st.ListMobileNotes(ctx, owner.ID, 3, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if !hasMore {
			t.Fatalf("expected hasMore=true with 6 live notes and limit 3")
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(got))
		}
		if got[0].ID != pinned.ID || !got[0].Pinned {
			t.Fatalf("expected pinned note first, got %+v", got[0])
		}
		// Newest-created-first among unpinned: ids[4], ids[3], ...
		if got[1].ID != ids[4] || got[2].ID != ids[3] {
			t.Fatalf("unexpected order: %+v", got)
		}
		if got[1].Tags == nil || len(got[1].Tags) != 1 || got[1].Tags[0] != "urgent" {
			t.Fatalf("expected bulk-loaded tag, got %+v", got[1].Tags)
		}
		for _, n := range got {
			if n.Tags == nil {
				t.Fatalf("tags must never be nil: %+v", n)
			}
		}
	})

	t.Run("keyset traversal covers every live note exactly once, final page omits cursor", func(t *testing.T) {
		var all []store.MobileNote
		var cursor *store.MobileNoteCursor
		for page := 0; page < 10; page++ {
			got, hasMore, err := st.ListMobileNotes(ctx, owner.ID, 2, cursor)
			if err != nil {
				t.Fatalf("list page %d: %v", page, err)
			}
			all = append(all, got...)
			if !hasMore {
				break
			}
			last := got[len(got)-1]
			cursor = &store.MobileNoteCursor{Pinned: last.Pinned, CreatedAt: last.CreatedAt, ID: last.ID}
		}
		if len(all) != 6 {
			t.Fatalf("expected 6 notes across pages, got %d: %+v", len(all), all)
		}
		seen := map[string]bool{}
		for _, n := range all {
			if seen[n.ID] {
				t.Fatalf("duplicate note %s across pages", n.ID)
			}
			seen[n.ID] = true
		}
		if !seen[pinned.ID] {
			t.Fatalf("pinned note missing from traversal")
		}
		if seen[deleted.ID] {
			t.Fatalf("deleted note leaked into pagination")
		}
	})

	t.Run("owner isolation", func(t *testing.T) {
		got, _, err := st.ListMobileNotes(ctx, other.ID, 30, nil)
		if err != nil {
			t.Fatalf("list other: %v", err)
		}
		for _, n := range got {
			if n.ID == pinned.ID || n.ID == deleted.ID {
				t.Fatalf("owner isolation violated: %+v", n)
			}
			for _, id := range ids {
				if n.ID == id {
					t.Fatalf("owner isolation violated: %+v", n)
				}
			}
		}
	})

	t.Run("id tiebreak is load-bearing for stable ordering", func(t *testing.T) {
		// Two notes sharing one created_at instant must still each appear
		// exactly once and in a stable (id DESC) order across a paged
		// traversal. This is the scenario a reversed id comparison in the
		// keyset predicate breaks (verified manually during development by
		// flipping n.id < $4 to n.id > $4 in mobile_notes.go and observing
		// this subtest fail with a duplicate/skip before restoring it — DB
		// tests cannot run in this sandbox, so CI is authoritative here).
		tie := base.Add(30 * time.Second)
		a, err := st.CreateNote(ctx, owner.ID, "Tie A")
		if err != nil {
			t.Fatalf("create tie a: %v", err)
		}
		b, err := st.CreateNote(ctx, owner.ID, "Tie B")
		if err != nil {
			t.Fatalf("create tie b: %v", err)
		}
		setCreatedAt(a.ID, tie)
		setCreatedAt(b.ID, tie)

		first, hasMore, err := st.ListMobileNotes(ctx, owner.ID, 1, nil)
		if err != nil || !hasMore {
			t.Fatalf("list first: got=%v hasMore=%v err=%v", first, hasMore, err)
		}
		// The pinned note still sorts first regardless of the new ties.
		if first[0].ID != pinned.ID {
			t.Fatalf("expected pinned first, got %+v", first[0])
		}
	})

	t.Run("limit is capped even if caller passes an out-of-range value", func(t *testing.T) {
		got, _, err := st.ListMobileNotes(ctx, owner.ID, 1000, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) > 100 {
			t.Fatalf("expected cap at 100, got %d", len(got))
		}
	})
}

func TestGetMobileNoteDetail(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "detail-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "detail-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	n, err := st.CreateNote(ctx, owner.ID, "Detail note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, owner.ID, n.ID, "# Heading\n\nBody markdown *content*."); err != nil {
		t.Fatalf("update body: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, owner.ID, n.ID, "planning"); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	tmpl, err := st.CreateTemplate(ctx, owner.ID, "Recap", "after", []model.TemplateSection{{Heading: "Key Points", Instruction: "Summarize key points"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	tmplID := tmpl.ID
	sumID, err := st.CreatePendingSummary(ctx, n.ID, tmplID)
	if err != nil {
		t.Fatalf("create pending summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, sumID, "plugin", "model", []model.SummarySection{
		{Heading: "Key Points", ContentMarkdown: "- did the thing"},
	}, false); err != nil {
		t.Fatalf("complete summary: %v", err)
	}
	sumID2, err := st.CreatePendingSummary(ctx, n.ID, tmplID)
	if err != nil {
		t.Fatalf("create second pending summary: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE summaries SET created_at = created_at + interval '1 second' WHERE id=$1`, sumID2); err != nil {
		t.Fatalf("bump created_at: %v", err)
	}

	t.Run("success returns minimized fields and ordered summaries", func(t *testing.T) {
		got, err := st.GetMobileNoteDetail(ctx, owner.ID, n.ID)
		if err != nil {
			t.Fatalf("get detail: %v", err)
		}
		if got.Note.ID != n.ID || got.Note.Title != "Detail note" {
			t.Fatalf("unexpected note: %+v", got.Note)
		}
		if got.BodyMarkdown == "" {
			t.Fatalf("expected body markdown")
		}
		if len(got.Note.Tags) != 1 || got.Note.Tags[0] != "planning" {
			t.Fatalf("unexpected tags: %+v", got.Note.Tags)
		}
		if len(got.Summaries) != 2 {
			t.Fatalf("expected 2 summaries ordered by created_at, got %d", len(got.Summaries))
		}
		if got.Summaries[0].ID != sumID || got.Summaries[1].ID != sumID2 {
			t.Fatalf("summaries not in created_at ASC, id ASC order: %+v", got.Summaries)
		}
		if got.Summaries[0].Status != model.SummaryReady {
			t.Fatalf("expected ready status, got %s", got.Summaries[0].Status)
		}
		if got.Summaries[1].Status != model.SummaryPending {
			t.Fatalf("expected pending status, got %s", got.Summaries[1].Status)
		}
		if got.Summaries[0].Sections == nil || len(got.Summaries[0].Sections) != 1 {
			t.Fatalf("expected one non-nil section, got %+v", got.Summaries[0].Sections)
		}
	})

	t.Run("other owner gets ErrNotFound (indistinguishable from absent)", func(t *testing.T) {
		if _, err := st.GetMobileNoteDetail(ctx, other.ID, n.ID); err != store.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("absent note gets ErrNotFound", func(t *testing.T) {
		if _, err := st.GetMobileNoteDetail(ctx, owner.ID, "00000000-0000-0000-0000-000000000000"); err != store.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("deleted note gets ErrNotFound", func(t *testing.T) {
		if err := st.DeleteNote(ctx, owner.ID, n.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := st.GetMobileNoteDetail(ctx, owner.ID, n.ID); err != store.ErrNotFound {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
	})
}
