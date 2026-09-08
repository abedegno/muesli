package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// readableFixture builds owner + viewer + stranger, a folder the viewer is
// granted on, a note directly assigned to it, a saved transcript, a tag, and
// a completed summary -- one real fixture reused by every readable-component
// test below.
type readableFixture struct {
	st                            *store.Store
	ownerID, viewerID, strangerID string
	folderID, noteID              string
}

func newReadableFixture(t *testing.T) readableFixture {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}

	owner, err := st.CreateUser(ctx, "rc-owner@example.com", "hash")
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	viewer, err := st.CreateUser(ctx, "rc-viewer@example.com", "hash")
	if err != nil {
		t.Fatalf("viewer: %v", err)
	}
	stranger, err := st.CreateUser(ctx, "rc-stranger@example.com", "hash")
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	folder, err := st.CreateFolder(ctx, owner.ID, "Shared", nil)
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, folder.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "N")
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, note.ID, folder.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, owner.ID, note.ID, "Body text"); err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, owner.ID, note.ID, "important"); err != nil {
		t.Fatalf("tag: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic", Speaker: "spk_0"}},
	}, 0); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	templates, err := st.TemplatesForSummary(ctx, owner.ID)
	if err != nil || len(templates) == 0 {
		t.Fatalf("templates: %v (%d)", err, len(templates))
	}
	sumID, err := st.CreatePendingSummary(ctx, note.ID, templates[0].ID)
	if err != nil {
		t.Fatalf("pending summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, sumID, "ollama", "llama3",
		[]model.SummarySection{{Heading: "H", ContentMarkdown: "md"}}, false); err != nil {
		t.Fatalf("complete summary: %v", err)
	}

	return readableFixture{st: st, ownerID: owner.ID, viewerID: viewer.ID, strangerID: stranger.ID, folderID: folder.ID, noteID: note.ID}
}

func TestGetReadableNoteBodyOwnerViewerStranger(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	if got, err := f.st.GetReadableNoteBody(ctx, f.ownerID, f.noteID); err != nil || got != "Body text" {
		t.Fatalf("owner: got %q err %v", got, err)
	}
	if got, err := f.st.GetReadableNoteBody(ctx, f.viewerID, f.noteID); err != nil || got != "Body text" {
		t.Fatalf("viewer: got %q err %v", got, err)
	}
	if _, err := f.st.GetReadableNoteBody(ctx, f.strangerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger: want ErrNotFound, got %v", err)
	}
}

func TestGetReadableNoteTagsOwnerViewerStranger(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	for _, uid := range []string{f.ownerID, f.viewerID} {
		tags, err := f.st.GetReadableNoteTags(ctx, uid, f.noteID)
		if err != nil {
			t.Fatalf("uid=%s: %v", uid, err)
		}
		if len(tags) != 1 || tags[0] != "important" {
			t.Fatalf("uid=%s: tags = %v", uid, tags)
		}
	}
	if _, err := f.st.GetReadableNoteTags(ctx, f.strangerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger: want ErrNotFound, got %v", err)
	}
}

func TestGetReadableTranscriptOwnerViewerStranger(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	for _, uid := range []string{f.ownerID, f.viewerID} {
		tr, err := f.st.GetReadableTranscript(ctx, uid, f.noteID)
		if err != nil || len(tr.Segments) != 1 || tr.Segments[0].Text != "hi" {
			t.Fatalf("uid=%s: tr=%+v err=%v", uid, tr, err)
		}
	}
	if _, err := f.st.GetReadableTranscript(ctx, f.strangerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger: want ErrNotFound, got %v", err)
	}
}

func TestGetReadableSummariesOwnerViewerStranger(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	for _, uid := range []string{f.ownerID, f.viewerID} {
		sums, err := f.st.GetReadableSummaries(ctx, uid, f.noteID)
		if err != nil || len(sums) != 1 || sums[0].Sections[0].Heading != "H" {
			t.Fatalf("uid=%s: sums=%+v err=%v", uid, sums, err)
		}
	}
	if _, err := f.st.GetReadableSummaries(ctx, f.strangerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stranger: want ErrNotFound, got %v", err)
	}
}

func TestReadableComponentsPrivateChildUnderSharedParent(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	owner, _ := st.CreateUser(ctx, "rcc-owner@example.com", "hash")
	viewer, _ := st.CreateUser(ctx, "rcc-viewer@example.com", "hash")
	parent, err := st.CreateFolder(ctx, owner.ID, "Parent", nil)
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	child, err := st.CreateFolder(ctx, owner.ID, "Child", &parent.ID)
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := st.UpsertFolderMember(ctx, owner.ID, parent.ID, viewer.ID, "viewer"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "Child note")
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, note.ID, child.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, owner.ID, note.ID, "secret"); err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, err := st.GetReadableNoteBody(ctx, viewer.ID, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("body via non-inherited parent grant: want ErrNotFound, got %v", err)
	}
	if _, err := st.GetReadableNoteTags(ctx, viewer.ID, note.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tags via non-inherited parent grant: want ErrNotFound, got %v", err)
	}
}

func TestReadableComponentsRevokedMembership(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	if err := f.st.DeleteFolderMember(ctx, f.ownerID, f.folderID, f.viewerID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.st.GetReadableNoteBody(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("body after revoke: want ErrNotFound, got %v", err)
	}
	if _, err := f.st.GetReadableNoteTags(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tags after revoke: want ErrNotFound, got %v", err)
	}
	if _, err := f.st.GetReadableTranscript(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("transcript after revoke: want ErrNotFound, got %v", err)
	}
	if _, err := f.st.GetReadableSummaries(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("summaries after revoke: want ErrNotFound, got %v", err)
	}
}

func TestReadableComponentsDeletedFolderRetainedMembership(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	if err := f.st.DeleteFolder(ctx, f.ownerID, f.folderID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := f.st.GetReadableNoteBody(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("body after folder soft delete: want ErrNotFound, got %v", err)
	}
	if _, err := f.st.GetReadableNoteTags(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tags after folder soft delete: want ErrNotFound, got %v", err)
	}

	var remaining int
	if err := f.st.Pool().QueryRow(ctx, `SELECT count(*) FROM folder_members WHERE folder_id=$1`, f.folderID).Scan(&remaining); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected membership row retained, got %d", remaining)
	}
}

func TestReadableComponentsDeletedNoteRetainedRows(t *testing.T) {
	t.Parallel()
	f := newReadableFixture(t)
	ctx := context.Background()

	if err := f.st.DeleteNote(ctx, f.ownerID, f.noteID); err != nil {
		t.Fatalf("soft delete note: %v", err)
	}
	if _, err := f.st.GetReadableNoteBody(ctx, f.viewerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("body after note soft delete: want ErrNotFound, got %v", err)
	}
	if _, err := f.st.GetReadableNoteBody(ctx, f.ownerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner body after note soft delete: want ErrNotFound, got %v", err)
	}
	if _, err := f.st.GetReadableTranscript(ctx, f.ownerID, f.noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner transcript after note soft delete: want ErrNotFound, got %v", err)
	}

	var nfCount int
	if err := f.st.Pool().QueryRow(ctx, `SELECT count(*) FROM note_folders WHERE note_id=$1`, f.noteID).Scan(&nfCount); err != nil {
		t.Fatalf("count note_folders: %v", err)
	}
	if nfCount != 1 {
		t.Fatalf("expected note_folders row retained after soft delete, got %d", nfCount)
	}
}
