package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/google/uuid"
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

// TestResolveFolders exercises the owner-scoped exact-id folder resolver: a
// live folder, a soft-deleted root and its auto-trashed descendant (trashed
// via DeleteFolder, so the child inherits deleted_at without its own delete
// call), another owner's folder, and a fresh unknown id.
func TestResolveFolders(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	live, err := st.CreateFolder(ctx, ownerID, "Live", nil)
	if err != nil {
		t.Fatalf("create live: %v", err)
	}
	root, err := st.CreateFolder(ctx, ownerID, "Root", nil)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := st.CreateFolder(ctx, ownerID, "Child", &root.ID)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := st.DeleteFolder(ctx, ownerID, root.ID); err != nil {
		t.Fatalf("trash root: %v", err)
	}
	theirs, err := st.CreateFolder(ctx, other, "Theirs", nil)
	if err != nil {
		t.Fatalf("create other's folder: %v", err)
	}

	ids := []string{live.ID, live.ID, root.ID, child.ID, theirs.ID, uuid.NewString()}
	got, err := st.ResolveFolders(ctx, ownerID, ids)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 distinct owned rows, got %d: %+v", len(got), got)
	}
	byID := map[string]model.Folder{}
	for _, f := range got {
		byID[f.ID] = f
	}
	if _, ok := byID[theirs.ID]; ok {
		t.Errorf("other-owner folder leaked into result: %+v", byID)
	}

	l, ok := byID[live.ID]
	if !ok {
		t.Fatalf("live folder missing from result: %+v", byID)
	}
	if l.DeletedAt != nil {
		t.Errorf("live folder DeletedAt = %v, want nil", l.DeletedAt)
	}
	if l.Name != "Live" || l.CreatedAt.IsZero() {
		t.Errorf("live folder fields wrong: %+v", l)
	}

	r, ok := byID[root.ID]
	if !ok {
		t.Fatalf("trashed root missing from result: %+v", byID)
	}
	if r.DeletedAt == nil {
		t.Errorf("trashed root DeletedAt = nil, want non-nil")
	}
	if r.ParentID != nil {
		t.Errorf("root ParentID = %v, want nil", r.ParentID)
	}

	c, ok := byID[child.ID]
	if !ok {
		t.Fatalf("auto-trashed descendant missing from result: %+v", byID)
	}
	if c.DeletedAt == nil {
		t.Errorf("auto-trashed descendant DeletedAt = nil, want non-nil")
	}
	if c.ParentID == nil || *c.ParentID != root.ID {
		t.Errorf("child ParentID = %v, want %s", c.ParentID, root.ID)
	}
	if c.Name != "Child" || c.CreatedAt.IsZero() {
		t.Errorf("child fields wrong: %+v", c)
	}
}

// ---------------------------------------------------------------------------
// Issue #12: deployment team boundary + shared folders.
// ---------------------------------------------------------------------------

