package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
)

const okRule = `{"op":"and","children":[{"field":"tag","operator":"is","value":"1on1"}]}`

func TestSmartListCRUD(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	sl, err := st.CreateSmartList(ctx, ownerID, "Standups", json.RawMessage(okRule))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sl.ID == "" || sl.Name != "Standups" {
		t.Fatalf("bad list %+v", sl)
	}
	lists, err := st.ListSmartLists(ctx, ownerID)
	if err != nil || len(lists) != 1 {
		t.Fatalf("list: %v len=%d", err, len(lists))
	}
	if err := st.UpdateSmartList(ctx, ownerID, sl.ID, "Renamed", json.RawMessage(okRule)); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := st.DeleteSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	lists, _ = st.ListSmartLists(ctx, ownerID)
	if len(lists) != 0 {
		t.Errorf("after delete len=%d", len(lists))
	}
}

func TestSmartListOwnerScoping(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	sl, _ := st.CreateSmartList(ctx, ownerID, "Mine", json.RawMessage(okRule))
	if err := st.UpdateSmartList(ctx, other, sl.ID, "X", json.RawMessage(okRule)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner update = %v want ErrNotFound", err)
	}
	if err := st.DeleteSmartList(ctx, other, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner delete = %v want ErrNotFound", err)
	}
}

