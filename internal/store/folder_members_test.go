package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestUpsertFolderMemberOwnerOnly(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, err := st.CreateUser(ctx, "fm-owner@example.com", "hash")
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "fm-other@example.com", "hash")
	if err != nil {
		t.Fatalf("other: %v", err)
	}
	viewer, err := st.CreateUser(ctx, "fm-viewer@example.com", "hash")
	if err != nil {
		t.Fatalf("viewer: %v", err)
	}
	f, err := st.CreateFolder(ctx, owner.ID, "Shared", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	// A non-owner cannot grant membership on a folder they don't own.
	if _, err := st.UpsertFolderMember(ctx, other.ID, f.ID, viewer.ID, "viewer"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("non-owner upsert: want ErrNotFound, got %v", err)
	}

	fm, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer")
	if err != nil {
		t.Fatalf("owner upsert: %v", err)
	}
	if fm.FolderID != f.ID || fm.UserID != viewer.ID || fm.Role != "viewer" || fm.Email != "fm-viewer@example.com" {
		t.Fatalf("unexpected member: %+v", fm)
	}
}

func TestUpsertFolderMemberValidation(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, err := st.CreateUser(ctx, "fmv-owner@example.com", "hash")
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	f, err := st.CreateFolder(ctx, owner.ID, "F", nil)
	if err != nil {
		t.Fatalf("folder: %v", err)
	}

	var ve store.ValidationError
	// Owner cannot add themselves.
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, owner.ID, "viewer"); !errors.As(err, &ve) {
		t.Fatalf("self-grant: want ValidationError, got %v", err)
	}
	// Invalid role.
	other, err := st.CreateUser(ctx, "fmv-other@example.com", "hash")
	if err != nil {
		t.Fatalf("other: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, other.ID, "editor"); !errors.As(err, &ve) {
		t.Fatalf("invalid role: want ValidationError, got %v", err)
	}
	// Nonexistent target user.
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, "00000000-0000-0000-0000-000000000000", "viewer"); !errors.As(err, &ve) {
		t.Fatalf("unknown user: want ValidationError, got %v", err)
	}
}

func TestUpsertFolderMemberIdempotentPreservesCreatedAt(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "fmi-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "fmi-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)

	first, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("created_at changed on repeat upsert: %v vs %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestDeleteFolderMemberIdempotent(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "fmd-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "fmd-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)

	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.DeleteFolderMember(ctx, owner.ID, f.ID, viewer.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	// Idempotent: deleting again is a no-op success.
	if err := st.DeleteFolderMember(ctx, owner.ID, f.ID, viewer.ID); err != nil {
		t.Fatalf("second delete: %v", err)
	}

	members, err := st.ListFolderMembers(ctx, owner.ID, f.ID, 10, time.Time{}, "")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members after delete, got %+v", members)
	}
}

func TestListFolderMembersOwnerAndMemberVisibility(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "fml-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "fml-viewer@example.com", "hash")
	stranger, _ := st.CreateUser(ctx, "fml-stranger@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Owner can list.
	if _, err := st.ListFolderMembers(ctx, owner.ID, f.ID, 10, time.Time{}, ""); err != nil {
		t.Fatalf("owner list: %v", err)
	}
	// The member themself can list.
	if _, err := st.ListFolderMembers(ctx, viewer.ID, f.ID, 10, time.Time{}, ""); err != nil {
		t.Fatalf("member list: %v", err)
	}
	// An unrelated user cannot.
	if _, err := st.ListFolderMembers(ctx, stranger.ID, f.ID, 10, time.Time{}, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger list: want ErrNotFound, got %v", err)
	}
}

func TestListFolderMembersOrderingAndPagination(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "fmp-owner@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)

	var viewers []string
	for i := 0; i < 4; i++ {
		u, err := st.CreateUser(ctx, "fmp-viewer"+string(rune('a'+i))+"@example.com", "hash")
		if err != nil {
			t.Fatalf("create viewer %d: %v", i, err)
		}
		if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, u.ID, "viewer"); err != nil {
			t.Fatalf("grant %d: %v", i, err)
		}
		viewers = append(viewers, u.ID)
	}

	page1, err := st.ListFolderMembers(ctx, owner.ID, f.ID, 2, time.Time{}, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	last := page1[len(page1)-1]
	page2, err := st.ListFolderMembers(ctx, owner.ID, f.ID, 10, last.CreatedAt, last.UserID)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
	for _, m := range page2 {
		if m.UserID == last.UserID {
			t.Fatalf("page2 repeated cursor row %s", m.UserID)
		}
	}
}

func TestFolderMemberSoftDeleteInvisibleMembershipRetained(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "fms-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "fms-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := st.DeleteFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// The membership row is preserved, but the folder is no longer visible
	// through it: even the (former) owner's own listing must independently
	// require a live folder.
	if _, err := st.ListFolderMembers(ctx, owner.ID, f.ID, 10, time.Time{}, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner list after soft delete: want ErrNotFound, got %v", err)
	}
	if _, err := st.ListFolderMembers(ctx, viewer.ID, f.ID, 10, time.Time{}, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("member list after soft delete: want ErrNotFound, got %v", err)
	}

	var remaining int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM folder_members WHERE folder_id=$1`, f.ID).Scan(&remaining); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected the membership row to survive soft delete, got %d", remaining)
	}
}

func TestFolderMemberHardDeleteCascades(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "fmh-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "fmh-viewer@example.com", "hash")
	f, _ := st.CreateFolder(ctx, owner.ID, "F", nil)
	if _, err := st.UpsertFolderMember(ctx, owner.ID, f.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.DeleteFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := st.PurgeFolder(ctx, owner.ID, f.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var remaining int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM folder_members WHERE folder_id=$1`, f.ID).Scan(&remaining); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected hard delete to cascade membership removal, got %d remaining", remaining)
	}
}
