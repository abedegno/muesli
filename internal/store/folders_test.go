package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// trashTree builds parent P -> child C -> grandchild G, attaches a note to C,
// and returns their ids plus the note id.
func trashTree(t *testing.T, st *store.Store, ownerID string) (p, c, g, noteID string) {
	t.Helper()
	ctx := context.Background()
	pf, err := st.CreateFolder(ctx, ownerID, "P", nil)
	if err != nil {
		t.Fatalf("create P: %v", err)
	}
	cf, err := st.CreateFolder(ctx, ownerID, "C", &pf.ID)
	if err != nil {
		t.Fatalf("create C: %v", err)
	}
	gf, err := st.CreateFolder(ctx, ownerID, "G", &cf.ID)
	if err != nil {
		t.Fatalf("create G: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, "Note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, cf.ID); err != nil {
		t.Fatalf("add note to C: %v", err)
	}
	return pf.ID, cf.ID, gf.ID, note.ID
}

func folderInList(t *testing.T, st *store.Store, ownerID, id string) bool {
	t.Helper()
	list, err := st.ListFolders(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	for _, f := range list {
		if f.ID == id {
			return true
		}
	}
	return false
}

func membershipCount(t *testing.T, pool *pgxpool.Pool, folderID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM note_folders WHERE folder_id=$1`, folderID).Scan(&n); err != nil {
		t.Fatalf("membership count: %v", err)
	}
	return n
}

func folderRowCount(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM folders WHERE id=$1`, id).Scan(&n); err != nil {
		t.Fatalf("folder row count: %v", err)
	}
	return n
}

func folderParentID(t *testing.T, pool *pgxpool.Pool, id string) *string {
	t.Helper()
	var parent *string
	if err := pool.QueryRow(context.Background(),
		`SELECT parent_id FROM folders WHERE id=$1`, id).Scan(&parent); err != nil {
		t.Fatalf("folder parent: %v", err)
	}
	return parent
}

func TestSoftDeleteFolderTrashesSubtree(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	p, c, g, noteID := trashTree(t, st, ownerID)

	if err := st.DeleteFolder(ctx, ownerID, p); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, id := range []string{p, c, g} {
		if folderInList(t, st, ownerID, id) {
			t.Errorf("folder %s still in ListFolders after trash", id)
		}
	}
	// Note untouched and its membership in C preserved.
	if _, err := st.GetNote(ctx, ownerID, noteID); err != nil {
		t.Errorf("note should survive folder trash: %v", err)
	}
	if got := membershipCount(t, pool, c); got != 1 {
		t.Errorf("note_folders membership for C: want 1, got %d", got)
	}
	// Re-deleting an already-trashed root is ErrNotFound.
	if err := st.DeleteFolder(ctx, ownerID, p); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete: want ErrNotFound, got %v", err)
	}
}

func TestListTrashedFoldersRootsOnly(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	p, c, _, _ := trashTree(t, st, ownerID)

	if err := st.DeleteFolder(ctx, ownerID, p); err != nil {
		t.Fatalf("delete: %v", err)
	}
	trashed, err := st.ListTrashedFolders(ctx, ownerID)
	if err != nil {
		t.Fatalf("list trashed: %v", err)
	}
	if len(trashed) != 1 || trashed[0].ID != p {
		var ids []string
		for _, f := range trashed {
			ids = append(ids, f.ID)
		}
		t.Fatalf("want only root P=%s, got %v (c=%s)", p, ids, c)
	}
}

func TestRestoreFolderSubtree(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	p, c, g, _ := trashTree(t, st, ownerID)

	if err := st.DeleteFolder(ctx, ownerID, p); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.RestoreFolder(ctx, ownerID, p); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, id := range []string{p, c, g} {
		if !folderInList(t, st, ownerID, id) {
			t.Errorf("folder %s missing from ListFolders after restore", id)
		}
	}
	if got := membershipCount(t, pool, c); got != 1 {
		t.Errorf("note_folders membership for C after restore: want 1, got %d", got)
	}
	// Restoring a live folder is ErrNotFound.
	if err := st.RestoreFolder(ctx, ownerID, p); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("restore live folder: want ErrNotFound, got %v", err)
	}
}