func TestSmartListRuleValidation(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"not a group":      `{"field":"tag","operator":"is","value":"x"}`,
		"bad op":           `{"op":"xor","children":[]}`,
		"bad field":        `{"op":"and","children":[{"field":"speaker","operator":"is","value":"x"}]}`,
		"bad operator":     `{"op":"and","children":[{"field":"tag","operator":"contains","value":"x"}]}`,
		"bad status value": `{"op":"and","children":[{"field":"status","operator":"is","value":"nope"}]}`,
		"bad days value":   `{"op":"and","children":[{"field":"created","operator":"withinLastDays","value":"x"}]}`,
		"negative days":    `{"op":"and","children":[{"field":"created","operator":"withinLastDays","value":-1}]}`,
	}
	for name, rule := range bad {
		if err := store.ValidateRule(json.RawMessage(rule)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	good := `{"op":"or","children":[{"op":"and","children":[{"field":"title","operator":"contains","value":"standup"},{"field":"created","operator":"withinLastDays","value":30}]},{"field":"status","operator":"isNot","value":"failed"}]}`
	if err := store.ValidateRule(json.RawMessage(good)); err != nil {
		t.Errorf("good rule rejected: %v", err)
	}
}

func TestSmartListSoftDelete(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	sl, err := st.CreateSmartList(ctx, ownerID, "Trashme", json.RawMessage(okRule))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.DeleteSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Hidden from the live list.
	if lists, _ := st.ListSmartLists(ctx, ownerID); len(lists) != 0 {
		t.Errorf("trashed list still in ListSmartLists: len=%d", len(lists))
	}
	// Present in trash.
	trash, err := st.ListTrashedSmartLists(ctx, ownerID)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 1 || trash[0].ID != sl.ID {
		t.Fatalf("trash = %+v, want [%s]", trash, sl.ID)
	}
	// Second delete → ErrNotFound (already trashed).
	if err := st.DeleteSmartList(ctx, ownerID, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
	// A trashed list can't be updated.
	if err := st.UpdateSmartList(ctx, ownerID, sl.ID, "Nope", json.RawMessage(okRule)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update trashed = %v, want ErrNotFound", err)
	}
}

func TestSmartListRestore(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	sl, _ := st.CreateSmartList(ctx, ownerID, "Restoreme", json.RawMessage(okRule))
	if err := st.DeleteSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.RestoreSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	lists, _ := st.ListSmartLists(ctx, ownerID)
	if len(lists) != 1 || lists[0].ID != sl.ID {
		t.Fatalf("after restore = %+v, want [%s]", lists, sl.ID)
	}
	if trash, _ := st.ListTrashedSmartLists(ctx, ownerID); len(trash) != 0 {
		t.Errorf("restored list still in trash: len=%d", len(trash))
	}
	// Restoring a live list → ErrNotFound.
	if err := st.RestoreSmartList(ctx, ownerID, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("restore live = %v, want ErrNotFound", err)
	}
}

func TestSmartListPurge(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	sl, _ := st.CreateSmartList(ctx, ownerID, "Purgeme", json.RawMessage(okRule))
	// Can't purge a live (non-trashed) list.
	if err := st.PurgeSmartList(ctx, ownerID, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("purge live = %v, want ErrNotFound", err)
	}
	if err := st.DeleteSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.PurgeSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	// Gone from trash; second purge → ErrNotFound.
	if trash, _ := st.ListTrashedSmartLists(ctx, ownerID); len(trash) != 0 {
		t.Errorf("purged list still in trash: len=%d", len(trash))
	}
	if err := st.PurgeSmartList(ctx, ownerID, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second purge = %v, want ErrNotFound", err)
	}
}

func TestSmartListRecycleBinOwnerScoping(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	sl, _ := st.CreateSmartList(ctx, ownerID, "Mine", json.RawMessage(okRule))
	if err := st.DeleteSmartList(ctx, ownerID, sl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.RestoreSmartList(ctx, other, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner restore = %v, want ErrNotFound", err)
	}
	if err := st.PurgeSmartList(ctx, other, sl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner purge = %v, want ErrNotFound", err)
	}
}

func TestPurgeExpiredSmartLists(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	oldSL, _ := st.CreateSmartList(ctx, ownerID, "Old", json.RawMessage(okRule))
	newSL, _ := st.CreateSmartList(ctx, ownerID, "New", json.RawMessage(okRule))
	if err := st.DeleteSmartList(ctx, ownerID, oldSL.ID); err != nil {
		t.Fatalf("delete old: %v", err)
	}
	if err := st.DeleteSmartList(ctx, ownerID, newSL.ID); err != nil {
		t.Fatalf("delete new: %v", err)
	}
	// Backdate the old one beyond the retention window.
	if _, err := pool.Exec(ctx,
		`UPDATE smart_lists SET deleted_at = now() - interval '31 days' WHERE id=$1`, oldSL.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, err := st.PurgeExpiredSmartLists(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("purge expired: %v", err)
	}
	if n != 1 {
		t.Errorf("purged count: want 1, got %d", n)
	}
	// Old gone, new remains in trash.
	if err := st.PurgeSmartList(ctx, ownerID, oldSL.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("old should be purged: %v", err)
	}
	if trash, _ := st.ListTrashedSmartLists(ctx, ownerID); len(trash) != 1 || trash[0].ID != newSL.ID {
		t.Errorf("new should remain in trash, got %+v", trash)
	}
}

func TestValidateRuleFolderField(t *testing.T) {
	// folder/is and folder/isNot are valid
	validRules := map[string]string{
		"folder is":    `{"op":"and","children":[{"field":"folder","operator":"is","value":"folder-123"}]}`,
		"folder isNot": `{"op":"and","children":[{"field":"folder","operator":"isNot","value":"folder-456"}]}`,
	}
	for name, rule := range validRules {
		if err := store.ValidateRule(json.RawMessage(rule)); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}

	// folder/contains is invalid (not a registered operator for folder)
	invalidOp := `{"op":"and","children":[{"field":"folder","operator":"contains","value":"folder-123"}]}`
	if err := store.ValidateRule(json.RawMessage(invalidOp)); err == nil {
		t.Error("folder/contains: expected validation error, got nil")
	}

	// folder with empty string value is invalid
	emptyVal := `{"op":"and","children":[{"field":"folder","operator":"is","value":""}]}`
	if err := store.ValidateRule(json.RawMessage(emptyVal)); err == nil {
		t.Error("folder empty value: expected validation error, got nil")
	}
}
