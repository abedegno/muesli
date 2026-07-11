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
	// Idempotent: a second call doesn't duplicate.
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed 2: %v", err)
	}
	names, err := st.BuiltInTemplateNames(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d templates, want 2: %v", len(names), names)
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
	tm, err := st.CreateTemplate(ctx, owner, "Standup", secs())
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
	if err := st.UpdateTemplate(ctx, owner, tm.ID, "Standup 2", secs()); err != nil {
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
	tm, _ := st.CreateTemplate(ctx, owner, "Mine", secs())
	if err := st.UpdateTemplate(ctx, other, tm.ID, "X", secs()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner update: want ErrNotFound, got %v", err)
	}
	// built-in (owner_id NULL) cannot be updated/deleted by a user
	builtins, _ := st.BuiltInTemplates(ctx)
	if len(builtins) > 0 {
		if err := st.UpdateTemplate(ctx, owner, builtins[0].ID, "Hacked", secs()); !errors.Is(err, store.ErrNotFound) {
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
	if _, err := st.CreateTemplate(ctx, owner, "  ", secs()); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "X", nil); err == nil {
		t.Error("no sections should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "X", []model.TemplateSection{{Heading: "", Instruction: "y"}}); err == nil {
		t.Error("empty heading should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "Dup", secs()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTemplate(ctx, owner, "dup", secs()); !errors.Is(err, store.ErrDuplicate) {
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
	tm, _ := st.CreateTemplate(ctx, owner, "Custom", secs())
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