func TestListFoldersOwnedAndSharedVisibility(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	mine, err := st.CreateFolder(ctx, ownerID, "Mine", nil)
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if mine.Visibility != model.FolderPrivate || !mine.IsOwner || mine.OwnerID != ownerID {
		t.Fatalf("new folder should be private+owned: %+v", mine)
	}

	theirsPrivate, err := st.CreateFolder(ctx, other, "TheirsPrivate", nil)
	if err != nil {
		t.Fatalf("create theirs private: %v", err)
	}
	theirsShared, err := st.CreateFolder(ctx, other, "TheirsShared", nil)
	if err != nil {
		t.Fatalf("create theirs shared: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, theirsShared.ID, model.FolderShared); err != nil {
		t.Fatalf("share theirs: %v", err)
	}
	theirsTrashedShared, err := st.CreateFolder(ctx, other, "TheirsTrashedShared", nil)
	if err != nil {
		t.Fatalf("create theirs trashed shared: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, theirsTrashedShared.ID, model.FolderShared); err != nil {
		t.Fatalf("share theirs trashed: %v", err)
	}
	if err := st.DeleteFolder(ctx, other, theirsTrashedShared.ID); err != nil {
		t.Fatalf("trash theirs shared: %v", err)
	}

	got, err := st.ListFolders(ctx, ownerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]model.Folder{}
	for _, f := range got {
		byID[f.ID] = f
	}
	if _, ok := byID[theirsPrivate.ID]; ok {
		t.Errorf("private non-owned folder leaked into list: %+v", byID)
	}
	if _, ok := byID[theirsTrashedShared.ID]; ok {
		t.Errorf("trashed shared folder leaked into list: %+v", byID)
	}
	m, ok := byID[mine.ID]
	if !ok || !m.IsOwner {
		t.Fatalf("owned private folder missing or IsOwner false: %+v", byID)
	}
	ts, ok := byID[theirsShared.ID]
	if !ok {
		t.Fatalf("live shared non-owned folder missing from list: %+v", byID)
	}
	if ts.IsOwner {
		t.Errorf("shared non-owned folder IsOwner = true, want false")
	}
	if ts.OwnerID != other {
		t.Errorf("shared folder owner_id = %q, want %q", ts.OwnerID, other)
	}
}

func TestListFoldersSanitizesInvisibleAncestryAndRetainsVisible(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	// other: private root -> shared child. Requester can see the child (shared)
	// but not the root (private), so the response must promote the child to a
	// null parent WITHOUT mutating the stored parent_id.
	privateRoot, err := st.CreateFolder(ctx, other, "PrivateRoot", nil)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	sharedChild, err := st.CreateFolder(ctx, other, "SharedChild", &privateRoot.ID)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, sharedChild.ID, model.FolderShared); err != nil {
		t.Fatalf("share child: %v", err)
	}

	// other: shared root -> shared grandchild. Both visible -> real relationship kept.
	sharedRoot, err := st.CreateFolder(ctx, other, "SharedRoot", nil)
	if err != nil {
		t.Fatalf("create shared root: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, sharedRoot.ID, model.FolderShared); err != nil {
		t.Fatalf("share root: %v", err)
	}
	sharedGrandchild, err := st.CreateFolder(ctx, other, "SharedGrandchild", &sharedRoot.ID)
	if err != nil {
		t.Fatalf("create grandchild: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, sharedGrandchild.ID, model.FolderShared); err != nil {
		t.Fatalf("share grandchild: %v", err)
	}

	got, err := st.ListFolders(ctx, ownerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]model.Folder{}
	for _, f := range got {
		byID[f.ID] = f
	}
	if _, ok := byID[privateRoot.ID]; ok {
		t.Fatalf("private root should not be visible: %+v", byID)
	}
	sc, ok := byID[sharedChild.ID]
	if !ok {
		t.Fatalf("shared child missing: %+v", byID)
	}
	if sc.ParentID != nil {
		t.Errorf("promoted shared child ParentID = %v, want nil (invisible ancestor)", *sc.ParentID)
	}
	sg, ok := byID[sharedGrandchild.ID]
	if !ok {
		t.Fatalf("shared grandchild missing: %+v", byID)
	}
	if sg.ParentID == nil || *sg.ParentID != sharedRoot.ID {
		t.Errorf("shared grandchild ParentID = %v, want %s (both visible)", sg.ParentID, sharedRoot.ID)
	}

	// Stored parent_id must be untouched by the response sanitization.
	stored, err := st.GetFolder(ctx, other, sharedChild.ID)
	if err != nil {
		t.Fatalf("owner re-read: %v", err)
	}
	if stored.ParentID == nil || *stored.ParentID != privateRoot.ID {
		t.Errorf("stored parent_id changed by list sanitization: got %v, want %s", stored.ParentID, privateRoot.ID)
	}
}

func TestListFoldersCountsDirectMembershipOnly(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	shared, err := st.CreateFolder(ctx, ownerID, "Shared", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, shared.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}
	mine, err := st.CreateNote(ctx, ownerID, "Mine")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, mine.ID, shared.ID); err != nil {
		t.Fatalf("file mine: %v", err)
	}
	theirs, err := st.CreateNote(ctx, other, "Theirs")
	if err != nil {
		t.Fatalf("create their note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, other, theirs.ID, shared.ID); err != nil {
		t.Fatalf("file theirs: %v", err)
	}

	// Both the folder owner and the contributor see the same count: 2.
	for _, requester := range []string{ownerID, other} {
		got, err := st.ListFolders(ctx, requester)
		if err != nil {
			t.Fatalf("list as %s: %v", requester, err)
		}
		var f *model.Folder
		for i := range got {
			if got[i].ID == shared.ID {
				f = &got[i]
			}
		}
		if f == nil {
			t.Fatalf("shared folder missing for requester %s", requester)
		}
		if f.NoteCount != 2 {
			t.Errorf("requester %s: note_count = %d, want 2", requester, f.NoteCount)
		}
	}
}

func TestListFoldersRecursiveCountExcludesPrivateDescendantForNonOwner(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	// other: shared root -> private child. The root is live-shared, but the
	// child stays private (independent visibility within one owner's tree).
	sharedRoot, err := st.CreateFolder(ctx, other, "SharedRoot", nil)
	if err != nil {
		t.Fatalf("create shared root: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, sharedRoot.ID, model.FolderShared); err != nil {
		t.Fatalf("share root: %v", err)
	}
	privateChild, err := st.CreateFolder(ctx, other, "PrivateChild", &sharedRoot.ID)
	if err != nil {
		t.Fatalf("create private child: %v", err)
	}

	// One note filed directly in the shared root, one filed only in the
	// private child.
	inRoot, err := st.CreateNote(ctx, other, "In root")
	if err != nil {
		t.Fatalf("create note in root: %v", err)
	}
	if err := st.AddNoteFolder(ctx, other, inRoot.ID, sharedRoot.ID); err != nil {
		t.Fatalf("file in root: %v", err)
	}
	inPrivateChild, err := st.CreateNote(ctx, other, "In private child")
	if err != nil {
		t.Fatalf("create note in private child: %v", err)
	}
	if err := st.AddNoteFolder(ctx, other, inPrivateChild.ID, privateChild.ID); err != nil {
		t.Fatalf("file in private child: %v", err)
	}

	// The owner sees both notes recursively (their own subtree, any visibility).
	ownList, err := st.ListFolders(ctx, other)
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	var ownerRoot *model.Folder
	for i := range ownList {
		if ownList[i].ID == sharedRoot.ID {
			ownerRoot = &ownList[i]
		}
	}
	if ownerRoot == nil {
		t.Fatalf("owner: shared root missing from list")
	}
	if ownerRoot.NoteCount != 2 {
		t.Errorf("owner recursive count = %d, want 2 (includes private child)", ownerRoot.NoteCount)
	}

	// A non-owner requester sees only the note directly in the shared root:
	// the private child's note must not leak into the recursive count.
	sharedReaderList, err := st.ListFolders(ctx, ownerID)
	if err != nil {
		t.Fatalf("non-owner list: %v", err)
	}
	var readerRoot *model.Folder
	for i := range sharedReaderList {
		if sharedReaderList[i].ID == sharedRoot.ID {
			readerRoot = &sharedReaderList[i]
		}
	}
	if readerRoot == nil {
		t.Fatalf("non-owner: shared root missing from list")
	}
	if readerRoot.NoteCount != 1 {
		t.Errorf("non-owner recursive count = %d, want 1 (private child excluded, no leak)", readerRoot.NoteCount)
	}
	// The private child itself must not even appear in the non-owner's list.
	for _, f := range sharedReaderList {
		if f.ID == privateChild.ID {
			t.Errorf("private child leaked into non-owner list: %+v", f)
		}
	}
}

func TestSetFolderVisibilityValuesAndAuthorization(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	f, err := st.CreateFolder(ctx, ownerID, "F", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := st.SetFolderVisibility(ctx, ownerID, f.ID, "public"); err == nil {
		t.Error("invalid visibility value should error")
	}

	shared, err := st.SetFolderVisibility(ctx, ownerID, f.ID, model.FolderShared)
	if err != nil {
		t.Fatalf("set shared: %v", err)
	}
	if shared.Visibility != model.FolderShared || !shared.IsOwner {
		t.Fatalf("set shared result: %+v", shared)
	}

	back, err := st.SetFolderVisibility(ctx, ownerID, f.ID, model.FolderPrivate)
	if err != nil {
		t.Fatalf("set private: %v", err)
	}
	if back.Visibility != model.FolderPrivate {
		t.Fatalf("set private result: %+v", back)
	}

	// Private non-owner: ErrNotFound (invisible).
	if _, err := st.SetFolderVisibility(ctx, other, f.ID, model.FolderShared); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("private foreign: want ErrNotFound, got %v", err)
	}

	// Shared non-owner: ErrForbidden (visible, not theirs to change).
	if _, err := st.SetFolderVisibility(ctx, ownerID, f.ID, model.FolderShared); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, f.ID, model.FolderPrivate); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("shared foreign: want ErrForbidden, got %v", err)
	}
}

func TestFolderOwnerOnlyMutationsDistinguishForbiddenFromNotFound(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	private, err := st.CreateFolder(ctx, ownerID, "Private", nil)
	if err != nil {
		t.Fatalf("create private: %v", err)
	}
	shared, err := st.CreateFolder(ctx, ownerID, "Shared", nil)
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, shared.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}

	// UpdateFolder
	if _, err := st.UpdateFolder(ctx, other, private.ID, "X", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update private foreign: want ErrNotFound, got %v", err)
	}
	if _, err := st.UpdateFolder(ctx, other, shared.ID, "X", nil); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("update shared foreign: want ErrForbidden, got %v", err)
	}

	// ReorderFolder
	if err := st.ReorderFolder(ctx, other, private.ID, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("reorder private foreign: want ErrNotFound, got %v", err)
	}
	if err := st.ReorderFolder(ctx, other, shared.ID, nil); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("reorder shared foreign: want ErrForbidden, got %v", err)
	}

	// DeleteFolder (trash)
	if err := st.DeleteFolder(ctx, other, private.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("trash private foreign: want ErrNotFound, got %v", err)
	}
	if err := st.DeleteFolder(ctx, other, shared.ID); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("trash shared foreign: want ErrForbidden, got %v", err)
	}

	// Owner can still proceed on the shared one.
	if err := st.DeleteFolder(ctx, ownerID, shared.ID); err != nil {
		t.Fatalf("owner trash shared: %v", err)
	}
}

