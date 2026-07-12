package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestSpeakerAliases runs all speaker-alias store tests in sub-tests under a
// single pool to avoid exhausting Postgres max_connections (each parallel
// top-level test creates its own pool).
func TestSpeakerAliases(t *testing.T) {
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	// Helper to create a unique owner for each sub-test.
	nextOwner := func(t *testing.T, tag string) string {
		t.Helper()
		u, err := st.CreateUser(ctx,
			fmt.Sprintf("alias-%s-%d@example.com", tag, seedUserCounter.Add(1)), "h")
		if err != nil {
			t.Fatalf("create owner: %v", err)
		}
		return u.ID
	}

	t.Run("EmptyList", func(t *testing.T) {
		ownerID := nextOwner(t, "empty")
		n, err := st.CreateNote(ctx, ownerID, "Meeting")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		aliases, err := st.ListSpeakerAliases(ctx, ownerID, n.ID)
		if err != nil {
			t.Fatalf("list aliases: %v", err)
		}
		if len(aliases) != 0 {
			t.Errorf("got %d aliases, want 0", len(aliases))
		}
	})

	t.Run("UpsertCreatesAndUpdates", func(t *testing.T) {
		ownerID := nextOwner(t, "upsert")
		n, err := st.CreateNote(ctx, ownerID, "Standup")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}

		// Create.
		if err := st.UpsertSpeakerAlias(ctx, ownerID, n.ID, "SPEAKER_00", "Alice"); err != nil {
			t.Fatalf("upsert alias: %v", err)
		}
		aliases, err := st.ListSpeakerAliases(ctx, ownerID, n.ID)
		if err != nil {
			t.Fatalf("list aliases: %v", err)
		}
		if len(aliases) != 1 || aliases[0].SpeakerLabel != "SPEAKER_00" || aliases[0].AliasName != "Alice" {
			t.Fatalf("unexpected alias: %+v", aliases)
		}

		// Update (idempotent upsert).
		if err := st.UpsertSpeakerAlias(ctx, ownerID, n.ID, "SPEAKER_00", "Alicia"); err != nil {
			t.Fatalf("re-upsert alias: %v", err)
		}
		aliases2, err := st.ListSpeakerAliases(ctx, ownerID, n.ID)
		if err != nil {
			t.Fatalf("list aliases after update: %v", err)
		}
		if len(aliases2) != 1 || aliases2[0].AliasName != "Alicia" {
			t.Errorf("after upsert: got %+v, want AliasName=Alicia", aliases2)
		}
	})

	t.Run("SetPersonAndClear", func(t *testing.T) {
		ownerID := nextOwner(t, "person")
		n, err := st.CreateNote(ctx, ownerID, "Planning")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		if err := st.UpsertSpeakerAlias(ctx, ownerID, n.ID, "SPEAKER_02", "Carol"); err != nil {
			t.Fatalf("upsert alias: %v", err)
		}
		person, err := st.UpsertPerson(ctx, ownerID, "carol@example.com", "Carol", nil)
		if err != nil {
			t.Fatalf("upsert person: %v", err)
		}

		if err := st.SetSpeakerAliasPerson(ctx, ownerID, n.ID, "SPEAKER_02", &person.ID); err != nil {
			t.Fatalf("set speaker alias person: %v", err)
		}
		aliases, err := st.ListSpeakerAliases(ctx, ownerID, n.ID)
		if err != nil {
			t.Fatalf("list aliases after set: %v", err)
		}
		if len(aliases) != 1 || aliases[0].PersonID == nil || *aliases[0].PersonID != person.ID {
			t.Fatalf("after set: got %+v, want PersonID=%s", aliases, person.ID)
		}

		if err := st.SetSpeakerAliasPerson(ctx, ownerID, n.ID, "SPEAKER_02", nil); err != nil {
			t.Fatalf("clear speaker alias person: %v", err)
		}
		aliases2, err := st.ListSpeakerAliases(ctx, ownerID, n.ID)
		if err != nil {
			t.Fatalf("list aliases after clear: %v", err)
		}
		if len(aliases2) != 1 || aliases2[0].PersonID != nil {
			t.Fatalf("after clear: got %+v, want PersonID=nil", aliases2)
		}
	})

	t.Run("DeleteRemovesAndReturnsNotFound", func(t *testing.T) {
		ownerID := nextOwner(t, "delete")
		n, err := st.CreateNote(ctx, ownerID, "Review")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		if err := st.UpsertSpeakerAlias(ctx, ownerID, n.ID, "SPEAKER_01", "Bob"); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := st.DeleteSpeakerAlias(ctx, ownerID, n.ID, "SPEAKER_01"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		err = st.DeleteSpeakerAlias(ctx, ownerID, n.ID, "SPEAKER_01")
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("second delete: got %v, want ErrNotFound", err)
		}
	})

	t.Run("OwnerIsolation", func(t *testing.T) {
		ownerA := nextOwner(t, "isoA")
		ownerB := nextOwner(t, "isoB")

		n, err := st.CreateNote(ctx, ownerA, "A's meeting")
		if err != nil {
			t.Fatalf("create note: %v", err)
		}
		if err := st.UpsertSpeakerAlias(ctx, ownerA, n.ID, "SPEAKER_00", "Alice"); err != nil {
			t.Fatalf("upsert alias: %v", err)
		}

		// Owner B sees nothing for the same note_id.
		aliases, err := st.ListSpeakerAliases(ctx, ownerB, n.ID)
		if err != nil {
			t.Fatalf("list aliases for owner B: %v", err)
		}
		if len(aliases) != 0 {
			t.Errorf("owner B sees %d aliases, want 0", len(aliases))
		}
	})

	t.Run("UpsertNoteNotFound", func(t *testing.T) {
		ownerID := nextOwner(t, "nf")
		err := st.UpsertSpeakerAlias(ctx, ownerID, "00000000-0000-0000-0000-000000000000", "SPEAKER_00", "X")
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("SetPersonNotFound", func(t *testing.T) {
		ownerID := nextOwner(t, "setnf")
		personID := "00000000-0000-0000-0000-000000000000"
		err := st.SetSpeakerAliasPerson(ctx, ownerID, "00000000-0000-0000-0000-000000000000", "SPEAKER_00", &personID)
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})
}
