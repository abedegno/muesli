package store_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestCreateAndGetUser(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "owner@example.com", "hashed")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" || u.Email != "owner@example.com" {
		t.Fatalf("unexpected user %+v", u)
	}

	got, err := st.GetUserByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != u.ID || got.PasswordHash != "hashed" {
		t.Fatalf("mismatch: %+v vs %+v", got, u)
	}

	if _, err := st.CreateUser(ctx, "owner@example.com", "x"); err == nil {
		t.Fatal("expected duplicate-email error")
	}

	if _, err := st.GetUserByEmail(ctx, "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCountUsers(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	n, err := st.CountUsers(ctx)
	if err != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	_, _ = st.CreateUser(ctx, "a@example.com", "h")
	n, _ = st.CountUsers(ctx)
	if n != 1 {
		t.Fatalf("count=%d, want 1", n)
	}
}

func TestCreateFirstUserIsAdmin(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	u, err := st.CreateFirstUser(ctx, "first@example.com", "hash")
	if err != nil {
		t.Fatalf("create first user: %v", err)
	}
	if u.Role != "admin" {
		t.Fatalf("first user role = %q, want admin", u.Role)
	}
	role, err := st.GetUserRole(ctx, u.ID)
	if err != nil {
		t.Fatalf("get role: %v", err)
	}
	if role != "admin" {
		t.Fatalf("GetUserRole = %q, want admin", role)
	}
}

func TestCreateUserIsMember(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	// An ordinary CreateUser (not the first-run setup path) is always a
	// member, even when it happens to be the first row in the table.
	u, err := st.CreateUser(ctx, "ordinary@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.Role != "member" {
		t.Fatalf("ordinary user role = %q, want member", u.Role)
	}
}

func TestGetUser(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "gu@example.com", "hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := st.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != u.ID || got.Email != u.Email || got.Role != u.Role {
		t.Fatalf("mismatch: %+v vs %+v", got, u)
	}
	if _, err := st.GetUser(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListUsersOrderingAndPagination(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	var ids []string
	for i := 0; i < 5; i++ {
		u, err := st.CreateUser(ctx, fmt.Sprintf("page%d@example.com", i), "hash")
		if err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		ids = append(ids, u.ID)
	}

	// First page of 2.
	page1, err := st.ListUsers(ctx, 2, time.Time{}, "")
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}

	// Next page starts strictly after the last row of page1.
	last := page1[len(page1)-1]
	page2, err := st.ListUsers(ctx, 10, last.CreatedAt, last.ID)
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	for _, u := range page2 {
		if u.ID == last.ID {
			t.Fatalf("page2 re-returned the cursor row %s", u.ID)
		}
	}
	// Together, page1 + page2 should cover exactly the 5 created users (order
	// preserved, no duplicates, no gaps) since this is a fresh isolated schema.
	seen := map[string]bool{}
	for _, u := range append(page1, page2...) {
		seen[u.ID] = true
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 distinct users across pages, got %d", len(seen))
	}
}

func TestListUsersTieBreaksByID(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	// Force equal timestamps so ordering can only be resolved by id.
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids []string
	for i := 0; i < 3; i++ {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (id, email, password_hash, created_at) VALUES (gen_random_uuid(),$1,'h',$2) RETURNING id`,
			fmt.Sprintf("tie%d@example.com", i), fixed).Scan(&id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	users, err := st.ListUsers(ctx, 10, time.Time{}, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got []string
	for _, u := range users {
		if u.CreatedAt.Equal(fixed) {
			got = append(got, u.ID)
		}
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 tied users, got %d", len(got))
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Fatalf("tie-break order = %v, want id-sorted %v", got, ids)
		}
	}
}

func TestSetUserRoleIdempotentAndDemotion(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	admin1, err := st.CreateFirstUser(ctx, "admin1@example.com", "hash")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	admin2, err := st.CreateUser(ctx, "admin2@example.com", "hash")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := st.SetUserRole(ctx, admin2.ID, "admin"); err != nil {
		t.Fatalf("promote admin2: %v", err)
	}

	// Reapplying the same role is idempotent.
	if err := st.SetUserRole(ctx, admin2.ID, "admin"); err != nil {
		t.Fatalf("idempotent reapply: %v", err)
	}
	role, err := st.GetUserRole(ctx, admin2.ID)
	if err != nil || role != "admin" {
		t.Fatalf("role=%q err=%v, want admin", role, err)
	}

	// With two admins, demoting one succeeds.
	if err := st.SetUserRole(ctx, admin2.ID, "member"); err != nil {
		t.Fatalf("demote admin2 (two admins): %v", err)
	}

	// With one admin left, demoting the sole admin is rejected.
	if err := st.SetUserRole(ctx, admin1.ID, "member"); !errors.Is(err, store.ErrLastAdmin) {
		t.Fatalf("demote sole admin: got %v, want ErrLastAdmin", err)
	}
	role, err = st.GetUserRole(ctx, admin1.ID)
	if err != nil || role != "admin" {
		t.Fatalf("sole admin role after rejected demotion = %q err=%v, want still admin", role, err)
	}
}

// TestSetUserRoleConcurrentDemotionsLeaveOneAdmin fires two concurrent
// demotions of two DIFFERENT admins (of a three-admin deployment) and
// requires the advisory lock to serialize them so at least one admin
// remains -- neither request may observe a stale admin count.
func TestSetUserRoleConcurrentDemotionsLeaveOneAdmin(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	admin1, err := st.CreateFirstUser(ctx, "c1@example.com", "hash")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	admin2, err := st.CreateUser(ctx, "c2@example.com", "hash")
	if err != nil {
		t.Fatalf("create c2: %v", err)
	}
	if err := st.SetUserRole(ctx, admin2.ID, "admin"); err != nil {
		t.Fatalf("promote c2: %v", err)
	}
	// Exactly two admins going in: demoting both concurrently must leave
	// exactly one standing, never zero.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []string{admin1.ID, admin2.ID}
	for i, id := range targets {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			errs[i] = st.SetUserRole(ctx, id, "member")
		}(i, id)
	}
	wg.Wait()

	succeeded := 0
	rejected := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, store.ErrLastAdmin):
			rejected++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("succeeded=%d rejected=%d, want exactly one of each", succeeded, rejected)
	}
	var admins int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&admins); err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 1 {
		t.Fatalf("admin count after concurrent demotions = %d, want 1", admins)
	}
}

func TestSetUserRoleInvalidRole(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	u, err := st.CreateFirstUser(ctx, "invalidrole@example.com", "hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var ve store.ValidationError
	if err := st.SetUserRole(ctx, u.ID, "root"); !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestSetUserRoleNotFound(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	if err := st.SetUserRole(ctx, "00000000-0000-0000-0000-000000000000", "admin"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
