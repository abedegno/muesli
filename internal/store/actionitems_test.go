package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.

import (
	"context"
	"errors"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func TestActionItemsStoreReplaceListSetStatusAndOwnerScoping(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	otherID := addUser(t, st)
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, "Planning")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	emptyNote, err := st.CreateNote(ctx, ownerID, "Empty")
	if err != nil {
		t.Fatalf("create empty note: %v", err)
	}
	otherNote, err := st.CreateNote(ctx, otherID, "Other planning")
	if err != nil {
		t.Fatalf("create other note: %v", err)
	}

	if err := st.ReplaceActionItemsForNote(ctx, ownerID, note.ID,
		[]model.ActionItem{
			{Text: "Ship the doc", DueHint: "Friday"},
			{Text: "Book the room", DueHint: "Monday"},
		},
		[]model.Decision{{Text: "Use the weekly cadence"}},
	); err != nil {
		t.Fatalf("replace initial: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(ctx, otherID, otherNote.ID,
		[]model.ActionItem{{Text: "Other owner's item", DueHint: "Soon"}},
		nil,
	); err != nil {
		t.Fatalf("replace other owner: %v", err)
	}

	emptyItems, emptyDecisions, err := st.ListForNote(ctx, ownerID, emptyNote.ID)
	if err != nil {
		t.Fatalf("list empty note: %v", err)
	}
	if len(emptyItems) != 0 || len(emptyDecisions) != 0 {
		t.Fatalf("empty note lists should be empty slices, got items=%v decisions=%v", emptyItems, emptyDecisions)
	}

	items, decisions, err := st.ListForNote(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list for note: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len=%d want 2", len(items))
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions len=%d want 1", len(decisions))
	}
	for _, item := range items {
		if item.OwnerID != ownerID || item.NoteID != note.ID {
			t.Fatalf("unexpected item ownership: %+v", item)
		}
		if item.Status != model.ActionItemOpen {
			t.Fatalf("new item status=%q want open", item.Status)
		}
		if item.OwnerPersonID != nil {
			t.Fatalf("owner_person_id should be nil, got %+v", item.OwnerPersonID)
		}
	}
	if decisions[0].OwnerID != ownerID || decisions[0].NoteID != note.ID || decisions[0].Text != "Use the weekly cadence" {
		t.Fatalf("unexpected decision: %+v", decisions[0])
	}

	openItems, err := st.ListForOwner(ctx, ownerID, model.ActionItemOpen)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(openItems) != 2 {
		t.Fatalf("open items len=%d want 2", len(openItems))
	}
	for _, item := range openItems {
		if item.OwnerID != ownerID {
			t.Fatalf("owner list leaked item: %+v", item)
		}
	}
	ownerAll, err := st.ListForOwner(ctx, ownerID, "")
	if err != nil {
		t.Fatalf("list owner all: %v", err)
	}
	if len(ownerAll) != 2 {
		t.Fatalf("owner all len=%d want 2", len(ownerAll))
	}

	updated, err := st.SetStatus(ctx, ownerID, items[0].ID, model.ActionItemDone)
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if updated.Status != model.ActionItemDone {
		t.Fatalf("updated status=%q want done", updated.Status)
	}
	doneItems, err := st.ListForOwner(ctx, ownerID, model.ActionItemDone)
	if err != nil {
		t.Fatalf("list done: %v", err)
	}
	if len(doneItems) != 1 || doneItems[0].ID != items[0].ID {
		t.Fatalf("done items=%+v want one item %s", doneItems, items[0].ID)
	}

	if _, err := st.SetStatus(ctx, otherID, items[0].ID, model.ActionItemOpen); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner patch = %v want ErrNotFound", err)
	}
	if _, _, err := st.ListForNote(ctx, otherID, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner list for note = %v want ErrNotFound", err)
	}

	if err := st.ReplaceActionItemsForNote(ctx, ownerID, note.ID,
		[]model.ActionItem{{Text: "Finalize slides", DueHint: "Tuesday"}},
		[]model.Decision{{Text: "Trim scope"}},
	); err != nil {
		t.Fatalf("replace updated: %v", err)
	}
	items, decisions, err = st.ListForNote(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list after replace: %v", err)
	}
	if len(items) != 1 || items[0].Text != "Finalize slides" || items[0].Status != model.ActionItemOpen {
		t.Fatalf("items after replace=%+v", items)
	}
	if len(decisions) != 1 || decisions[0].Text != "Trim scope" {
		t.Fatalf("decisions after replace=%+v", decisions)
	}
	if ownerAll, err = st.ListForOwner(ctx, ownerID, ""); err != nil {
		t.Fatalf("list owner all after replace: %v", err)
	} else if len(ownerAll) != 1 || ownerAll[0].Text != "Finalize slides" {
		t.Fatalf("owner all after replace=%+v", ownerAll)
	}
	if otherItems, err := st.ListForOwner(ctx, otherID, ""); err != nil {
		t.Fatalf("list other owner: %v", err)
	} else if len(otherItems) != 1 || otherItems[0].Text != "Other owner's item" {
		t.Fatalf("other owner items=%+v", otherItems)
	}
}

