package store_test

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/google/uuid"
)

func TestNotesStore(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "o@example.com", "h")
	other, _ := st.CreateUser(ctx, "x@example.com", "h")

	n, err := st.CreateNote(ctx, owner.ID, "Sprint planning")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.Status != model.NoteRecording || n.OwnerID != owner.ID {
		t.Fatalf("unexpected note %+v", n)
	}

	got, err := st.GetNote(ctx, owner.ID, n.ID)
	if err != nil || got.ID != n.ID {
		t.Fatalf("get: %+v err=%v", got, err)
	}

	// Other users cannot read it.
	if _, err := st.GetNote(ctx, other.ID, n.ID); err != store.ErrNotFound {
		t.Fatalf("cross-owner read should be ErrNotFound, got %v", err)
	}

	list, err := st.ListNotes(ctx, owner.ID, store.ListNotesFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}
	if l, _ := st.ListNotes(ctx, other.ID, store.ListNotesFilter{}); len(l) != 0 {
		t.Fatalf("other owner list len=%d, want 0", len(l))
	}
}

func TestUpdateNoteTitle(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "o@example.com", "h")
	other, _ := st.CreateUser(ctx, "x@example.com", "h")
	n, _ := st.CreateNote(ctx, owner.ID, "Original title")

	if err := st.UpdateNoteTitle(ctx, owner.ID, n.ID, "New title"); err != nil {
		t.Fatalf("update title: %v", err)
	}
	got, err := st.GetNote(ctx, owner.ID, n.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Title != "New title" {
		t.Fatalf("title=%q, want %q", got.Title, "New title")
	}
	// Cross-owner update is rejected.
	if err := st.UpdateNoteTitle(ctx, other.ID, n.ID, "Hijacked"); err != store.ErrNotFound {
		t.Fatalf("cross-owner update want ErrNotFound, got %v", err)
	}
}

func TestSetNoteHashes(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "o@example.com", "h")
	n, _ := st.CreateNote(ctx, owner.ID, "Hash me")

	if err := st.SetNoteHashes(ctx, n.ID, "raw-hash", ""); err != nil {
		t.Fatalf("set note hashes: %v", err)
	}

	var raw, normalized sql.NullString
	if err := st.Pool().QueryRow(ctx, `SELECT audio_hash, normalized_audio_hash FROM notes WHERE id=$1`, n.ID).Scan(&raw, &normalized); err != nil {
		t.Fatalf("query hashes: %v", err)
	}
	if !raw.Valid || raw.String != "raw-hash" {
		t.Fatalf("audio_hash = %+v, want raw-hash", raw)
	}
	if normalized.Valid {
		t.Fatalf("normalized_audio_hash should be NULL, got %+v", normalized)
	}
}

func TestNoteBodyUpdate(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "o@example.com", "h")
	other, _ := st.CreateUser(ctx, "x@example.com", "h")
	n, _ := st.CreateNote(ctx, owner.ID, "M")

	if err := st.UpdateNoteBody(ctx, owner.ID, n.ID, "# notes"); err != nil {
		t.Fatalf("update: %v", err)
	}
	body, err := st.GetNoteBody(ctx, owner.ID, n.ID)
	if err != nil || body != "# notes" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	// Cross-owner update is rejected.
	if err := st.UpdateNoteBody(ctx, other.ID, n.ID, "x"); err != store.ErrNotFound {
		t.Fatalf("cross-owner update want ErrNotFound, got %v", err)
	}
}

