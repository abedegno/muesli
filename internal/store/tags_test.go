package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newStoreWithOwner returns a *Store, a freshly-created owner user ID, and the
// underlying pool (so tests can run raw queries when needed).
func newStoreWithOwner(t *testing.T) (*store.Store, string, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, fmt.Sprintf("owner-%d@example.com", seedUserCounter.Add(1)), "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return st, u.ID, pool
}

// addUser creates an additional user in the given store and returns its ID.
func addUser(t *testing.T, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, fmt.Sprintf("extra-%d@example.com", seedUserCounter.Add(1)), "h")
	if err != nil {
		t.Fatalf("create extra user: %v", err)
	}
	return u.ID
}

func TestAddAndRemoveNoteTag(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	n, err := st.CreateNote(ctx, ownerID, "Standup")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	tag, err := st.AddNoteTag(ctx, ownerID, n.ID, "1on1")
	if err != nil {
		t.Fatalf("add tag: %v", err)
	}
	if tag.Name != "1on1" || tag.ID == "" {
		t.Fatalf("unexpected tag %+v", tag)
	}

	again, err := st.AddNoteTag(ctx, ownerID, n.ID, "1ON1") // case-insensitive reuse + idempotent
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if again.ID != tag.ID {
		t.Errorf("reuse: got %s want %s", again.ID, tag.ID)
	}

	names, err := st.NoteTags(ctx, n.ID)
	if err != nil {
		t.Fatalf("note tags: %v", err)
	}
	if len(names) != 1 || names[0] != "1on1" {
		t.Errorf("note tags = %v, want [1on1]", names)
	}

	if err := st.RemoveNoteTag(ctx, ownerID, n.ID, "1on1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	names2, _ := st.NoteTags(ctx, n.ID)
	if len(names2) != 0 {
		t.Errorf("after remove = %v, want []", names2)
	}

	// Verify the tag row itself was deleted (orphan cleanup).
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tags WHERE id=$1`, tag.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("orphan tag not deleted: count=%d", count)
	}
}

func TestSharedTagNotOrphanedOnPartialRemoval(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	noteA, err := st.CreateNote(ctx, ownerID, "Note A")
	if err != nil {
		t.Fatalf("create note A: %v", err)
	}
	noteB, err := st.CreateNote(ctx, ownerID, "Note B")
	if err != nil {
		t.Fatalf("create note B: %v", err)
	}

	if _, err := st.AddNoteTag(ctx, ownerID, noteA.ID, "shared"); err != nil {
		t.Fatalf("add tag to A: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, ownerID, noteB.ID, "shared"); err != nil {
		t.Fatalf("add tag to B: %v", err)
	}

	if err := st.RemoveNoteTag(ctx, ownerID, noteA.ID, "shared"); err != nil {
		t.Fatalf("remove tag from A: %v", err)
	}

	// Note A should have no tags.
	tagsA, err := st.NoteTags(ctx, noteA.ID)
	if err != nil {
		t.Fatalf("note A tags: %v", err)
	}
	if len(tagsA) != 0 {
		t.Errorf("note A tags = %v, want []", tagsA)
	}

	// Note B should still have the "shared" tag.
	tagsB, err := st.NoteTags(ctx, noteB.ID)
	if err != nil {
		t.Fatalf("note B tags: %v", err)
	}
	if len(tagsB) != 1 || tagsB[0] != "shared" {
		t.Errorf("note B tags = %v, want [shared]", tagsB)
	}

	// The tags row itself must NOT have been deleted (it's still referenced by B).
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tags WHERE lower(name)='shared'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("shared tag row count = %d, want 1", count)
	}
}

func TestNoteTagOwnerScoping(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	otherID := addUser(t, st)
	ctx := context.Background()
	n, _ := st.CreateNote(ctx, ownerID, "Private")
	if _, err := st.AddNoteTag(ctx, otherID, n.ID, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner add err = %v, want ErrNotFound", err)
	}
}

func TestListTags(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	n1, _ := st.CreateNote(ctx, ownerID, "Note 1")
	n2, _ := st.CreateNote(ctx, ownerID, "Note 2")
	trashed, _ := st.CreateNote(ctx, ownerID, "Trashed")

	// "alpha" on two live notes → count 2.
	if _, err := st.AddNoteTag(ctx, ownerID, n1.ID, "alpha"); err != nil {
		t.Fatalf("tag n1: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, ownerID, n2.ID, "alpha"); err != nil {
		t.Fatalf("tag n2: %v", err)
	}
	// "Beta" on one live note → count 1.
	if _, err := st.AddNoteTag(ctx, ownerID, n1.ID, "Beta"); err != nil {
		t.Fatalf("tag beta: %v", err)
	}
	// "gamma" only on a trashed note → excluded (count 0 rows after the JOIN).
	if _, err := st.AddNoteTag(ctx, ownerID, trashed.ID, "gamma"); err != nil {
		t.Fatalf("tag trashed: %v", err)
	}
	if err := st.DeleteNote(ctx, ownerID, trashed.ID); err != nil {
		t.Fatalf("trash note: %v", err)
	}

	got, err := st.ListTags(ctx, ownerID)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	// Ordered by lower(name): alpha, Beta. gamma excluded (its only note is trashed).
	want := []struct {
		name  string
		count int
	}{{"alpha", 2}, {"Beta", 1}}
	if len(got) != len(want) {
		t.Fatalf("got %d tags %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Count != w.count {
			t.Errorf("tag[%d] = {%s %d}, want {%s %d}", i, got[i].Name, got[i].Count, w.name, w.count)
		}
	}
}

func TestListTagsOwnerScoped(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	otherID := addUser(t, st)
	ctx := context.Background()

	mine, _ := st.CreateNote(ctx, ownerID, "Mine")
	if _, err := st.AddNoteTag(ctx, ownerID, mine.ID, "private"); err != nil {
		t.Fatalf("tag mine: %v", err)
	}
	theirs, _ := st.CreateNote(ctx, otherID, "Theirs")
	if _, err := st.AddNoteTag(ctx, otherID, theirs.ID, "secret"); err != nil {
		t.Fatalf("tag theirs: %v", err)
	}

	got, err := st.ListTags(ctx, ownerID)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(got) != 1 || got[0].Name != "private" {
		t.Fatalf("owner-scoped tags = %+v, want [private]", got)
	}
}

func TestEmptyTagNameRejected(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	n, _ := st.CreateNote(ctx, ownerID, "N")
	if _, err := st.AddNoteTag(ctx, ownerID, n.ID, "   "); err == nil {
		t.Error("expected error for blank tag name")
	}
}

func TestTagNameTooLongRejected(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	n, _ := st.CreateNote(ctx, ownerID, "N")
	long := strings.Repeat("a", 65)
	if _, err := st.AddNoteTag(ctx, ownerID, n.ID, long); err == nil {
		t.Error("expected error for 65-rune tag name")
	}
}

func TestRenameTag(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	n1, _ := st.CreateNote(ctx, ownerID, "Note 1")
	n2, _ := st.CreateNote(ctx, ownerID, "Note 2")
	tag, err := st.AddNoteTag(ctx, ownerID, n1.ID, "old")
	if err != nil {
		t.Fatalf("add tag: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, ownerID, n2.ID, "old"); err != nil {
		t.Fatalf("add tag to n2: %v", err)
	}

	// Success: rename "old" -> "new"; same row id, cascades to both notes.
	renamed, err := st.RenameTag(ctx, ownerID, tag.ID, "new")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.ID != tag.ID || renamed.Name != "new" {
		t.Fatalf("renamed = %+v, want {id=%s new}", renamed, tag.ID)
	}
	for _, n := range []string{n1.ID, n2.ID} {
		names, err := st.NoteTags(ctx, n)
		if err != nil {
			t.Fatalf("note tags: %v", err)
		}
		if len(names) != 1 || names[0] != "new" {
			t.Errorf("note %s tags = %v, want [new]", n, names)
		}
	}

	// Rename to its own current name must succeed (no conflict).
	if _, err := st.RenameTag(ctx, ownerID, tag.ID, "new"); err != nil {
		t.Errorf("rename to own name: %v", err)
	}

	// Cross-owner tag id -> ErrNotFound.
	otherID := addUser(t, st)
	if _, err := st.RenameTag(ctx, otherID, tag.ID, "hijack"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner rename err = %v, want ErrNotFound", err)
	}

	// Blank name -> ErrTagNameInvalid.
	if _, err := st.RenameTag(ctx, ownerID, tag.ID, "   "); !errors.Is(err, store.ErrTagNameInvalid) {
		t.Errorf("blank name err = %v, want ErrTagNameInvalid", err)
	}

	// Too-long name -> ErrTagNameInvalid.
	if _, err := st.RenameTag(ctx, ownerID, tag.ID, strings.Repeat("a", 65)); !errors.Is(err, store.ErrTagNameInvalid) {
		t.Errorf("too-long name err = %v, want ErrTagNameInvalid", err)
	}

	// Case-insensitive duplicate: create "Foo", rename target to "foo" -> ErrDuplicate.
	if _, err := st.AddNoteTag(ctx, ownerID, n1.ID, "Foo"); err != nil {
		t.Fatalf("add Foo: %v", err)
	}
	if _, err := st.RenameTag(ctx, ownerID, tag.ID, "foo"); !errors.Is(err, store.ErrDuplicate) {
		t.Errorf("duplicate rename err = %v, want ErrDuplicate", err)
	}
}

func TestAddNoteTagConcurrent(t *testing.T) {
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	n, err := st.CreateNote(ctx, ownerID, "Concurrent Note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	const goroutines = 5
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, errs[i] = st.AddNoteTag(ctx, ownerID, n.ID, "concurrent-tag")
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM tags WHERE owner_id=$1 AND lower(name)='concurrent-tag'`, ownerID).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("tag row count = %d, want 1", count)
	}
}
