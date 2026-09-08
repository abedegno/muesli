package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestCreateAndInspectInvite(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	admin, err := st.CreateFirstUser(ctx, "inviter@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	expires := time.Now().Add(7 * 24 * time.Hour)
	inv, err := st.CreateInvite(ctx, "hash-of-token", "member", admin.ID, expires)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if inv.ID == "" || inv.Role != "member" || inv.CreatedBy != admin.ID {
		t.Fatalf("unexpected invite: %+v", inv)
	}

	got, err := st.GetLiveInviteByHash(ctx, "hash-of-token")
	if err != nil {
		t.Fatalf("get live invite: %v", err)
	}
	if got.ID != inv.ID || got.Role != "member" {
		t.Fatalf("mismatch: %+v vs %+v", got, inv)
	}

	if _, err := st.GetLiveInviteByHash(ctx, "unknown-hash"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown token: want ErrNotFound, got %v", err)
	}
}

func TestCreateInviteInvalidRole(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	admin, err := st.CreateFirstUser(ctx, "inviter2@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	var ve store.ValidationError
	if _, err := st.CreateInvite(ctx, "h", "root", admin.ID, time.Now().Add(time.Hour)); !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestInviteExpired(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	admin, err := st.CreateFirstUser(ctx, "expiry@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	// Already expired.
	if _, err := st.CreateInvite(ctx, "expired-hash", "member", admin.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("create expired invite: %v", err)
	}
	if _, err := st.GetLiveInviteByHash(ctx, "expired-hash"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired invite: want ErrNotFound, got %v", err)
	}
	if _, err := st.AcceptInvite(ctx, "expired-hash", "new@example.com", "hash"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("accept expired invite: want ErrNotFound, got %v", err)
	}
}

func TestAcceptInviteCreatesUserAndConsumes(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	admin, err := st.CreateFirstUser(ctx, "accept-admin@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := st.CreateInvite(ctx, "accept-hash", "member", admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	u, err := st.AcceptInvite(ctx, "accept-hash", "invitee@example.com", "hashed-pw")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if u.Email != "invitee@example.com" || u.Role != "member" || u.PasswordHash != "hashed-pw" {
		t.Fatalf("unexpected user: %+v", u)
	}

	// The token is now consumed: reuse is rejected.
	if _, err := st.AcceptInvite(ctx, "accept-hash", "second@example.com", "hashed-pw"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reuse: want ErrNotFound, got %v", err)
	}
	if _, err := st.GetLiveInviteByHash(ctx, "accept-hash"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed invite inspect: want ErrNotFound, got %v", err)
	}
}

func TestAcceptInviteDuplicateEmailRollsBack(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	admin, err := st.CreateFirstUser(ctx, "dup-admin@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	// The existing admin's own email collides with the invite's target email.
	if _, err := st.CreateInvite(ctx, "dup-hash", "member", admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if _, err := st.AcceptInvite(ctx, "dup-hash", "dup-admin@example.com", "hashed-pw"); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}

	// Consumption rolled back with the insert -- the invite is still live and
	// can be accepted with a non-colliding email.
	inv, err := st.GetLiveInviteByHash(ctx, "dup-hash")
	if err != nil {
		t.Fatalf("invite should still be live after rollback: %v", err)
	}
	if inv.ConsumedAt != nil {
		t.Fatalf("invite should be unconsumed after rollback, got %+v", inv)
	}

	if _, err := st.AcceptInvite(ctx, "dup-hash", "no-collision@example.com", "hashed-pw"); err != nil {
		t.Fatalf("accept after rollback: %v", err)
	}
}

// TestAcceptInviteConcurrentSerializesOnOneUser fires two concurrent accepts
// of the SAME invite token and requires exactly one to succeed (creating
// exactly one user) and the other to see the invite as already gone.
func TestAcceptInviteConcurrentSerializesOnOneUser(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	admin, err := st.CreateFirstUser(ctx, "race-admin@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := st.CreateInvite(ctx, "race-hash", "member", admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	var wg sync.WaitGroup
	emails := []string{"racer-a@example.com", "racer-b@example.com"}
	errs := make([]error, 2)
	for i, email := range emails {
		wg.Add(1)
		go func(i int, email string) {
			defer wg.Done()
			_, errs[i] = st.AcceptInvite(ctx, "race-hash", email, "hashed-pw")
		}(i, email)
	}
	wg.Wait()

	succeeded, notFound := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, store.ErrNotFound):
			notFound++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || notFound != 1 {
		t.Fatalf("succeeded=%d notFound=%d, want exactly one of each", succeeded, notFound)
	}

	var userCount int
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM users WHERE email IN ($1,$2)`, emails[0], emails[1]).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected exactly one user created from the race, got %d", userCount)
	}
}