func TestFindNoteByTitleCI(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "lookup@example.com", "h")
	other, _ := st.CreateUser(ctx, "lookup2@example.com", "h")

	unique, err := st.CreateNote(ctx, owner.ID, "Existing Title")
	if err != nil {
		t.Fatalf("create unique: %v", err)
	}
	if _, err := st.CreateNote(ctx, owner.ID, "Existing Title Copy"); err != nil {
		t.Fatalf("create filler: %v", err)
	}
	if _, err := st.CreateNote(ctx, other.ID, "Existing Title"); err != nil {
		t.Fatalf("create foreign: %v", err)
	}

	got, err := st.FindNoteByTitleCI(ctx, owner.ID, "existing title")
	if err != nil {
		t.Fatalf("find unique: %v", err)
	}
	if got.ID != unique.ID || got.OwnerID != owner.ID {
		t.Fatalf("found %+v, want %+v", got, unique)
	}

	trashed, err := st.CreateNote(ctx, owner.ID, "Trashed Title")
	if err != nil {
		t.Fatalf("create trashed: %v", err)
	}
	if err := st.DeleteNote(ctx, owner.ID, trashed.ID); err != nil {
		t.Fatalf("trash note: %v", err)
	}
	if _, err := st.FindNoteByTitleCI(ctx, owner.ID, "trashed title"); err != store.ErrNotFound {
		t.Fatalf("trashed lookup err = %v, want ErrNotFound", err)
	}

	if _, err := st.CreateNote(ctx, owner.ID, "Ambiguous"); err != nil {
		t.Fatalf("create ambig1: %v", err)
	}
	if _, err := st.CreateNote(ctx, owner.ID, "ambiguous"); err != nil {
		t.Fatalf("create ambig2: %v", err)
	}
	if _, err := st.FindNoteByTitleCI(ctx, owner.ID, "AMBIGUOUS"); err != store.ErrNotFound {
		t.Fatalf("ambiguous lookup err = %v, want ErrNotFound", err)
	}

	if _, err := st.FindNoteByTitleCI(ctx, owner.ID, "missing"); err != store.ErrNotFound {
		t.Fatalf("missing lookup err = %v, want ErrNotFound", err)
	}
}

func TestDuplicateNoteCopiesEditableContent(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "o@example.com", "h")

	orig, err := st.CreateNote(ctx, owner.ID, "Original")
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, owner.ID, orig.ID, "# live notes"); err != nil {
		t.Fatalf("update body: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, owner.ID, orig.ID, "work"); err != nil {
		t.Fatalf("add tag work: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, owner.ID, orig.ID, "urgent"); err != nil {
		t.Fatalf("add tag urgent: %v", err)
	}
	folder, err := st.CreateFolder(ctx, owner.ID, "Clients", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, orig.ID, folder.ID); err != nil {
		t.Fatalf("add folder: %v", err)
	}

	copyNote, err := st.DuplicateNote(ctx, owner.ID, orig.ID)
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if copyNote.ID == orig.ID {
		t.Fatalf("duplicate reused original id")
	}
	if copyNote.Title != "Copy of Original" {
		t.Fatalf("title=%q, want %q", copyNote.Title, "Copy of Original")
	}
	if copyNote.Status != model.NoteRecording {
		t.Fatalf("status=%q, want %q", copyNote.Status, model.NoteRecording)
	}
	if copyNote.OwnerID != owner.ID {
		t.Fatalf("owner=%q, want %q", copyNote.OwnerID, owner.ID)
	}

	body, err := st.GetNoteBody(ctx, owner.ID, copyNote.ID)
	if err != nil {
		t.Fatalf("copy body: %v", err)
	}
	if body != "# live notes" {
		t.Fatalf("copy body=%q, want %q", body, "# live notes")
	}
	tags, err := st.NoteTags(ctx, copyNote.ID)
	if err != nil {
		t.Fatalf("copy tags: %v", err)
	}
	sort.Strings(tags)
	if got, want := tags, []string{"urgent", "work"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("copy tags=%v, want %v", got, want)
	}
	folders, err := st.NoteFolderIDs(ctx, copyNote.ID)
	if err != nil {
		t.Fatalf("copy folders: %v", err)
	}
	if len(folders) != 1 || folders[0] != folder.ID {
		t.Fatalf("copy folders=%v, want [%s]", folders, folder.ID)
	}

	got, err := st.GetNote(ctx, owner.ID, copyNote.ID)
	if err != nil {
		t.Fatalf("get copy: %v", err)
	}
	if got.AudioObjectKey != "" || got.AudioHash != nil || got.NormalizedAudioHash != nil {
		t.Fatalf("copy should not inherit audio state: %+v", got)
	}
}

func TestListNotesIncludesSnippet(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "snippet@example.com", "h")
	n, _ := st.CreateNote(ctx, owner.ID, "Snippet test note")

	body := "# Heading\nDiscussed the release plan and the new onboarding flow in detail."
	if err := st.UpdateNoteBody(ctx, owner.ID, n.ID, body); err != nil {
		t.Fatalf("update body: %v", err)
	}

	notes, err := st.ListNotes(ctx, owner.ID, store.ListNotesFilter{})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].Snippet == "" {
		t.Fatal("expected non-empty Snippet, got empty string")
	}
	if r := []rune(notes[0].Snippet); len(r) > 160 {
		t.Fatalf("Snippet too long: %d runes, want <= 160", len(r))
	}
}

