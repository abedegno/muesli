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
	tm, err := st.CreateTemplate(ctx, owner, "Standup", "after", secs(), true, "", "", nil)
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
	if err := st.UpdateTemplate(ctx, owner, tm.ID, "Standup 2", "after", secs(), true, "", "", nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := st.DeleteTemplate(ctx, owner, tm.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// TestTemplateAgentOverridesRoundTrip covers the optional per-template agent
// overrides (system_prompt, model, temperature) round-tripping through
// create, update, get, and list. nil/empty means unset.
func TestTemplateAgentOverridesRoundTrip(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	temp := 0.7
	tm, err := st.CreateTemplate(ctx, owner, "Overrides", "after", secs(), true,
		"You are a terse summarizer.", "llama3.2:3b", &temp)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tm.SystemPrompt != "You are a terse summarizer." || tm.Model != "llama3.2:3b" || tm.Temperature == nil || *tm.Temperature != 0.7 {
		t.Fatalf("create did not round-trip overrides: %+v", tm)
	}

	got, err := st.GetTemplate(ctx, owner, tm.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SystemPrompt != "You are a terse summarizer." || got.Model != "llama3.2:3b" || got.Temperature == nil || *got.Temperature != 0.7 {
		t.Fatalf("get did not round-trip overrides: %+v", got)
	}

	list, err := st.ListTemplates(ctx, owner)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var sawInList bool
	for _, x := range list {
		if x.ID == tm.ID {
			sawInList = true
			if x.SystemPrompt != "You are a terse summarizer." || x.Model != "llama3.2:3b" || x.Temperature == nil || *x.Temperature != 0.7 {
				t.Fatalf("list did not round-trip overrides: %+v", x)
			}
		}
	}
	if !sawInList {
		t.Fatalf("list missing created template: %+v", list)
	}

	// Update to a different override set.
	newTemp := 1.2
	if err := st.UpdateTemplate(ctx, owner, tm.ID, "Overrides 2", "after", secs(), true,
		"Be verbose.", "llama3.2:70b", &newTemp); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = st.GetTemplate(ctx, owner, tm.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.SystemPrompt != "Be verbose." || got.Model != "llama3.2:70b" || got.Temperature == nil || *got.Temperature != 1.2 {
		t.Fatalf("update did not round-trip new overrides: %+v", got)
	}

	// Update to clear overrides (empty/nil = unset).
	if err := st.UpdateTemplate(ctx, owner, tm.ID, "Overrides 3", "after", secs(), true, "", "", nil); err != nil {
		t.Fatalf("update clear: %v", err)
	}
	got, err = st.GetTemplate(ctx, owner, tm.ID)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got.SystemPrompt != "" || got.Model != "" || got.Temperature != nil {
		t.Fatalf("update did not clear overrides: %+v", got)
	}

	// Out-of-range temperature is rejected.
	badTemp := 5.0
	if _, err := st.CreateTemplate(ctx, owner, "BadTemp", "after", secs(), true, "", "", &badTemp); err == nil {
		t.Error("out-of-range temperature should fail validation")
	}

	// GetTemplate for a template belonging to another owner returns ErrNotFound.
	other := addUser(t, st)
	if _, err := st.GetTemplate(ctx, other, tm.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner get: want ErrNotFound, got %v", err)
	}
}

func TestTemplateOwnerScopingAndBuiltInReadOnly(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	other := addUser(t, st)
	ctx := context.Background()
	_ = st.SeedBuiltInTemplates(ctx)
	tm, _ := st.CreateTemplate(ctx, owner, "Mine", "after", secs(), true, "", "", nil)
	if err := st.UpdateTemplate(ctx, other, tm.ID, "X", "after", secs(), true, "", "", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner update: want ErrNotFound, got %v", err)
	}
	// built-in (owner_id NULL) cannot be updated/deleted by a user
	builtins, _ := st.BuiltInTemplates(ctx)
	if len(builtins) > 0 {
		if err := st.UpdateTemplate(ctx, owner, builtins[0].ID, "Hacked", "after", secs(), true, "", "", nil); !errors.Is(err, store.ErrNotFound) {
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
	if _, err := st.CreateTemplate(ctx, owner, "  ", "after", secs(), true, "", "", nil); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "X", "after", nil, true, "", "", nil); err == nil {
		t.Error("no sections should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "X", "after", []model.TemplateSection{{Heading: "", Instruction: "y"}}, true, "", "", nil); err == nil {
		t.Error("empty heading should fail")
	}
	if _, err := st.CreateTemplate(ctx, owner, "Dup", "after", secs(), true, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTemplate(ctx, owner, "dup", "after", secs(), true, "", "", nil); !errors.Is(err, store.ErrDuplicate) {
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
	tm, _ := st.CreateTemplate(ctx, owner, "Custom", "after", secs(), true, "", "", nil)
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

// TestTemplatesForSummaryExcludesPreTemplates proves the after-summary
// fan-out is explicitly phase-scoped: an auto-run "pre" template must never
// be selected for transcript-driven summarization, closing the gap where
// TemplatesForSummary used to select every auto-run template regardless of
// phase (issue #763's TemplatesForPhase).
func TestTemplatesForSummaryExcludesPreTemplates(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	after, err := st.CreateTemplate(ctx, owner, "After template", "after", secs(), true, "", "", nil)
	if err != nil {
		t.Fatalf("create after template: %v", err)
	}
	pre, err := st.CreateTemplate(ctx, owner, "Pre template", "pre", secs(), true, "", "", nil)
	if err != nil {
		t.Fatalf("create pre template: %v", err)
	}

	forSummary, err := st.TemplatesForSummary(ctx, owner)
	if err != nil {
		t.Fatalf("TemplatesForSummary: %v", err)
	}
	var sawAfter, sawPre bool
	for _, tmpl := range forSummary {
		if tmpl.ID == after.ID {
			sawAfter = true
		}
		if tmpl.ID == pre.ID {
			sawPre = true
		}
	}
	if !sawAfter {
		t.Fatalf("TemplatesForSummary missing auto-run after template: %+v", forSummary)
	}
	if sawPre {
		t.Fatalf("TemplatesForSummary must exclude pre templates even when auto-run: %+v", forSummary)
	}

	forPre, err := st.TemplatesForPhase(ctx, owner, "pre", true)
	if err != nil {
		t.Fatalf("TemplatesForPhase(pre): %v", err)
	}
	var sawPreInPrePhase, sawAfterInPrePhase bool
	for _, tmpl := range forPre {
		if tmpl.ID == pre.ID {
			sawPreInPrePhase = true
		}
		if tmpl.ID == after.ID {
			sawAfterInPrePhase = true
		}
	}
	if !sawPreInPrePhase {
		t.Fatalf("TemplatesForPhase(pre) missing the pre template: %+v", forPre)
	}
	if sawAfterInPrePhase {
		t.Fatalf("TemplatesForPhase(pre) must exclude after templates: %+v", forPre)
	}
}

// TestCreateTemplateRejectsCrossAutoRun exercises the accepted spec for
// issue #765: cross-phase templates are manual only. First a deliberately
// invalid fixture (auto_run=false, so it should succeed) is created to prove
// the phase itself is accepted, then the guard is exercised failing on
// auto_run=true before being restored to a non-auto-run cross template.
func TestCreateTemplateRejectsCrossAutoRun(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	// Phase cross is accepted on its own (manual run).
	manual, err := st.CreateTemplate(ctx, owner, "Cross manual", "cross", secs(), false, "", "", nil)
	if err != nil {
		t.Fatalf("create manual cross template: %v", err)
	}
	if manual.Phase != "cross" || manual.AutoRun {
		t.Fatalf("unexpected manual cross template: %+v", manual)
	}

	// cross + auto_run=true is rejected.
	if _, err := st.CreateTemplate(ctx, owner, "Cross auto", "cross", secs(), true, "", "", nil); err == nil {
		t.Fatal("expected CreateTemplate to reject cross+auto_run, got nil error")
	} else if !errors.As(err, new(store.ValidationError)) {
		t.Fatalf("expected a ValidationError, got %v (%T)", err, err)
	}
}

// TestUpdateTemplateRejectsCrossAutoRun mirrors TestCreateTemplateRejectsCrossAutoRun
// for UpdateTemplate: an existing after-phase template cannot be edited into
// cross+auto_run.
func TestUpdateTemplateRejectsCrossAutoRun(t *testing.T) {
	t.Parallel()
	st, owner, _ := newStoreWithOwner(t)
	ctx := context.Background()

	tmpl, err := st.CreateTemplate(ctx, owner, "Will become cross", "after", secs(), true, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.UpdateTemplate(ctx, owner, tmpl.ID, tmpl.Name, "cross", secs(), true, "", "", nil); err == nil {
		t.Fatal("expected UpdateTemplate to reject cross+auto_run, got nil error")
	} else if !errors.As(err, new(store.ValidationError)) {
		t.Fatalf("expected a ValidationError, got %v (%T)", err, err)
	}

	// Restoring to cross+manual succeeds.
	if err := st.UpdateTemplate(ctx, owner, tmpl.ID, tmpl.Name, "cross", secs(), false, "", "", nil); err != nil {
		t.Fatalf("update to manual cross: %v", err)
	}
}

// TestTemplatesForSummaryExcludesCrossAutoRunLegacyRow proves the read-side
// guard (Store.TemplatesForSummary's phase filter) protects a row that
// predates the write-side validation added above -- inserted directly via
// raw SQL, bypassing CreateTemplate/UpdateTemplate entirely, exactly as a
// legacy row from before this guard existed would look.
func TestTemplatesForSummaryExcludesCrossAutoRunLegacyRow(t *testing.T) {
	t.Parallel()
	st, owner, pool := newStoreWithOwner(t)
	ctx := context.Background()

	var legacyID string
	err := pool.QueryRow(ctx,
		`INSERT INTO templates (id, owner_id, name, phase, sections, auto_run)
		 VALUES (gen_random_uuid(), $1, 'Legacy cross auto-run', 'cross', '[{"heading":"H","instruction":"I"}]'::jsonb, true)
		 RETURNING id`, owner).Scan(&legacyID)
	if err != nil {
		t.Fatalf("insert legacy cross auto-run row: %v", err)
	}

	forSummary, err := st.TemplatesForSummary(ctx, owner)
	if err != nil {
		t.Fatalf("TemplatesForSummary: %v", err)
	}
	for _, tmpl := range forSummary {
		if tmpl.ID == legacyID {
			t.Fatalf("TemplatesForSummary must exclude a legacy cross+auto_run row, got it in %+v", forSummary)
		}
	}
}
