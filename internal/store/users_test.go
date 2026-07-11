package store_test

import (
	"context"
	"errors"
	"testing"

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
