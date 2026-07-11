package store_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestConversationsCRUD(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "conv-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "conv-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "Sprint planning")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// Global conversation (no note).
	global, err := st.CreateConversation(ctx, owner.ID, nil, "General chat", nil)
	if err != nil {
		t.Fatalf("create global conversation: %v", err)
	}
	if global.NoteID != nil {
		t.Fatalf("expected nil note id, got %+v", global.NoteID)
	}

	// Note-scoped conversation.
	modelOverride := "gpt-test"
	scoped, err := st.CreateConversation(ctx, owner.ID, &note.ID, "About this note", &modelOverride)
	if err != nil {
		t.Fatalf("create scoped conversation: %v", err)
	}
	if scoped.NoteID == nil || *scoped.NoteID != note.ID {
		t.Fatalf("expected note id %q, got %+v", note.ID, scoped.NoteID)
	}
	if scoped.ModelOverride == nil || *scoped.ModelOverride != modelOverride {
		t.Fatalf("expected model override %q, got %+v", modelOverride, scoped.ModelOverride)
	}

	// Creating a conversation against a note the caller doesn't own fails.
	if _, err := st.CreateConversation(ctx, other.ID, &note.ID, "sneaky", nil); err != store.ErrNotFound {
		t.Fatalf("cross-owner note attach: want ErrNotFound got %v", err)
	}

	// GetConversation is owner-scoped.
	got, err := st.GetConversation(ctx, owner.ID, scoped.ID)
	if err != nil || got.ID != scoped.ID {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if _, err := st.GetConversation(ctx, other.ID, scoped.ID); err != store.ErrNotFound {
		t.Fatalf("cross-owner get: want ErrNotFound got %v", err)
	}

	// ListConversations: note-scoped filter returns only that note's conversation.
	byNote, err := st.ListConversations(ctx, owner.ID, &note.ID)
	if err != nil {
		t.Fatalf("list by note: %v", err)
	}
	if len(byNote) != 1 || byNote[0].ID != scoped.ID {
		t.Fatalf("list by note: unexpected result %+v", byNote)
	}

	// ListConversations: nil filter returns all owned conversations (global + scoped).
	all, err := st.ListConversations(ctx, owner.ID, nil)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("list all: want 2 got %d (%+v)", len(all), all)
	}

	// Owner isolation: other user sees none of owner's conversations.
	otherList, err := st.ListConversations(ctx, other.ID, nil)
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(otherList) != 0 {
		t.Fatalf("expected no conversations for other owner, got %+v", otherList)
	}

	// Append + list messages, chronological order, owner-scoped.
	m1, err := st.AppendMessage(ctx, scoped.ID, "user", "hello", "gpt-test", nil)
	if err != nil {
		t.Fatalf("append user message: %v", err)
	}
	tokens := 42
	m2, err := st.AppendMessage(ctx, scoped.ID, "assistant", "hi there", "gpt-test", &tokens)
	if err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	msgs, err := st.ListMessages(ctx, owner.ID, scoped.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != m1.ID || msgs[1].ID != m2.ID {
		t.Fatalf("unexpected message order: %+v", msgs)
	}
	if msgs[1].TokensUsed == nil || *msgs[1].TokensUsed != tokens {
		t.Fatalf("expected tokens_used %d, got %+v", tokens, msgs[1].TokensUsed)
	}

	// AppendMessage bumps the parent conversation's updated_at.
	afterAppend, err := st.GetConversation(ctx, owner.ID, scoped.ID)
	if err != nil {
		t.Fatalf("get after append: %v", err)
	}
	if !afterAppend.UpdatedAt.After(scoped.UpdatedAt) && !afterAppend.UpdatedAt.Equal(scoped.UpdatedAt) {
		t.Fatalf("expected updated_at to advance: before=%v after=%v", scoped.UpdatedAt, afterAppend.UpdatedAt)
	}

	// Messages are owner-scoped: another user can't list them.
	if _, err := st.ListMessages(ctx, other.ID, scoped.ID); err != store.ErrNotFound {
		t.Fatalf("cross-owner list messages: want ErrNotFound got %v", err)
	}

	// Delete + cascade: messages disappear along with the conversation.
	if err := st.DeleteConversation(ctx, owner.ID, scoped.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetConversation(ctx, owner.ID, scoped.ID); err != store.ErrNotFound {
		t.Fatalf("get after delete: want ErrNotFound got %v", err)
	}
	if _, err := st.ListMessages(ctx, owner.ID, scoped.ID); err != store.ErrNotFound {
		t.Fatalf("list messages after delete: want ErrNotFound got %v", err)
	}

	// Deleting someone else's (or a nonexistent) conversation is ErrNotFound.
	if err := st.DeleteConversation(ctx, other.ID, global.ID); err != store.ErrNotFound {
		t.Fatalf("cross-owner delete: want ErrNotFound got %v", err)
	}
}

func TestSetConversationTitleIfEmpty(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "title-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	// Create an empty-title conversation.
	conv, err := st.CreateConversation(ctx, owner.ID, nil, "", nil)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if conv.Title != "" {
		t.Fatalf("expected empty title, got %q", conv.Title)
	}

	// Setting a title when empty should succeed and return true.
	updated, err := st.SetConversationTitleIfEmpty(ctx, conv.ID, "First Title")
	if err != nil {
		t.Fatalf("SetConversationTitleIfEmpty: %v", err)
	}
	if !updated {
		t.Fatalf("expected updated=true when title was empty, got false")
	}

	// Verify the title was set.
	got, err := st.GetConversation(ctx, owner.ID, conv.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.Title != "First Title" {
		t.Fatalf("expected title=%q, got %q", "First Title", got.Title)
	}

	// Trying to set a title when one already exists should return false
	// (no update).
	updated2, err := st.SetConversationTitleIfEmpty(ctx, conv.ID, "Second Title")
	if err != nil {
		t.Fatalf("SetConversationTitleIfEmpty (second attempt): %v", err)
	}
	if updated2 {
		t.Fatalf("expected updated=false when title already set, got true")
	}

	// Verify the original title was NOT overwritten.
	got2, err := st.GetConversation(ctx, owner.ID, conv.ID)
	if err != nil {
		t.Fatalf("get conversation (second attempt): %v", err)
	}
	if got2.Title != "First Title" {
		t.Fatalf("expected title unchanged at %q, got %q", "First Title", got2.Title)
	}

	// Create a conversation with a non-empty title from the start.
	withTitle, err := st.CreateConversation(ctx, owner.ID, nil, "Preset Title", nil)
	if err != nil {
		t.Fatalf("create conversation with title: %v", err)
	}

	// Trying to set the title should return false (not updated).
	updated3, err := st.SetConversationTitleIfEmpty(ctx, withTitle.ID, "Override Attempt")
	if err != nil {
		t.Fatalf("SetConversationTitleIfEmpty (preset title): %v", err)
	}
	if updated3 {
		t.Fatalf("expected updated=false for preset title, got true")
	}

	// Verify the preset title was not overwritten.
	got3, err := st.GetConversation(ctx, owner.ID, withTitle.ID)
	if err != nil {
		t.Fatalf("get conversation (preset title): %v", err)
	}
	if got3.Title != "Preset Title" {
		t.Fatalf("expected title unchanged at %q, got %q", "Preset Title", got3.Title)
	}
}