func TestRestoreFolderOrphansDeadParent(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	pf, err := st.CreateFolder(ctx, ownerID, "P", nil)
	if err != nil {
		t.Fatalf("create P: %v", err)
	}
	cf, err := st.CreateFolder(ctx, ownerID, "C", &pf.ID)
	if err != nil {
		t.Fatalf("create C: %v", err)
	}
	// Trash C alone (its subtree is just C), then trash P separately.
	if err := st.DeleteFolder(ctx, ownerID, cf.ID); err != nil {
		t.Fatalf("delete C: %v", err)
	}
	if err := st.DeleteFolder(ctx, ownerID, pf.ID); err != nil {
		t.Fatalf("delete P: %v", err)
	}
	// Restore C while its parent P remains trashed: C must be orphaned to top level.
	if err := st.RestoreFolder(ctx, ownerID, cf.ID); err != nil {
		t.Fatalf("restore C: %v", err)
	}
	if parent := folderParentID(t, pool, cf.ID); parent != nil {
		t.Errorf("restored C should have null parent (dead parent), got %v", *parent)
	}
}

func TestPurgeFolderCascades(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	p, c, _, _ := trashTree(t, st, ownerID)

	if err := st.DeleteFolder(ctx, ownerID, p); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Cross-owner purge is ErrNotFound and leaves rows intact.
	if err := st.PurgeFolder(ctx, other, p); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner purge: want ErrNotFound, got %v", err)
	}
	if err := st.PurgeFolder(ctx, ownerID, p); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n := folderRowCount(t, pool, p); n != 0 {
		t.Errorf("P row should be gone, count=%d", n)
	}
	if n := folderRowCount(t, pool, c); n != 0 {
		t.Errorf("C row should cascade-delete, count=%d", n)
	}
	if err := st.PurgeFolder(ctx, ownerID, p); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second purge: want ErrNotFound, got %v", err)
	}
}