func TestActionItemsStoreUpdateAndAssignOwner(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	otherID := addUser(t, st)
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, "Planning")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	otherNote, err := st.CreateNote(ctx, otherID, "Other planning")
	if err != nil {
		t.Fatalf("create other note: %v", err)
	}

	ownerPerson, err := st.UpsertPerson(ctx, ownerID, "assignee@example.com", "Assignee", nil)
	if err != nil {
		t.Fatalf("create owner person: %v", err)
	}
	foreignPerson, err := st.UpsertPerson(ctx, otherID, "foreign@example.com", "Foreign", nil)
	if err != nil {
		t.Fatalf("create foreign person: %v", err)
	}

	if err := st.ReplaceActionItemsForNote(ctx, ownerID, note.ID,
		[]model.ActionItem{{Text: "Ship the doc", DueHint: "Friday"}},
		nil,
	); err != nil {
		t.Fatalf("seed action item: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(ctx, otherID, otherNote.ID,
		[]model.ActionItem{{Text: "Other owner's item", DueHint: "Soon"}},
		nil,
	); err != nil {
		t.Fatalf("seed other action item: %v", err)
	}
	items, _, err := st.ListForNote(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list seeded item: %v", err)
	}
	item := items[0]

	updatedText := "Ship the updated doc"
	updated, err := st.UpdateActionItem(ctx, ownerID, item.ID, &updatedText, nil)
	if err != nil {
		t.Fatalf("update text: %v", err)
	}
	if updated.Text != updatedText || updated.Status != model.ActionItemOpen || updated.OwnerPersonID != nil {
		t.Fatalf("unexpected text-only update: %+v", updated)
	}

	updatedStatus := model.ActionItemDone
	updated, err = st.UpdateActionItem(ctx, ownerID, item.ID, nil, &updatedStatus)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Text != updatedText || updated.Status != model.ActionItemDone || updated.OwnerPersonID != nil {
		t.Fatalf("unexpected status update: %+v", updated)
	}

	assigned, err := st.AssignOwner(ctx, ownerID, item.ID, &ownerPerson.ID)
	if err != nil {
		t.Fatalf("assign owner: %v", err)
	}
	if assigned.OwnerPersonID == nil || *assigned.OwnerPersonID != ownerPerson.ID {
		t.Fatalf("unexpected assigned owner: %+v", assigned)
	}
	if assigned.Text != updatedText || assigned.Status != model.ActionItemDone {
		t.Fatalf("assign should preserve text/status: %+v", assigned)
	}

	cleared, err := st.AssignOwner(ctx, ownerID, item.ID, nil)
	if err != nil {
		t.Fatalf("clear owner: %v", err)
	}
	if cleared.OwnerPersonID != nil {
		t.Fatalf("clear should nil owner: %+v", cleared)
	}

	if _, err := st.AssignOwner(ctx, ownerID, item.ID, &foreignPerson.ID); !errors.Is(err, store.ErrInvalidOwner) {
		t.Fatalf("foreign owner = %v want ErrInvalidOwner", err)
	}
	afterForeign, _, err := st.ListForNote(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("relist after foreign owner: %v", err)
	}
	if afterForeign[0].OwnerPersonID != nil {
		t.Fatalf("foreign owner attempt should not change item: %+v", afterForeign[0])
	}

	otherItems, _, err := st.ListForNote(ctx, otherID, otherNote.ID)
	if err != nil {
		t.Fatalf("list other note: %v", err)
	}
	otherItemID := otherItems[0].ID
	if _, err := st.UpdateActionItem(ctx, ownerID, otherItemID, &updatedText, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner update = %v want ErrNotFound", err)
	}
	if _, err := st.AssignOwner(ctx, ownerID, otherItemID, &ownerPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner assign = %v want ErrNotFound", err)
	}
	if _, err := st.AssignOwner(ctx, ownerID, otherItemID, &foreignPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner foreign assign = %v want ErrNotFound", err)
	}
}
