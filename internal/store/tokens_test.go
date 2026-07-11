package store_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestTokenStore(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "owner@example.com", "h")

	if err := st.CreateToken(ctx, u.ID, "desktop", "hash-1", "app"); err != nil {
		t.Fatalf("create token: %v", err)
	}

	uid, err := st.UserIDByTokenHash(ctx, "hash-1")
	if err != nil || uid != u.ID {
		t.Fatalf("lookup: uid=%q err=%v", uid, err)
	}

	if _, err := st.UserIDByTokenHash(ctx, "nope"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