func TestListNotesIncludesTags(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t) // third return is *pgxpool.Pool, ignored here
	ctx := context.Background()
	n, _ := st.CreateNote(ctx, ownerID, "Standup")
	if _, err := st.AddNoteTag(ctx, ownerID, n.ID, "1on1"); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("len=%d", len(notes))
	}
	if len(notes[0].Tags) != 1 || notes[0].Tags[0] != "1on1" {
		t.Errorf("tags=%v want [1on1]", notes[0].Tags)
	}
	// untagged note still exposes a non-nil slice
	n2, _ := st.CreateNote(ctx, ownerID, "Empty")
	notes, _ = st.ListNotes(ctx, ownerID, store.ListNotesFilter{})
	for _, x := range notes {
		if x.ID == n2.ID && x.Tags == nil {
			t.Error("untagged note has nil Tags; want []")
		}
	}
}

func TestDeleteNote(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	n, _ := st.CreateNote(ctx, ownerID, "Doomed")
	if err := st.DeleteNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Soft-deleted note is absent from GetNote.
	if _, gerr := st.GetNote(ctx, ownerID, n.ID); !errors.Is(gerr, store.ErrNotFound) {
		t.Errorf("note still present in GetNote after delete: %v", gerr)
	}
	// And absent from ListNotes.
	list, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, x := range list {
		if x.ID == n.ID {
			t.Errorf("trashed note %s still present in ListNotes", n.ID)
		}
	}
	// A second delete on an already-trashed note returns ErrNotFound.
	if derr := st.DeleteNote(ctx, ownerID, n.ID); !errors.Is(derr, store.ErrNotFound) {
		t.Errorf("second delete: want ErrNotFound, got %v", derr)
	}
	// cross-owner delete
	other := addUser(t, st)
	n2, _ := st.CreateNote(ctx, ownerID, "Mine")
	if derr := st.DeleteNote(ctx, other, n2.ID); !errors.Is(derr, store.ErrNotFound) {
		t.Errorf("cross-owner delete: want ErrNotFound, got %v", derr)
	}
}

func TestTrashLifecycle(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	n, err := st.CreateNote(ctx, ownerID, "Doomed")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.DeleteNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Appears in trash.
	trash, err := st.ListTrash(ctx, ownerID)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 1 || trash[0].ID != n.ID {
		t.Fatalf("trash = %+v, want 1 note %s", trash, n.ID)
	}

	// Restore brings it back.
	if err := st.RestoreNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := st.GetNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("get after restore: %v", err)
	}
	trash, err = st.ListTrash(ctx, ownerID)
	if err != nil {
		t.Fatalf("list trash after restore: %v", err)
	}
	if len(trash) != 0 {
		t.Fatalf("trash after restore = %d, want 0", len(trash))
	}

	// Restoring a live note is ErrNotFound.
	if err := st.RestoreNote(ctx, ownerID, n.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("restore live note: want ErrNotFound, got %v", err)
	}

	// Delete again, then purge.
	if err := st.DeleteNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("re-delete: %v", err)
	}
	if _, err := st.PurgeNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	// Second purge is ErrNotFound.
	if _, err := st.PurgeNote(ctx, ownerID, n.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second purge: want ErrNotFound, got %v", err)
	}
}