func TestValidateParentRejectsCrossOwnerEvenWhenShared(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	theirShared, err := st.CreateFolder(ctx, other, "TheirShared", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, theirShared.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}

	// A shared parent is still not usable cross-owner: same-owner nesting only.
	if _, err := st.CreateFolder(ctx, ownerID, "Child", &theirShared.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("create under foreign shared parent: want ErrInvalidParent, got %v", err)
	}

	mine, err := st.CreateFolder(ctx, ownerID, "Mine", nil)
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if _, err := st.UpdateFolder(ctx, ownerID, mine.ID, "Mine", &theirShared.ID); !errors.Is(err, store.ErrInvalidParent) {
		t.Errorf("reparent under foreign shared parent: want ErrInvalidParent, got %v", err)
	}
}

func TestAddNoteFolderFilingMatrix(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	theirShared, err := st.CreateFolder(ctx, other, "TheirShared", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, other, theirShared.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}
	theirPrivate, err := st.CreateFolder(ctx, other, "TheirPrivate", nil)
	if err != nil {
		t.Fatalf("create private: %v", err)
	}

	myNote, err := st.CreateNote(ctx, ownerID, "Mine")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// Own note into someone else's shared folder: succeeds.
	if err := st.AddNoteFolder(ctx, ownerID, myNote.ID, theirShared.ID); err != nil {
		t.Fatalf("file own note into shared folder: %v", err)
	}

	// Own note into someone else's private folder: invisible -> ErrNotFound.
	if err := st.AddNoteFolder(ctx, ownerID, myNote.ID, theirPrivate.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("file into private foreign folder: want ErrNotFound, got %v", err)
	}

	// Someone else's note into my folder: I don't own the note -> visible note
	// (it's now readable via theirShared membership) but forbidden to add.
	mine, err := st.CreateFolder(ctx, ownerID, "Mine", nil)
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, mine.ID, model.FolderShared); err != nil {
		t.Fatalf("share mine: %v", err)
	}
	theirNote, err := st.CreateNote(ctx, other, "Theirs")
	if err != nil {
		t.Fatalf("create their note: %v", err)
	}
	// their note is not yet visible to ownerID (not filed anywhere shared) -> ErrNotFound.
	if err := st.AddNoteFolder(ctx, ownerID, theirNote.ID, mine.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("add invisible foreign note: want ErrNotFound, got %v", err)
	}
	// File their note into their own shared folder, making it readable to ownerID...
	if err := st.AddNoteFolder(ctx, other, theirNote.ID, theirShared.ID); err != nil {
		t.Fatalf("owner files own note: %v", err)
	}
	// ...now it's visible to ownerID but still not theirs to file -> ErrForbidden.
	if err := st.AddNoteFolder(ctx, ownerID, theirNote.ID, mine.ID); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("add now-visible foreign note: want ErrForbidden, got %v", err)
	}
}