func TestPurgeExpiredFolders(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	oldF, err := st.CreateFolder(ctx, ownerID, "Old", nil)
	if err != nil {
		t.Fatalf("create old: %v", err)
	}
	newF, err := st.CreateFolder(ctx, ownerID, "New", nil)
	if err != nil {
		t.Fatalf("create new: %v", err)
	}
	if err := st.DeleteFolder(ctx, ownerID, oldF.ID); err != nil {
		t.Fatalf("delete old: %v", err)
	}
	if err := st.DeleteFolder(ctx, ownerID, newF.ID); err != nil {
		t.Fatalf("delete new: %v", err)
	}
	// Backdate the old one beyond the retention window.
	if _, err := pool.Exec(ctx,
		`UPDATE folders SET deleted_at = now() - interval '31 days' WHERE id=$1`, oldF.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, err := st.PurgeExpiredFolders(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("purge expired: %v", err)
	}
	if n != 1 {
		t.Errorf("purged count: want 1, got %d", n)
	}
	if c := folderRowCount(t, pool, oldF.ID); c != 0 {
		t.Errorf("old folder should be purged, count=%d", c)
	}
	if c := folderRowCount(t, pool, newF.ID); c != 1 {
		t.Errorf("new folder should remain, count=%d", c)
	}
}

// TestTrashedFolderIsImmutable proves a trashed folder can't be filed into, renamed,
// re-parented, or used as a parent — the API contract must treat it as gone (404).
func TestTrashedFolderIsImmutable(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	trashed, _ := st.CreateFolder(ctx, ownerID, "Gone", nil)
	live, _ := st.CreateFolder(ctx, ownerID, "Here", nil)
	note, _ := st.CreateNote(ctx, ownerID, "n")
	if err := st.DeleteFolder(ctx, ownerID, trashed.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Can't add a note to a trashed folder.
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, trashed.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("AddNoteFolder into trashed = %v, want ErrNotFound", err)
	}
	// Can't rename/re-parent a trashed folder.
	if _, err := st.UpdateFolder(ctx, ownerID, trashed.ID, "Renamed", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateFolder on trashed = %v, want ErrNotFound", err)
	}
	// Can't set a trashed folder as a live folder's parent.
	if _, err := st.UpdateFolder(ctx, ownerID, live.ID, "Here", &trashed.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("re-parent under trashed = %v, want ErrInvalidParent", err)
	}
}

func TestFolderCRUD(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	f, err := st.CreateFolder(ctx, ownerID, "  Clients  ", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if f.ID == "" || f.Name != "Clients" {
		t.Fatalf("bad folder %+v", f)
	}
	folders, err := st.ListFolders(ctx, ownerID)
	if err != nil || len(folders) != 1 {
		t.Fatalf("list: %v len=%d", err, len(folders))
	}
	updated, err := st.UpdateFolder(ctx, ownerID, f.ID, "Renamed", nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != f.ID || updated.Name != "Renamed" || updated.CreatedAt.IsZero() {
		t.Fatalf("update returned %+v, want id=%s name=Renamed created_at non-zero", updated, f.ID)
	}
	if err := st.DeleteFolder(ctx, ownerID, f.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if folders, _ = st.ListFolders(ctx, ownerID); len(folders) != 0 {
		t.Errorf("after delete len=%d", len(folders))
	}
}

func TestFolderNameValidation(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	if _, err := st.CreateFolder(ctx, ownerID, "   ", nil); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := st.CreateFolder(ctx, ownerID, strings.Repeat("x", 81), nil); err == nil {
		t.Error("too-long name should fail")
	}
}

func TestFolderDuplicateName(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	if _, err := st.CreateFolder(ctx, ownerID, "Clients", nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := st.CreateFolder(ctx, ownerID, "clients", nil)
	if !errors.Is(err, store.ErrDuplicate) {
		t.Errorf("want ErrDuplicate, got %v", err)
	}
}

func TestFolderOwnerScoping(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	f, _ := st.CreateFolder(ctx, ownerID, "Mine", nil)
	if _, err := st.UpdateFolder(ctx, other, f.ID, "X", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner update: want ErrNotFound, got %v", err)
	}
	if err := st.DeleteFolder(ctx, other, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner delete: want ErrNotFound, got %v", err)
	}
}

func TestNoteFolderMembership(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	note, err := st.CreateNote(ctx, ownerID, "Note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	f, _ := st.CreateFolder(ctx, ownerID, "Clients", nil)

	if err := st.AddNoteFolder(ctx, ownerID, note.ID, f.ID); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, f.ID); err != nil { // idempotent
		t.Fatalf("add twice: %v", err)
	}
	ids, err := st.NoteFolderIDs(ctx, note.ID)
	if err != nil || len(ids) != 1 || ids[0] != f.ID {
		t.Fatalf("ids: %v %v", err, ids)
	}
	if err := st.RemoveNoteFolder(ctx, ownerID, note.ID, f.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ids, _ = st.NoteFolderIDs(ctx, note.ID); len(ids) != 0 {
		t.Errorf("after remove len=%d", len(ids))
	}
}

func TestAddNoteFolderCrossOwner(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	note, _ := st.CreateNote(ctx, ownerID, "Note")
	f, _ := st.CreateFolder(ctx, other, "Theirs", nil)
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner folder: want ErrNotFound, got %v", err)
	}
}

func TestFolderNestingAndCycle(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	root, err := st.CreateFolder(ctx, owner, "Root", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := st.CreateFolder(ctx, owner, "Child", &root.ID)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != root.ID {
		t.Fatalf("child parent: %+v", child)
	}
	// cycle: root cannot become a child of its own descendant
	if _, err := st.UpdateFolder(ctx, owner, root.ID, "Root", &child.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("cycle: want ErrInvalidParent, got %v", err)
	}
	// self-parent rejected
	if _, err := st.UpdateFolder(ctx, owner, root.ID, "Root", &root.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("self-parent: want ErrInvalidParent, got %v", err)
	}
	// ListFolders exposes parent_id
	list, _ := st.ListFolders(ctx, owner)
	var sawChildParent bool
	for _, f := range list {
		if f.ID == child.ID && f.ParentID != nil && *f.ParentID == root.ID {
			sawChildParent = true
		}
	}
	if !sawChildParent {
		t.Error("ListFolders missing child's parent_id")
	}
}

func TestFolderDepthLimit(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	var parent *string
	for i := 0; i < 5; i++ { // depths 1..5 should succeed
		f, err := st.CreateFolder(ctx, owner, "L"+string(rune('a'+i)), parent)
		if err != nil {
			t.Fatalf("depth %d: %v", i+1, err)
		}
		parent = &f.ID
	}
	// 6th level should be rejected
	if _, err := st.CreateFolder(ctx, owner, "L6", parent); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("depth 6: want ErrInvalidParent, got %v", err)
	}
}

// listFolderIDs returns ListFolders ids in order, filtered to the given parent
// (nil → top level).
func listFolderIDs(t *testing.T, st *store.Store, ownerID string, parent *string) []string {
	t.Helper()
	list, err := st.ListFolders(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	var ids []string
	for _, f := range list {
		if (parent == nil && f.ParentID == nil) || (parent != nil && f.ParentID != nil && *f.ParentID == *parent) {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

func listFolderNoteIDs(t *testing.T, st *store.Store, ownerID, folderID string) []string {
	t.Helper()
	notes, err := st.ListNotes(context.Background(), ownerID, store.ListNotesFilter{FolderID: folderID, FolderIDSet: true})
	if err != nil {
		t.Fatalf("list folder notes: %v", err)
	}
	ids := make([]string, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	return ids
}

func folderPosition(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var p int
	if err := pool.QueryRow(context.Background(),
		`SELECT position FROM folders WHERE id=$1`, id).Scan(&p); err != nil {
		t.Fatalf("folder position: %v", err)
	}
	return p
}

func TestListFoldersPositionOrder(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	// Created in this order; ListFolders should return them by position (= creation
	// order here), not by name.
	c, _ := st.CreateFolder(ctx, ownerID, "Charlie", nil)
	a, _ := st.CreateFolder(ctx, ownerID, "Alpha", nil)
	b, _ := st.CreateFolder(ctx, ownerID, "Bravo", nil)
	got := listFolderIDs(t, st, ownerID, nil)
	want := []string{c.ID, a.ID, b.ID}
	if !equalStrings(got, want) {
		t.Errorf("position order: got %v want %v", got, want)
	}
}

func TestCreateFolderAppendsPosition(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	_, _ = st.CreateFolder(ctx, ownerID, "One", nil)
	_, _ = st.CreateFolder(ctx, ownerID, "Two", nil)
	third, _ := st.CreateFolder(ctx, ownerID, "Three", nil)
	if p := folderPosition(t, pool, third.ID); p != 2 {
		t.Errorf("3rd sibling position: want 2, got %d", p)
	}
}

func TestReorderFolderAfterSibling(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	a, _ := st.CreateFolder(ctx, ownerID, "A", nil)
	b, _ := st.CreateFolder(ctx, ownerID, "B", nil)
	c, _ := st.CreateFolder(ctx, ownerID, "C", nil)
	// Move c right after a: order becomes a, c, b.
	if err := st.ReorderFolder(ctx, ownerID, c.ID, &a.ID); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	got := listFolderIDs(t, st, ownerID, nil)
	want := []string{a.ID, c.ID, b.ID}
	if !equalStrings(got, want) {
		t.Errorf("after reorder: got %v want %v", got, want)
	}
}

func TestReorderFolderToFirst(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	a, _ := st.CreateFolder(ctx, ownerID, "A", nil)
	b, _ := st.CreateFolder(ctx, ownerID, "B", nil)
	c, _ := st.CreateFolder(ctx, ownerID, "C", nil)
	if err := st.ReorderFolder(ctx, ownerID, c.ID, nil); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	got := listFolderIDs(t, st, ownerID, nil)
	want := []string{c.ID, a.ID, b.ID}
	if !equalStrings(got, want) {
		t.Errorf("reorder to first: got %v want %v", got, want)
	}
}

func TestReorderFolderNonSibling(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	a, _ := st.CreateFolder(ctx, ownerID, "A", nil)
	// b is under a different parent, so it's not a sibling of c.
	parent, _ := st.CreateFolder(ctx, ownerID, "P", nil)
	b, _ := st.CreateFolder(ctx, ownerID, "B", &parent.ID)
	c, _ := st.CreateFolder(ctx, ownerID, "C", nil)
	_ = a
	if err := st.ReorderFolder(ctx, ownerID, c.ID, &b.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("non-sibling afterID: want ErrInvalidParent, got %v", err)
	}
}

func TestReorderFolderCrossOwner(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	f, _ := st.CreateFolder(ctx, ownerID, "Mine", nil)
	if err := st.ReorderFolder(ctx, other, f.ID, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner reorder: want ErrNotFound, got %v", err)
	}
}

func TestReorderNoteInFolderAfterSibling(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	f, _ := st.CreateFolder(ctx, ownerID, "Clients", nil)
	n1, _ := st.CreateNote(ctx, ownerID, "One")
	n2, _ := st.CreateNote(ctx, ownerID, "Two")
	n3, _ := st.CreateNote(ctx, ownerID, "Three")
	_ = st.AddNoteFolder(ctx, ownerID, n1.ID, f.ID)
	_ = st.AddNoteFolder(ctx, ownerID, n2.ID, f.ID)
	_ = st.AddNoteFolder(ctx, ownerID, n3.ID, f.ID)

	if err := st.ReorderNoteInFolder(ctx, ownerID, f.ID, n3.ID, &n1.ID); err != nil {
		t.Fatalf("reorder note: %v", err)
	}
	got := listFolderNoteIDs(t, st, ownerID, f.ID)
	want := []string{n1.ID, n3.ID, n2.ID}
	if !equalStrings(got, want) {
		t.Errorf("after reorder: got %v want %v", got, want)
	}
}

func TestReorderNoteInFolderToFirst(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	f, _ := st.CreateFolder(ctx, ownerID, "Clients", nil)
	n1, _ := st.CreateNote(ctx, ownerID, "One")
	n2, _ := st.CreateNote(ctx, ownerID, "Two")
	n3, _ := st.CreateNote(ctx, ownerID, "Three")
	_ = st.AddNoteFolder(ctx, ownerID, n1.ID, f.ID)
	_ = st.AddNoteFolder(ctx, ownerID, n2.ID, f.ID)
	_ = st.AddNoteFolder(ctx, ownerID, n3.ID, f.ID)

	if err := st.ReorderNoteInFolder(ctx, ownerID, f.ID, n3.ID, nil); err != nil {
		t.Fatalf("reorder note to first: %v", err)
	}
	got := listFolderNoteIDs(t, st, ownerID, f.ID)
	want := []string{n3.ID, n1.ID, n2.ID}
	if !equalStrings(got, want) {
		t.Errorf("reorder note to first: got %v want %v", got, want)
	}
}

func TestReorderNoteInFolderRejectsInvalidSiblingAndMissingMembership(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	f1, _ := st.CreateFolder(ctx, ownerID, "Clients", nil)
	f2, _ := st.CreateFolder(ctx, ownerID, "Other", nil)
	n1, _ := st.CreateNote(ctx, ownerID, "One")
	n2, _ := st.CreateNote(ctx, ownerID, "Two")
	n3, _ := st.CreateNote(ctx, ownerID, "Three")
	_ = st.AddNoteFolder(ctx, ownerID, n1.ID, f1.ID)
	_ = st.AddNoteFolder(ctx, ownerID, n2.ID, f1.ID)
	_ = st.AddNoteFolder(ctx, ownerID, n3.ID, f2.ID)

	if err := st.ReorderNoteInFolder(ctx, ownerID, f1.ID, n1.ID, &n1.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("self after: want ErrInvalidParent, got %v", err)
	}
	if err := st.ReorderNoteInFolder(ctx, ownerID, f1.ID, n1.ID, &n3.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("cross-folder after: want ErrInvalidParent, got %v", err)
	}
	if err := st.ReorderNoteInFolder(ctx, ownerID, f1.ID, n3.ID, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing membership: want ErrNotFound, got %v", err)
	}
}

func TestReparentLandsAtEnd(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	// New parent already has two children at positions 0 and 1.
	dst, _ := st.CreateFolder(ctx, ownerID, "Dst", nil)
	_, _ = st.CreateFolder(ctx, ownerID, "X", &dst.ID)
	_, _ = st.CreateFolder(ctx, ownerID, "Y", &dst.ID)
	// A top-level folder moved under dst should land at position 2.
	mover, _ := st.CreateFolder(ctx, ownerID, "Mover", nil)
	if _, err := st.UpdateFolder(ctx, ownerID, mover.ID, "Mover", &dst.ID); err != nil {
		t.Fatalf("reparent: %v", err)
	}
	if p := folderPosition(t, pool, mover.ID); p != 2 {
		t.Errorf("reparented folder position: want 2, got %d", p)
	}
	// A pure rename (no parent change) leaves position untouched.
	posBefore := folderPosition(t, pool, dst.ID)
	if _, err := st.UpdateFolder(ctx, ownerID, dst.ID, "Dst2", nil); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if p := folderPosition(t, pool, dst.ID); p != posBefore {
		t.Errorf("rename changed position: was %d, now %d", posBefore, p)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFolderParentCrossOwner(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	theirs, _ := st.CreateFolder(ctx, other, "Theirs", nil)
	if _, err := st.CreateFolder(ctx, owner, "Mine", &theirs.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("cross-owner parent: want ErrInvalidParent, got %v", err)
	}
}

func TestListFoldersNoteCountRecursive(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	// Create parent and two children.
	parent, err := st.CreateFolder(ctx, ownerID, "Parent", nil)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child1, err := st.CreateFolder(ctx, ownerID, "Child1", &parent.ID)
	if err != nil {
		t.Fatalf("create child1: %v", err)
	}
	child2, err := st.CreateFolder(ctx, ownerID, "Child2", &parent.ID)
	if err != nil {
		t.Fatalf("create child2: %v", err)
	}

	// Create one note in each child folder.
	note1, err := st.CreateNote(ctx, ownerID, "Note in child1")
	if err != nil {
		t.Fatalf("create note1: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note1.ID, child1.ID); err != nil {
		t.Fatalf("add note1 to child1: %v", err)
	}
	note2, err := st.CreateNote(ctx, ownerID, "Note in child2")
	if err != nil {
		t.Fatalf("create note2: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note2.ID, child2.ID); err != nil {
		t.Fatalf("add note2 to child2: %v", err)
	}

	folders, err := st.ListFolders(ctx, ownerID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}

	counts := map[string]int{}
	for _, f := range folders {
		counts[f.ID] = f.NoteCount
	}

	if got := counts[parent.ID]; got != 2 {
		t.Errorf("parent NoteCount: want 2 (recursive), got %d", got)
	}
	if got := counts[child1.ID]; got != 1 {
		t.Errorf("child1 NoteCount: want 1, got %d", got)
	}
	if got := counts[child2.ID]; got != 1 {
		t.Errorf("child2 NoteCount: want 1, got %d", got)
	}
}

func TestListFoldersNoteCountExcludesDeleted(t *testing.T) {
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	f, err := st.CreateFolder(ctx, ownerID, "Folder", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, "Note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, f.ID); err != nil {
		t.Fatalf("add note to folder: %v", err)
	}

	// Soft-delete the note.
	if _, err := pool.Exec(ctx, `UPDATE notes SET deleted_at = now() WHERE id = $1`, note.ID); err != nil {
		t.Fatalf("soft-delete note: %v", err)
	}

	folders, err := st.ListFolders(ctx, ownerID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	for _, fl := range folders {
		if fl.ID == f.ID && fl.NoteCount != 0 {
			t.Errorf("NoteCount should exclude deleted notes, got %d", fl.NoteCount)
		}
	}
}

// TestFolderMoveSubtreeDepthGuard verifies that moving a folder with a subtree
// does not silently exceed the maxFolderDepth (5) cap.
func TestFolderMoveSubtreeDepthGuard(t *testing.T) {
	ctx := context.Background()

	t.Run("leaf into depth-4 target allowed", func(t *testing.T) {
		// Build chain A->B->C->D (D is at depth 4 in the store's 1-indexed system).
		st, ownerID, _ := newStoreWithOwner(t)
		a, err := st.CreateFolder(ctx, ownerID, "A", nil)
		if err != nil {
			t.Fatalf("create A: %v", err)
		}
		b, err := st.CreateFolder(ctx, ownerID, "B", &a.ID)
		if err != nil {
			t.Fatalf("create B: %v", err)
		}
		c, err := st.CreateFolder(ctx, ownerID, "C", &b.ID)
		if err != nil {
			t.Fatalf("create C: %v", err)
		}
		d, err := st.CreateFolder(ctx, ownerID, "D", &c.ID)
		if err != nil {
			t.Fatalf("create D: %v", err)
		}
		// L is a plain leaf at top level.
		l, err := st.CreateFolder(ctx, ownerID, "L", nil)
		if err != nil {
			t.Fatalf("create L: %v", err)
		}
		// Move L under D: depth=4, H=0, 4+0+1=5 == maxFolderDepth → allowed.
		if _, err := st.UpdateFolder(ctx, ownerID, l.ID, "L", &d.ID); err != nil {
			t.Errorf("leaf under depth-4: want success, got %v", err)
		}
	})

	t.Run("2-level subtree into depth-2 target allowed", func(t *testing.T) {
		// Build chain A->B (B at depth 2).
		st, ownerID, _ := newStoreWithOwner(t)
		a, err := st.CreateFolder(ctx, ownerID, "A", nil)
		if err != nil {
			t.Fatalf("create A: %v", err)
		}
		b, err := st.CreateFolder(ctx, ownerID, "B", &a.ID)
		if err != nil {
			t.Fatalf("create B: %v", err)
		}
		// Create P->Q->R (subtree height=2).
		p, err := st.CreateFolder(ctx, ownerID, "P", nil)
		if err != nil {
			t.Fatalf("create P: %v", err)
		}
		q, err := st.CreateFolder(ctx, ownerID, "Q", &p.ID)
		if err != nil {
			t.Fatalf("create Q: %v", err)
		}
		if _, err := st.CreateFolder(ctx, ownerID, "R", &q.ID); err != nil {
			t.Fatalf("create R: %v", err)
		}
		// Move P under B: depth=2, H=2, 2+2+1=5 == maxFolderDepth → allowed.
		if _, err := st.UpdateFolder(ctx, ownerID, p.ID, "P", &b.ID); err != nil {
			t.Errorf("2-level subtree under depth-2: want success, got %v", err)
		}
	})

	t.Run("2-level subtree into depth-4 target rejected", func(t *testing.T) {
		// Build chain A->B->C->D (D at depth 4).
		st, ownerID, _ := newStoreWithOwner(t)
		a, err := st.CreateFolder(ctx, ownerID, "A", nil)
		if err != nil {
			t.Fatalf("create A: %v", err)
		}
		b, err := st.CreateFolder(ctx, ownerID, "B", &a.ID)
		if err != nil {
			t.Fatalf("create B: %v", err)
		}
		c, err := st.CreateFolder(ctx, ownerID, "C", &b.ID)
		if err != nil {
			t.Fatalf("create C: %v", err)
		}
		d, err := st.CreateFolder(ctx, ownerID, "D", &c.ID)
		if err != nil {
			t.Fatalf("create D: %v", err)
		}
		// Create P->Q (subtree height=1).
		p, err := st.CreateFolder(ctx, ownerID, "P", nil)
		if err != nil {
			t.Fatalf("create P: %v", err)
		}
		if _, err := st.CreateFolder(ctx, ownerID, "Q", &p.ID); err != nil {
			t.Fatalf("create Q: %v", err)
		}
		// Move P under D: depth=4, H=1, 4+1+1=6 > 5 → rejected.
		_, err = st.UpdateFolder(ctx, ownerID, p.ID, "P", &d.ID)
		if !errors.Is(err, store.ErrInvalidParent) {
			t.Errorf("2-level subtree under depth-4: want ErrInvalidParent, got %v", err)
		}
	})
}

func TestReorderFolder_EdgeCases(t *testing.T) {
	// Case 1: self-reference (afterID == id)
	t.Run("self-reference", func(t *testing.T) {
		st, ownerID, _ := newStoreWithOwner(t)
		ctx := context.Background()
		f, err := st.CreateFolder(ctx, ownerID, "Solo", nil)
		if err != nil {
			t.Fatalf("create folder: %v", err)
		}
		if err := st.ReorderFolder(ctx, ownerID, f.ID, &f.ID); !errors.Is(err, store.ErrInvalidParent) {
			t.Errorf("self-reference afterID: want ErrInvalidParent, got %v", err)
		}
	})

	// Case 2: cross-parent afterID (B is under P2, not a sibling of A which is under P1)
	t.Run("cross-parent afterID", func(t *testing.T) {
		st, ownerID, _ := newStoreWithOwner(t)
		ctx := context.Background()
		p1, err := st.CreateFolder(ctx, ownerID, "P1", nil)
		if err != nil {
			t.Fatalf("create P1: %v", err)
		}
		a, err := st.CreateFolder(ctx, ownerID, "A", &p1.ID)
		if err != nil {
			t.Fatalf("create A: %v", err)
		}
		p2, err := st.CreateFolder(ctx, ownerID, "P2", nil)
		if err != nil {
			t.Fatalf("create P2: %v", err)
		}
		b, err := st.CreateFolder(ctx, ownerID, "B", &p2.ID)
		if err != nil {
			t.Fatalf("create B: %v", err)
		}
		if err := st.ReorderFolder(ctx, ownerID, a.ID, &b.ID); !errors.Is(err, store.ErrInvalidParent) {
			t.Errorf("cross-parent afterID: want ErrInvalidParent, got %v", err)
		}
	})

	// Case 3: trashed folder cannot be reordered
	t.Run("trashed folder", func(t *testing.T) {
		st, ownerID, _ := newStoreWithOwner(t)
		ctx := context.Background()
		f, err := st.CreateFolder(ctx, ownerID, "Gone", nil)
		if err != nil {
			t.Fatalf("create folder: %v", err)
		}
		if err := st.DeleteFolder(ctx, ownerID, f.ID); err != nil {
			t.Fatalf("delete folder: %v", err)
		}
		if err := st.ReorderFolder(ctx, ownerID, f.ID, nil); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("trashed folder reorder: want ErrNotFound, got %v", err)
		}
	})
}
