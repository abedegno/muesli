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

func TestRelatedNotesRankingAndFilters(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "related-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "related-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	target, err := st.CreateNote(ctx, owner.ID, "Target")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	near, err := st.CreateNote(ctx, owner.ID, "Near")
	if err != nil {
		t.Fatalf("create near: %v", err)
	}
	far, err := st.CreateNote(ctx, owner.ID, "Far")
	if err != nil {
		t.Fatalf("create far: %v", err)
	}
	self, err := st.CreateNote(ctx, owner.ID, "Self")
	if err != nil {
		t.Fatalf("create self: %v", err)
	}
	outgoing, err := st.CreateNote(ctx, owner.ID, "Outgoing linked")
	if err != nil {
		t.Fatalf("create outgoing: %v", err)
	}
	incoming, err := st.CreateNote(ctx, owner.ID, "Incoming linked")
	if err != nil {
		t.Fatalf("create incoming: %v", err)
	}
	trashed, err := st.CreateNote(ctx, owner.ID, "Trashed")
	if err != nil {
		t.Fatalf("create trashed: %v", err)
	}
	foreign, err := st.CreateNote(ctx, other.ID, "Foreign")
	if err != nil {
		t.Fatalf("create foreign: %v", err)
	}
	missing, err := st.CreateNote(ctx, owner.ID, "Missing embedding")
	if err != nil {
		t.Fatalf("create missing: %v", err)
	}

	for _, id := range []string{target.ID, near.ID, far.ID, self.ID, outgoing.ID, incoming.ID, trashed.ID, foreign.ID, missing.ID} {
		if _, err := st.Pool().Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", id); err != nil {
			t.Fatalf("set ready %s: %v", id, err)
		}
	}

	if err := st.UpsertEmbedding(ctx, target.ID, testModel, queryVec()); err != nil {
		t.Fatalf("upsert target: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, near.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert near: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, far.ID, testModel, farVec()); err != nil {
		t.Fatalf("upsert far: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, self.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert self: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, outgoing.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert outgoing: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, incoming.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert incoming: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, trashed.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert trashed: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, foreign.ID, testModel, nearVec()); err != nil {
		t.Fatalf("upsert foreign: %v", err)
	}

	if _, err := st.AddLink(ctx, owner.ID, target.ID, outgoing.ID); err != nil {
		t.Fatalf("add outgoing link: %v", err)
	}
	if _, err := st.AddLink(ctx, owner.ID, incoming.ID, target.ID); err != nil {
		t.Fatalf("add incoming link: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, "UPDATE notes SET deleted_at=now() WHERE id=$1", trashed.ID); err != nil {
		t.Fatalf("trash candidate: %v", err)
	}

	related, err := st.RelatedNotes(ctx, owner.ID, target.ID, testModel, 768, 10)
	if err != nil {
		t.Fatalf("related notes: %v", err)
	}
	if len(related) != 2 {
		t.Fatalf("related = %+v, want 2 notes", related)
	}
	if related[0].ID != near.ID || related[1].ID != far.ID {
		t.Fatalf("related ordering = %+v, want near %s then far %s", related, near.ID, far.ID)
	}
	if related[0].Score < related[1].Score {
		t.Fatalf("scores not descending: near=%v far=%v", related[0].Score, related[1].Score)
	}

	got, err := st.RelatedNotes(ctx, owner.ID, missing.ID, testModel, 768, 10)
	if err != nil {
		t.Fatalf("missing embedding related: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing embedding related = %+v, want empty slice", got)
	}

	if _, err := st.RelatedNotes(ctx, owner.ID, foreign.ID, testModel, 768, 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign owner err = %v, want ErrNotFound", err)
	}
}
