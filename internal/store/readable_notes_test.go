package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestGetReadableNoteOwnerAndExactMember(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "grn-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "grn-viewer@example.com", "hash")
	stranger, _ := st.CreateUser(ctx, "grn-stranger@example.com", "hash")

	f, err := st.CreateFolder(ctx, owner.ID, "F", nil)
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "N")
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, note.ID, f.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if _, err := st.GetReadableNote(ctx, owner.ID, note.ID); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, viewer.ID, note.ID); err != nil {
		t.Fatalf("member: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, stranger.ID, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger: want ErrNotFound, got %v", err)
	}

	// An unassigned note is invisible even to a folder viewer.
	lonely, err := st.CreateNote(ctx, owner.ID, "Lonely")
	if err != nil {
		t.Fatalf("lonely note: %v", err)
	}
	if _, err := st.GetReadableNote(ctx, viewer.ID, lonely.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("viewer on unassigned note: want ErrNotFound, got %v", err)
	}
}

// TestReadableFolderIDsIsolatesSharedAFromPrivateB proves that a note filed
// in both a shared folder A and a private folder B projects only A for a
// viewer of A, in both the single-note and list responses, and that B
// cannot be used as a listing filter even though the viewer (correctly)
// learned its id would not work.
func TestReadableFolderIDsIsolatesSharedAFromPrivateB(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "iso-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "iso-viewer@example.com", "hash")

	a, err := st.CreateFolder(ctx, owner.ID, "A", nil)
	if err != nil {
		t.Fatalf("folder A: %v", err)
	}
	b, err := st.CreateFolder(ctx, owner.ID, "B", nil)
	if err != nil {
		t.Fatalf("folder B: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, a.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant A: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "Shared+Private")
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, note.ID, a.ID); err != nil {
		t.Fatalf("assign A: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, note.ID, b.ID); err != nil {
		t.Fatalf("assign B: %v", err)
	}

	// Single-note projection.
	ids, err := st.ReadableNoteFolderIDs(ctx, viewer.ID, note.ID)
	if err != nil {
		t.Fatalf("readable folder ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("projected folder ids = %v, want only [%s]", ids, a.ID)
	}

	// List projection (via ListReadableNotes on A).
	notes, err := st.ListReadableNotes(ctx, viewer.ID, a.ID)
	if err != nil {
		t.Fatalf("list readable notes: %v", err)
	}
	if len(notes) != 1 || len(notes[0].FolderIDs) != 1 || notes[0].FolderIDs[0] != a.ID {
		t.Fatalf("list projection leaked B: %+v", notes)
	}

	// B cannot be used as a filter -- GetReadableFolder denies it outright.
	if _, err := st.GetReadableFolder(ctx, viewer.ID, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("B as filter: want ErrNotFound, got %v", err)
	}
}

// TestReadableFolderNonInheritance shares a parent folder and proves a live
// private child (and its notes) remain invisible to the parent's viewer.
func TestReadableFolderNonInheritance(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "inh-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "inh-viewer@example.com", "hash")

	parent, err := st.CreateFolder(ctx, owner.ID, "Parent", nil)
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	child, err := st.CreateFolder(ctx, owner.ID, "Child", &parent.ID)
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, parent.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant parent: %v", err)
	}
	childNote, err := st.CreateNote(ctx, owner.ID, "Child note")
	if err != nil {
		t.Fatalf("child note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, childNote.ID, child.ID); err != nil {
		t.Fatalf("assign child note: %v", err)
	}

	if _, err := st.GetReadableFolder(ctx, viewer.ID, child.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child folder: want ErrNotFound (no inheritance), got %v", err)
	}
	if _, err := st.GetReadableNote(ctx, viewer.ID, childNote.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child note: want ErrNotFound (no inheritance), got %v", err)
	}
}

func TestListReadableNotesDuplicateAssignmentAndSoftDelete(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "dup-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "dup-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	live, err := st.CreateNote(ctx, owner.ID, "Live")
	if err != nil {
		t.Fatalf("live note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, live.ID, f.ID); err != nil {
		t.Fatalf("assign live: %v", err)
	}
	// Re-adding is idempotent (no duplicate row/entry in results).
	if err := st.AddNoteFolder(ctx, owner.ID, live.ID, f.ID); err != nil {
		t.Fatalf("re-assign live: %v", err)
	}

	deleted, err := st.CreateNote(ctx, owner.ID, "Deleted")
	if err != nil {
		t.Fatalf("deleted note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, deleted.ID, f.ID); err != nil {
		t.Fatalf("assign deleted: %v", err)
	}
	if err := st.DeleteNote(ctx, owner.ID, deleted.ID); err != nil {
		t.Fatalf("soft delete note: %v", err)
	}

	notes, err := st.ListReadableNotes(ctx, viewer.ID, f.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != live.ID {
		t.Fatalf("expected only the live note, got %+v", notes)
	}

	// The soft-deleted note is also invisible directly, though its
	// note_folders row is retained.
	if _, err := st.GetReadableNote(ctx, viewer.ID, deleted.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted note direct read: want ErrNotFound, got %v", err)
	}
}

func TestListReadableNotesSoftDeletedFolder(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "sdf-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "sdf-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "N")
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, note.ID, f.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := st.DeleteFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("soft delete folder: %v", err)
	}

	if _, err := st.GetReadableFolder(ctx, viewer.ID, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("folder: want ErrNotFound, got %v", err)
	}
	ids, err := st.ReadableNoteFolderIDs(ctx, viewer.ID, note.ID)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected the deleted folder to drop out of projection, got %v", ids)
	}
	if _, err := st.GetReadableNote(ctx, viewer.ID, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("note read via deleted folder: want ErrNotFound, got %v", err)
	}
}

// TestReadableSubtreeIndependentGrants grants access to a parent AND one of
// its descendants independently, soft-deletes the parent's whole subtree,
// and requires neither node nor descendant-only notes readable/discoverable.
func TestReadableSubtreeIndependentGrants(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "sub-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "sub-viewer@example.com", "hash")

	parent, err := st.CreateFolder(ctx, owner.ID, "Parent", nil)
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	child, err := st.CreateFolder(ctx, owner.ID, "Child", &parent.ID)
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, parent.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant parent: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, child.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant child: %v", err)
	}
	childNote, err := st.CreateNote(ctx, owner.ID, "Child note")
	if err != nil {
		t.Fatalf("child note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, childNote.ID, child.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// Before deletion, both are visible.
	if _, err := st.GetReadableFolder(ctx, viewer.ID, child.ID); err != nil {
		t.Fatalf("child before delete: %v", err)
	}

	if err := st.DeleteFolder(ctx, owner.ID, parent.ID); err != nil {
		t.Fatalf("soft delete subtree: %v", err)
	}

	if _, err := st.GetReadableFolder(ctx, viewer.ID, parent.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("parent after subtree delete: want ErrNotFound, got %v", err)
	}
	if _, err := st.GetReadableFolder(ctx, viewer.ID, child.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child after subtree delete: want ErrNotFound (independent grant still requires live folder), got %v", err)
	}
	if _, err := st.GetReadableNote(ctx, viewer.ID, childNote.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child note after subtree delete: want ErrNotFound, got %v", err)
	}
	shared, err := st.ListSharedFolders(ctx, viewer.ID, 10, time.Time{}, "")
	if err != nil {
		t.Fatalf("list shared: %v", err)
	}
	if len(shared) != 0 {
		t.Fatalf("expected no shared folders after subtree delete, got %+v", shared)
	}
}

func TestReadableFolderRestorationReactivatesGrant(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "restore-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "restore-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := st.DeleteFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := st.GetReadableFolder(ctx, viewer.ID, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("while deleted: want ErrNotFound, got %v", err)
	}
	if err := st.RestoreFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// The preserved grant becomes effective again without recreating it.
	if _, err := st.GetReadableFolder(ctx, viewer.ID, f.ID); err != nil {
		t.Fatalf("after restore: %v", err)
	}
}