func TestCrossOwnerTrashIsolation(t *testing.T) {
	t.Parallel()
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()
	n, _ := st.CreateNote(ctx, ownerID, "Private")
	if err := st.DeleteNote(ctx, ownerID, n.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	other := addUser(t, st)
	if err := st.RestoreNote(ctx, other, n.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner restore: want ErrNotFound, got %v", err)
	}
	if _, err := st.PurgeNote(ctx, other, n.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-owner purge: want ErrNotFound, got %v", err)
	}
}

func TestPurgeExpiredOnlyOldEnough(t *testing.T) {
	t.Parallel()
	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	oldNote, _ := st.CreateNote(ctx, ownerID, "Ancient")
	recentNote, _ := st.CreateNote(ctx, ownerID, "Fresh")
	if err := st.DeleteNote(ctx, ownerID, oldNote.ID); err != nil {
		t.Fatalf("delete old: %v", err)
	}
	if err := st.DeleteNote(ctx, ownerID, recentNote.ID); err != nil {
		t.Fatalf("delete recent: %v", err)
	}

	// Backdate the old note's trashing to 31 days ago.
	if _, err := pool.Exec(ctx,
		`UPDATE notes SET deleted_at = now() - interval '31 days' WHERE id=$1`, oldNote.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	if _, err := st.PurgeExpired(ctx, 30*24*time.Hour); err != nil {
		t.Fatalf("purge expired: %v", err)
	}

	// The old note is gone.
	if _, err := st.PurgeNote(ctx, ownerID, oldNote.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("old note still present: want ErrNotFound, got %v", err)
	}
	// The recent note is still in trash.
	trash, err := st.ListTrash(ctx, ownerID)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 1 || trash[0].ID != recentNote.ID {
		t.Fatalf("trash = %+v, want 1 note %s", trash, recentNote.ID)
	}
}

// ---------------------------------------------------------------------------
// ListNotes filter tests
// ---------------------------------------------------------------------------

func TestListNotesFilterNoFilter(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	n1, _ := st.CreateNote(ctx, ownerID, "Note A")
	n2, _ := st.CreateNote(ctx, ownerID, "Note B")

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("want 2 notes, got %d", len(notes))
	}
	ids := map[string]bool{n1.ID: true, n2.ID: true}
	for _, n := range notes {
		if !ids[n.ID] {
			t.Errorf("unexpected note %s in results", n.ID)
		}
	}
}

func TestListNotesFilterByTag(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	tagged, _ := st.CreateNote(ctx, ownerID, "Tagged Note")
	untagged, _ := st.CreateNote(ctx, ownerID, "Untagged Note")
	if _, err := st.AddNoteTag(ctx, ownerID, tagged.ID, "work"); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{Tag: "work"})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("want 1 note, got %d", len(notes))
	}
	if notes[0].ID != tagged.ID {
		t.Errorf("got note %s, want %s", notes[0].ID, tagged.ID)
	}
	_ = untagged
}

func TestListNotesFilterByStatus(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	n1, _ := st.CreateNote(ctx, ownerID, "Ready Note")
	n2, _ := st.CreateNote(ctx, ownerID, "Recording Note")
	// Advance n1 to "ready" status via the store.
	if err := st.SetNoteStatus(ctx, n1.ID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{Status: model.NoteReady})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("want 1 note, got %d", len(notes))
	}
	if notes[0].ID != n1.ID {
		t.Errorf("got note %s, want %s", notes[0].ID, n1.ID)
	}
	_ = n2
}

func TestListNotesFilterByFolder(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	folder, err := st.CreateFolder(ctx, ownerID, "Project Alpha", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	inFolder, _ := st.CreateNote(ctx, ownerID, "In Folder")
	notInFolder, _ := st.CreateNote(ctx, ownerID, "Not In Folder")

	if err := st.AddNoteFolder(ctx, ownerID, inFolder.ID, folder.ID); err != nil {
		t.Fatalf("add note to folder: %v", err)
	}

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{
		FolderID:    folder.ID,
		FolderIDSet: true,
	})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("want 1 note, got %d", len(notes))
	}
	if notes[0].ID != inFolder.ID {
		t.Errorf("got note %s, want %s", notes[0].ID, inFolder.ID)
	}
	_ = notInFolder
}

