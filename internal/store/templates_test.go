package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func TestSeedBuiltInTemplates(t *testing.T) {
	t.Parallel()
	st, _, _ := newStoreWithOwner(t)
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	namesBefore, err := st.BuiltInTemplateNames(ctx)
	if err != nil {
		t.Fatalf("list before reseed: %v", err)
	}
	if len(namesBefore) == 0 {
		t.Fatal("expected at least one built-in template after first seed")
	}
	// Idempotent: a second call leaves the built-in template set unchanged.
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed 2: %v", err)
	}
	namesAfter, err := st.BuiltInTemplateNames(ctx)
	if err != nil {
		t.Fatalf("list after reseed: %v", err)
	}
	if len(namesBefore) != len(namesAfter) {
		t.Fatalf("built-in template count changed after reseed: before=%d after=%d namesBefore=%v namesAfter=%v", len(namesBefore), len(namesAfter), namesBefore, namesAfter)
	}
	for i := range namesBefore {
		if namesBefore[i] != namesAfter[i] {
			t.Fatalf("built-in template names changed after reseed: before=%v after=%v", namesBefore, namesAfter)
		}
	}
}

func secs() []model.TemplateSection {
	return []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise it."}}
}

func TestTemplateCRUD(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	tm, err := st.CreateTemplate(ctx, owner, "Standup", "after", secs())
	if err != nil || tm.ID == "" || tm.BuiltIn {
		t.Fatalf("create: %v %+v", err, tm)
	}
	list, err := st.ListTemplates(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	var sawBuiltIn, sawMine bool
	for _, x := range list {
		if x.BuiltIn {
			sawBuiltIn = true
		}
		if x.ID == tm.ID && !x.BuiltIn {
			sawMine = true
		}
	}
	if !sawBuiltIn || !sawMine {
		t.Fatalf("list missing built-in or mine: %+v", list)
	}
	if err := st.UpdateTemplate(ctx, owner, tm.ID, "Standup 2", "after", secs()); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := st.DeleteTemplate(ctx, owner, tm.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestTemplateOwnerScopingAndBuiltInReadOnly(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	_ = st.SeedBuiltInTemplates(ctx)
	tm, _ := st.CreateTemplate(ctx, owner, "Mine", "after", secs())
	if err := st.UpdateTemplate(ctx, other, tm.ID, "X", "after", secs()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner update: want ErrNotFound, got %v", err)
	}
	// built-in (owner_id NULL) cannot be updated/deleted by a user
	builtins, _ := st.BuiltInTemplates(ctx)
	if len(builtins) > 0 {
		if err := st.UpdateTemplate(ctx, owner, builtins[0].ID, "Hacked", "after", secs()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("built-in update: want ErrNotFound, got %v", err)
		}
		if err := st.DeleteTemplate(ctx, owner, builtins[0].ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("built-in delete: want ErrNotFound, got %v", err)
		}
	}
}

func TestTemplateValidationAndDuplicate(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	if _, err := st.CreateTemplate(ctx, owner, "  ", "after", secs()); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "X", "after", nil); err == nil {
		t.Error("no sections should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "X", "after", []model.TemplateSection{{Heading: "", Instruction: "y"}}); err == nil {
		t.Error("empty heading should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "Dup", "after", secs()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTemplate(ctx, owner, "dup", "after", secs()); !errors.Is(err, store.ErrDuplicate) {
		t.Errorf("dup name: want ErrDuplicate, got %v", err)
	}
	_ = strings.TrimSpace
}

func TestNoteOwnerIDAndTemplatesForSummary(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()
	_ = st.SeedBuiltInTemplates(ctx)
	note, _ := st.CreateNote(ctx, owner, "N")
	got, err := st.NoteOwnerID(ctx, note.ID)
	if err != nil || got != owner {
		t.Fatalf("NoteOwnerID: %v %q want %q", err, got, owner)
	}
	tm, _ := st.CreateTemplate(ctx, owner, "Custom", "after", secs())
	forSum, err := st.TemplatesForSummary(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	var sawCustom bool
	for _, x := range forSum {
		if x.ID == tm.ID {
			sawCustom = true
		}
	}
	if !sawCustom || len(forSum) < 2 {
		t.Fatalf("TemplatesForSummary missing custom or built-ins: %+v", forSum)
	}
	if _, err := st.NoteOwnerID(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("NoteOwnerID missing: want ErrNotFound, got %v", err)
	}
}