func TestRemoveNoteFolderFilingMatrix(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	folder, err := st.CreateFolder(ctx, ownerID, "Shared", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, folder.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}
	contributed, err := st.CreateNote(ctx, other, "Contributed")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, other, contributed.ID, folder.ID); err != nil {
		t.Fatalf("contribute: %v", err)
	}

	// Folder owner removes a teammate's contribution: allowed.
	if err := st.RemoveNoteFolder(ctx, ownerID, contributed.ID, folder.ID); err != nil {
		t.Fatalf("folder owner removes teammate note: %v", err)
	}

	// Re-file, then have the note owner remove their own contribution: allowed.
	if err := st.AddNoteFolder(ctx, other, contributed.ID, folder.ID); err != nil {
		t.Fatalf("re-contribute: %v", err)
	}
	if err := st.RemoveNoteFolder(ctx, other, contributed.ID, folder.ID); err != nil {
		t.Fatalf("note owner removes own note: %v", err)
	}

	// A third party (neither note owner nor folder owner) cannot remove.
	third := addUser(t, st)
	if err := st.AddNoteFolder(ctx, other, contributed.ID, folder.ID); err != nil {
		t.Fatalf("re-contribute 2: %v", err)
	}
	if err := st.RemoveNoteFolder(ctx, third, contributed.ID, folder.ID); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("third party remove: want ErrForbidden, got %v", err)
	}
}