func TestListNotesFilterTwoFiltersCombined(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	// Note that matches both: tag "work" + status "ready"
	both, _ := st.CreateNote(ctx, ownerID, "Both match")
	if err := st.SetNoteStatus(ctx, both.ID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, ownerID, both.ID, "work"); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	// Note that has the tag but wrong status
	tagOnly, _ := st.CreateNote(ctx, ownerID, "Tag only")
	if _, err := st.AddNoteTag(ctx, ownerID, tagOnly.ID, "work"); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	// Note that has the status but no tag
	statusOnly, _ := st.CreateNote(ctx, ownerID, "Status only")
	if err := st.SetNoteStatus(ctx, statusOnly.ID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{
		Tag:    "work",
		Status: model.NoteReady,
	})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("want 1 note (intersection), got %d", len(notes))
	}
	if notes[0].ID != both.ID {
		t.Errorf("got note %s, want %s", notes[0].ID, both.ID)
	}
}

func TestListNotesFilterUnknownTagReturnsEmpty(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	_, _ = st.CreateNote(ctx, ownerID, "Some note")

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{Tag: "does-not-exist"})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("want 0 notes for unknown tag, got %d", len(notes))
	}
}

func TestListNotesFilterUnknownStatusReturnsEmpty(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	_, _ = st.CreateNote(ctx, ownerID, "Some note")

	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{Status: "nonexistent-status"})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("want 0 notes for unknown status, got %d", len(notes))
	}
}

func TestListNotesFilterUnknownFolderIDReturnsEmpty(t *testing.T) {
	st, ownerID, _ := newStoreWithOwner(t)
	ctx := context.Background()

	_, _ = st.CreateNote(ctx, ownerID, "Some note")

	// Use a valid-format UUID that refers to no existing folder.
	unknownFolderID := "00000000-0000-0000-0000-000000000001"
	notes, err := st.ListNotes(ctx, ownerID, store.ListNotesFilter{
		FolderID:    unknownFolderID,
		FolderIDSet: true,
	})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("want 0 notes for unknown folder, got %d", len(notes))
	}
}

func TestListTrashDeletedAt(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "deletedAt@example.com", "h")

	n, err := st.CreateNote(ctx, owner.ID, "Trashed note")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := time.Now().Add(-time.Second)
	if err := st.DeleteNote(ctx, owner.ID, n.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after := time.Now().Add(time.Second)

	trash, err := st.ListTrash(ctx, owner.ID)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 1 {
		t.Fatalf("expected 1 trashed note, got %d", len(trash))
	}
	got := trash[0]
	if got.DeletedAt == nil {
		t.Fatal("DeletedAt is nil, want a non-nil timestamp")
	}
	if got.DeletedAt.Before(before) || got.DeletedAt.After(after) {
		t.Fatalf("DeletedAt %v is not within expected range [%v, %v]", got.DeletedAt, before, after)
	}
}

// TestSetNotePartialTranscript verifies that SetNotePartialTranscript correctly
// sets and clears the partial_transcript flag, and that GetNote reflects the change.
func TestSetNotePartialTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "partial@example.com", "h")

	n, err := st.CreateNote(ctx, owner.ID, "Partial test note")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	// Initially false.
	got, err := st.GetNote(ctx, owner.ID, n.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.PartialTranscript {
		t.Error("new note should have PartialTranscript=false")
	}

	// Set to true.
	if err := st.SetNotePartialTranscript(ctx, n.ID, true); err != nil {
		t.Fatalf("SetNotePartialTranscript(true): %v", err)
	}
	got, err = st.GetNote(ctx, owner.ID, n.ID)
	if err != nil {
		t.Fatalf("GetNote after set true: %v", err)
	}
	if !got.PartialTranscript {
		t.Error("expected PartialTranscript=true after SetNotePartialTranscript(true)")
	}

	// Clear back to false.
	if err := st.SetNotePartialTranscript(ctx, n.ID, false); err != nil {
		t.Fatalf("SetNotePartialTranscript(false): %v", err)
	}
	got, err = st.GetNote(ctx, owner.ID, n.ID)
	if err != nil {
		t.Fatalf("GetNote after set false: %v", err)
	}
	if got.PartialTranscript {
		t.Error("expected PartialTranscript=false after SetNotePartialTranscript(false)")
	}
}

