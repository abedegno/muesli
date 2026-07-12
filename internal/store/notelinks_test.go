package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI runner.

import (
	"context"
	"errors"
	"testing"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestNoteLinksAddRemoveAndBacklinks(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "links-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	from, err := st.CreateNote(ctx, owner.ID, "From")
	if err != nil {
		t.Fatalf("create from note: %v", err)
	}
	to, err := st.CreateNote(ctx, owner.ID, "To")
	if err != nil {
		t.Fatalf("create to note: %v", err)
	}

	link, err := st.AddLink(ctx, owner.ID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("add link: %v", err)
	}
	if link.OwnerID != owner.ID || link.FromNoteID != from.ID || link.ToNoteID != to.ID || link.ID == "" {
		t.Fatalf("unexpected link: %+v", link)
	}

	outgoing, err := st.OutgoingLinks(ctx, owner.ID, from.ID)
	if err != nil {
		t.Fatalf("outgoing links: %v", err)
	}
	if len(outgoing) != 1 || outgoing[0].ID != link.ID {
		t.Fatalf("outgoing=%+v, want one link %q", outgoing, link.ID)
	}

	backlinks, err := st.Backlinks(ctx, owner.ID, to.ID)
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].ID != link.ID {
		t.Fatalf("backlinks=%+v, want one link %q", backlinks, link.ID)
	}

	if err := st.RemoveLink(ctx, owner.ID, from.ID, to.ID); err != nil {
		t.Fatalf("remove link: %v", err)
	}

	outgoing, err = st.OutgoingLinks(ctx, owner.ID, from.ID)
	if err != nil {
		t.Fatalf("outgoing after remove: %v", err)
	}
	if len(outgoing) != 0 {
		t.Fatalf("outgoing after remove = %+v, want empty", outgoing)
	}
}

func TestNoteLinksRejectSelfLinkAndForeignOwner(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "links-owner2@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "links-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	owned, err := st.CreateNote(ctx, owner.ID, "Owned")
	if err != nil {
		t.Fatalf("create owned note: %v", err)
	}
	foreign, err := st.CreateNote(ctx, other.ID, "Foreign")
	if err != nil {
		t.Fatalf("create foreign note: %v", err)
	}

	if _, err := st.AddLink(ctx, owner.ID, owned.ID, owned.ID); !errors.Is(err, store.ErrSelfLink) {
		t.Fatalf("self-link err = %v, want ErrSelfLink", err)
	}
	if _, err := st.AddLink(ctx, owner.ID, owned.ID, foreign.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign target err = %v, want ErrNotFound", err)
	}
	if _, err := st.AddLink(ctx, other.ID, owned.ID, foreign.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign source err = %v, want ErrNotFound", err)
	}
	if _, err := st.OutgoingLinks(ctx, owner.ID, foreign.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign owner read err = %v, want ErrNotFound", err)
	}
}

func TestNoteLinksOwnerScopeAndDuplicate(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "links-owner3@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "links-other3@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	from, err := st.CreateNote(ctx, owner.ID, "From")
	if err != nil {
		t.Fatalf("create from note: %v", err)
	}
	to, err := st.CreateNote(ctx, owner.ID, "To")
	if err != nil {
		t.Fatalf("create to note: %v", err)
	}
	foreign, err := st.CreateNote(ctx, other.ID, "Foreign")
	if err != nil {
		t.Fatalf("create foreign note: %v", err)
	}

	if _, err := st.AddLink(ctx, owner.ID, from.ID, to.ID); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if _, err := st.AddLink(ctx, owner.ID, from.ID, to.ID); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("duplicate add err = %v, want ErrDuplicate", err)
	}

	if _, err := st.OutgoingLinks(ctx, other.ID, from.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other owner outgoing err = %v, want ErrNotFound", err)
	}
	if _, err := st.Backlinks(ctx, other.ID, to.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other owner backlinks err = %v, want ErrNotFound", err)
	}
	if err := st.RemoveLink(ctx, other.ID, from.ID, to.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other owner remove err = %v, want ErrNotFound", err)
	}
	if err := st.RemoveLink(ctx, owner.ID, from.ID, foreign.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("remove foreign target err = %v, want ErrNotFound", err)
	}
}
