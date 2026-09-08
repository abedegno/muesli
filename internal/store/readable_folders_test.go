package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestGetReadableFolderOwnerAndMember(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "grf-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "grf-viewer@example.com", "hash")
	stranger, _ := st.CreateUser(ctx, "grf-stranger@example.com", "hash")
	f, err := st.CreateFolder(ctx, owner.ID, "Shared", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if _, err := st.GetReadableFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("owner read: %v", err)
	}
	if _, err := st.GetReadableFolder(ctx, viewer.ID, f.ID); err != nil {
		t.Fatalf("member read: %v", err)
	}
	if _, err := st.GetReadableFolder(ctx, stranger.ID, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger read: want ErrNotFound, got %v", err)
	}
}

func TestGetReadableFolderExcludesSoftDeleted(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "grfd-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "grfd-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := st.DeleteFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := st.GetReadableFolder(ctx, owner.ID, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner after soft delete: want ErrNotFound, got %v", err)
	}
	if _, err := st.GetReadableFolder(ctx, viewer.ID, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("member after soft delete: want ErrNotFound, got %v", err)
	}
}

func TestListSharedFoldersExcludesOwnershipAndOrders(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "lsf-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "lsf-viewer@example.com", "hash")

	// Owner's own folder must never appear in the viewer's shared list, even
	// though the viewer is granted on it (ownership always wins over an
	// incidental grant, and this store never issues self-grants anyway).
	ownFolder, _ := st.CreateFolder(ctx, viewer.ID, "Mine", nil)
	_ = ownFolder

	var shared []string
	for i := 0; i < 3; i++ {
		f, err := st.CreateFolder(ctx, owner.ID, "Shared"+string(rune('A'+i)), nil)
		if err != nil {
			t.Fatalf("create folder %d: %v", i, err)
		}
		if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
			t.Fatalf("grant %d: %v", i, err)
		}
		shared = append(shared, f.ID)
	}

	got, err := st.ListSharedFolders(ctx, viewer.ID, 10, time.Time{}, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 shared folders, got %d: %+v", len(got), got)
	}
	for _, sf := range got {
		if sf.OwnerID != owner.ID || sf.CountScope != "direct" {
			t.Fatalf("unexpected shared folder: %+v", sf)
		}
		if sf.ID == ownFolder.ID {
			t.Fatal("viewer's own folder must not appear in their shared list")
		}
	}
	// Ordering follows grant creation order (A, B, C created in that order).
	for i, id := range shared {
		if got[i].ID != id {
			t.Fatalf("order[%d] = %s, want %s (grant order)", i, got[i].ID, id)
		}
	}
}

func TestListSharedFoldersDirectCountExcludesDescendants(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "lsfc-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "lsfc-viewer@example.com", "hash")

	parent, _ := st.CreateFolder(ctx, owner.ID, "Parent", nil)
	child, _ := st.CreateFolder(ctx, owner.ID, "Child", &parent.ID)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, parent.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// One note directly in parent, one in child (not directly in parent).
	directNote, err := st.CreateNote(ctx, owner.ID, "Direct")
	if err != nil {
		t.Fatalf("create direct note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, directNote.ID, parent.ID); err != nil {
		t.Fatalf("assign direct: %v", err)
	}
	childNote, err := st.CreateNote(ctx, owner.ID, "Child note")
	if err != nil {
		t.Fatalf("create child note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, childNote.ID, child.ID); err != nil {
		t.Fatalf("assign child: %v", err)
	}
	// A deleted note directly in parent must not inflate the count.
	deletedNote, err := st.CreateNote(ctx, owner.ID, "Deleted")
	if err != nil {
		t.Fatalf("create deleted note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, deletedNote.ID, parent.ID); err != nil {
		t.Fatalf("assign deleted: %v", err)
	}
	if err := st.DeleteNote(ctx, owner.ID, deletedNote.ID); err != nil {
		t.Fatalf("delete note: %v", err)
	}

	got, err := st.ListSharedFolders(ctx, viewer.ID, 10, time.Time{}, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 shared folder, got %d", len(got))
	}
	if got[0].NoteCount != 1 {
		t.Fatalf("note_count = %d, want 1 (direct live only, no descendant, no deleted)", got[0].NoteCount)
	}
}

func TestListSharedFoldersSoftDeleteExcludes(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "lsfsd-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "lsfsd-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := st.DeleteFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	got, err := st.ListSharedFolders(ctx, viewer.ID, 10, time.Time{}, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 shared folders after soft delete, got %+v", got)
	}
}

func TestListSharedFoldersPagination(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "lsfp-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "lsfp-viewer@example.com", "hash")
	for i := 0; i < 4; i++ {
		f, err := st.CreateFolder(ctx, owner.ID, "P"+string(rune('A'+i)), nil)
		if err != nil {
			t.Fatalf("folder %d: %v", i, err)
		}
		if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
			t.Fatalf("grant %d: %v", i, err)
		}
	}
	page1, err := st.ListSharedFolders(ctx, viewer.ID, 2, time.Time{}, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	last := page1[len(page1)-1]
	page2, err := st.ListSharedFolders(ctx, viewer.ID, 10, last.GrantedAt, last.ID)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
}
