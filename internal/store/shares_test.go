package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI runner.

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestCreateShareGeneratesRandomURLSafeToken(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "shares-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "Shared")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	s1, err := st.CreateShare(ctx, owner.ID, note.ID, nil)
	if err != nil {
		t.Fatalf("create share 1: %v", err)
	}
	s2, err := st.CreateShare(ctx, owner.ID, note.ID, nil)
	if err != nil {
		t.Fatalf("create share 2: %v", err)
	}

	if s1.Token == s2.Token {
		t.Fatalf("tokens matched: %q", s1.Token)
	}
	if len(s1.Token) < 22 || len(s2.Token) < 22 {
		t.Fatalf("token too short: %d, %d", len(s1.Token), len(s2.Token))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(s1.Token) || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(s2.Token) {
		t.Fatalf("token not URL-safe: %q %q", s1.Token, s2.Token)
	}
}

func TestShareActiveLookupRevokedExpiredAndActive(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "shares-active@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	note, err := st.CreateNote(ctx, owner.ID, "Shared")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	active, err := st.CreateShare(ctx, owner.ID, note.ID, nil)
	if err != nil {
		t.Fatalf("create active share: %v", err)
	}
	got, err := st.GetActiveShare(ctx, active.Token)
	if err != nil {
		t.Fatalf("get active share: %v", err)
	}
	if got == nil || got.ID != active.ID || got.Token != active.Token || got.NoteID != note.ID || got.OwnerID != owner.ID {
		t.Fatalf("active share = %+v, want %+v", got, active)
	}

	revoked, err := st.CreateShare(ctx, owner.ID, note.ID, nil)
	if err != nil {
		t.Fatalf("create revoked share: %v", err)
	}
	if err := st.RevokeShare(ctx, owner.ID, revoked.ID); err != nil {
		t.Fatalf("revoke share: %v", err)
	}
	if got, err := st.GetActiveShare(ctx, revoked.Token); !errors.Is(err, store.ErrNotFound) || got != nil {
		t.Fatalf("revoked lookup = (%+v, %v), want (nil, ErrNotFound)", got, err)
	}

	expiredAt := time.Now().Add(-time.Hour)
	expired, err := st.CreateShare(ctx, owner.ID, note.ID, &expiredAt)
	if err != nil {
		t.Fatalf("create expired share: %v", err)
	}
	if got, err := st.GetActiveShare(ctx, expired.Token); !errors.Is(err, store.ErrNotFound) || got != nil {
		t.Fatalf("expired lookup = (%+v, %v), want (nil, ErrNotFound)", got, err)
	}
}

func TestShareOwnerScope(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "shares-owner-scope@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "shares-other-scope@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	ownedNote, err := st.CreateNote(ctx, owner.ID, "Owned")
	if err != nil {
		t.Fatalf("create owned note: %v", err)
	}
	foreignNote, err := st.CreateNote(ctx, other.ID, "Foreign")
	if err != nil {
		t.Fatalf("create foreign note: %v", err)
	}

	if _, err := st.CreateShare(ctx, owner.ID, foreignNote.ID, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("create share on foreign note = %v, want ErrNotFound", err)
	}
	if _, err := st.ListSharesForNote(ctx, other.ID, ownedNote.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("list shares for foreign owner = %v, want ErrNotFound", err)
	}

	share, err := st.CreateShare(ctx, owner.ID, ownedNote.ID, nil)
	if err != nil {
		t.Fatalf("create owned share: %v", err)
	}
	if err := st.RevokeShare(ctx, other.ID, share.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke foreign owner share = %v, want ErrNotFound", err)
	}

	shares, err := st.ListSharesForNote(ctx, owner.ID, ownedNote.ID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 1 || shares[0].ID != share.ID {
		t.Fatalf("shares = %+v, want one share %q", shares, share.ID)
	}
}