// TestRetryNoteResetsPartialTranscript verifies that RetryNote clears
// partial_transcript as part of its atomic reset transaction.
func TestRetryNoteResetsPartialTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "retry-partial@example.com", "h")

	n, err := st.CreateNote(ctx, owner.ID, "Retry partial note")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	// Simulate the note going through a partial run: mark it partial+failed.
	if err := st.SetNotePartialTranscript(ctx, n.ID, true); err != nil {
		t.Fatalf("SetNotePartialTranscript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, n.ID, "failed"); err != nil {
		t.Fatalf("SetNoteStatus: %v", err)
	}

	// RetryNote must atomically reset status → uploaded and clear partial_transcript.
	if err := st.RetryNote(ctx, n.ID, "transcribe", nil); err != nil {
		t.Fatalf("RetryNote: %v", err)
	}

	got, err := st.GetNote(ctx, owner.ID, n.ID)
	if err != nil {
		t.Fatalf("GetNote after RetryNote: %v", err)
	}
	if got.PartialTranscript {
		t.Error("PartialTranscript should be false after RetryNote")
	}
	if got.Status != "uploaded" {
		t.Errorf("status after RetryNote = %q, want %q", got.Status, "uploaded")
	}
}

// TestMarkNoteReady verifies the atomic ready-transition primitive: the first
// call on a not-yet-ready note wins (won=true) and flips status to ready; a
// second call on the now-already-ready note does not win (won=false, no
// error) because it observes status = 'ready' and its WHERE clause matches
// zero rows.
func TestMarkNoteReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "mark-ready@example.com", "h")
	n, _ := st.CreateNote(ctx, owner.ID, "N")

	won, err := st.MarkNoteReady(ctx, n.ID)
	if err != nil {
		t.Fatalf("MarkNoteReady: %v", err)
	}
	if !won {
		t.Fatal("first MarkNoteReady call on a non-ready note should win")
	}
	got, err := st.GetNote(ctx, owner.ID, n.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Status != model.NoteReady {
		t.Fatalf("status = %q, want %q", got.Status, model.NoteReady)
	}

	won2, err := st.MarkNoteReady(ctx, n.ID)
	if err != nil {
		t.Fatalf("MarkNoteReady (2nd): %v", err)
	}
	if won2 {
		t.Fatal("second MarkNoteReady call on an already-ready note should not win")
	}
}

// TestMarkNoteReadyMissingNote verifies a nonexistent note id is a clean
// no-op: won=false, err=nil (RowsAffected==0 either way).
func TestMarkNoteReadyMissingNote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))

	won, err := st.MarkNoteReady(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("MarkNoteReady: %v", err)
	}
	if won {
		t.Fatal("MarkNoteReady on a nonexistent note should not win")
	}
}

// TestMarkNoteReadyConcurrent is the store-level regression test for the
// FinalizeNote TOCTOU fix: of many concurrent MarkNoteReady calls racing on
// the same not-yet-ready note, exactly one may observe won=true. A
// read-then-write implementation (the bug this replaces) would let multiple
// callers "win" under this race.
func TestMarkNoteReadyConcurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "mark-ready-race@example.com", "h")
	n, _ := st.CreateNote(ctx, owner.ID, "N")

	const races = 20
	var wonCount int32
	var wg sync.WaitGroup
	for i := 0; i < races; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := st.MarkNoteReady(ctx, n.ID)
			if err != nil {
				t.Errorf("MarkNoteReady: %v", err)
				return
			}
			if won {
				atomic.AddInt32(&wonCount, 1)
			}
		}()
	}
	wg.Wait()
	if wonCount != 1 {
		t.Fatalf("won count = %d, want exactly 1 of %d racing calls", wonCount, races)
	}
}