func TestReorderNoteInFolderIsFolderOwnerOnly(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	folder, err := st.CreateFolder(ctx, ownerID, "Shared", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, folder.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}
	mine, err := st.CreateNote(ctx, ownerID, "Mine")
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, mine.ID, folder.ID); err != nil {
		t.Fatalf("file mine: %v", err)
	}
	theirs, err := st.CreateNote(ctx, other, "Theirs")
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}
	if err := st.AddNoteFolder(ctx, other, theirs.ID, folder.ID); err != nil {
		t.Fatalf("contribute: %v", err)
	}

	// Folder owner may reorder a teammate's note alongside their own.
	if err := st.ReorderNoteInFolder(ctx, ownerID, folder.ID, theirs.ID, &mine.ID); err != nil {
		t.Fatalf("folder owner reorders teammate note: %v", err)
	}

	// The contributor (note owner, not folder owner) may not reorder.
	if err := st.ReorderNoteInFolder(ctx, other, folder.ID, mine.ID, nil); !errors.Is(err, store.ErrForbidden) {
		t.Errorf("non-folder-owner reorder: want ErrForbidden, got %v", err)
	}
}

func TestGetReadableNoteSharedAccessAndRevocation(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, "Standup")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	folderA, err := st.CreateFolder(ctx, ownerID, "A", nil)
	if err != nil {
		t.Fatalf("create folder a: %v", err)
	}
	folderB, err := st.CreateFolder(ctx, ownerID, "B", nil)
	if err != nil {
		t.Fatalf("create folder b: %v", err)
	}
	privateFolder, err := st.CreateFolder(ctx, ownerID, "Private", nil)
	if err != nil {
		t.Fatalf("create private folder: %v", err)
	}

	// Not readable before any sharing.
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, privateFolder.ID); err != nil {
		t.Fatalf("file private: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("before sharing: want ErrNotFound, got %v", err)
	}

	if _, err := st.SetFolderVisibility(ctx, ownerID, folderA.ID, model.FolderShared); err != nil {
		t.Fatalf("share a: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, folderB.ID, model.FolderShared); err != nil {
		t.Fatalf("share b: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, folderA.ID); err != nil {
		t.Fatalf("file a: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, folderB.ID); err != nil {
		t.Fatalf("file b: %v", err)
	}

	readable, err := st.GetReadableNote(ctx, other, note.ID)
	if err != nil {
		t.Fatalf("read via shared folder: %v", err)
	}
	if readable.IsOwner == nil || *readable.IsOwner {
		t.Errorf("shared reader IsOwner = %v, want false", readable.IsOwner)
	}
	// folder_ids filtered: private folder membership must not leak to the shared reader.
	for _, id := range readable.FolderIDs {
		if id == privateFolder.ID {
			t.Errorf("private folder id leaked to shared reader: %v", readable.FolderIDs)
		}
	}

	// Two shared memberships: removing one still leaves it readable.
	if err := st.RemoveNoteFolder(ctx, ownerID, note.ID, folderA.ID); err != nil {
		t.Fatalf("remove a: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); err != nil {
		t.Errorf("after removing one of two memberships: want readable, got %v", err)
	}

	// Removing the last shared membership revokes access.
	if err := st.RemoveNoteFolder(ctx, ownerID, note.ID, folderB.ID); err != nil {
		t.Fatalf("remove b: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after removing last membership: want ErrNotFound, got %v", err)
	}

	// Re-file and prove privatizing the folder revokes access.
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, folderA.ID); err != nil {
		t.Fatalf("re-file a: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); err != nil {
		t.Fatalf("re-shared: want readable, got %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, folderA.ID, model.FolderPrivate); err != nil {
		t.Fatalf("privatize a: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after privatizing: want ErrNotFound, got %v", err)
	}

	// Re-share, then prove trashing the folder revokes access.
	if _, err := st.SetFolderVisibility(ctx, ownerID, folderA.ID, model.FolderShared); err != nil {
		t.Fatalf("re-share a: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); err != nil {
		t.Fatalf("re-shared 2: want readable, got %v", err)
	}
	if err := st.DeleteFolder(ctx, ownerID, folderA.ID); err != nil {
		t.Fatalf("trash a: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after trashing folder: want ErrNotFound, got %v", err)
	}
}

func TestGetReadableNoteRevokedByNoteTrash(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, "Standup")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	folder, err := st.CreateFolder(ctx, ownerID, "Shared", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, folder.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, folder.ID); err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); err != nil {
		t.Fatalf("before trash: want readable, got %v", err)
	}
	if err := st.DeleteNote(ctx, ownerID, note.ID); err != nil {
		t.Fatalf("trash note: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, other, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after note trash: want ErrNotFound, got %v", err)
	}
}

func TestListReadableNotesFolderVisibilityAndOwnership(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()

	shared, err := st.CreateFolder(ctx, ownerID, "Shared", nil)
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	if _, err := st.SetFolderVisibility(ctx, ownerID, shared.ID, model.FolderShared); err != nil {
		t.Fatalf("share: %v", err)
	}
	private, err := st.CreateFolder(ctx, ownerID, "Private", nil)
	if err != nil {
		t.Fatalf("create private: %v", err)
	}

	mine, err := st.CreateNote(ctx, ownerID, "Mine")
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, mine.ID, shared.ID); err != nil {
		t.Fatalf("file mine: %v", err)
	}
	contributed, err := st.CreateNote(ctx, other, "Contributed")
	if err != nil {
		t.Fatalf("create contributed: %v", err)
	}
	if err := st.AddNoteFolder(ctx, other, contributed.ID, shared.ID); err != nil {
		t.Fatalf("file contributed: %v", err)
	}

	// A teammate listing the shared folder sees both notes, correctly attributed.
	got, err := st.ListReadableNotes(ctx, other, store.ListNotesFilter{FolderID: shared.ID, FolderIDSet: true})
	if err != nil {
		t.Fatalf("list readable: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 notes, got %d: %+v", len(got), got)
	}
	for _, n := range got {
		if n.IsOwner == nil {
			t.Fatalf("IsOwner not populated: %+v", n)
		}
		want := n.ID == contributed.ID
		if *n.IsOwner != want {
			t.Errorf("note %s IsOwner = %v, want %v", n.ID, *n.IsOwner, want)
		}
	}

	// Listing a private foreign folder is ErrNotFound.
	if _, err := st.ListReadableNotes(ctx, other, store.ListNotesFilter{FolderID: private.ID, FolderIDSet: true}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("list private foreign folder: want ErrNotFound, got %v", err)
	}

	// The folder owner listing their own private folder still works normally.
	if _, err := st.ListReadableNotes(ctx, ownerID, store.ListNotesFilter{FolderID: private.ID, FolderIDSet: true}); err != nil {
		t.Errorf("owner list own private folder: %v", err)
	}
}
