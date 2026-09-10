package db_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/testutil"
	"github.com/google/uuid"
)

// TestFolderVisibilityDefaultAndCheckConstraint proves the folder_visibility
// migration (issue #12): omitted visibility defaults to 'private', 'shared'
// is accepted, and any other value violates the CHECK constraint.
func TestFolderVisibilityDefaultAndCheckConstraint(t *testing.T) {
	pool := testutil.NewPool(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'h')`,
		ownerID, ownerID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Omitted visibility defaults to 'private'.
	defaultID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO folders (id, owner_id, name) VALUES ($1, $2, 'Defaulted')`,
		defaultID, ownerID); err != nil {
		t.Fatalf("insert without visibility: %v", err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT visibility FROM folders WHERE id=$1`, defaultID).Scan(&got); err != nil {
		t.Fatalf("select visibility: %v", err)
	}
	if got != "private" {
		t.Errorf("default visibility = %q, want %q", got, "private")
	}

	// 'shared' is accepted.
	sharedID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO folders (id, owner_id, name, visibility) VALUES ($1, $2, 'Shared one', 'shared')`,
		sharedID, ownerID); err != nil {
		t.Fatalf("insert with visibility=shared: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT visibility FROM folders WHERE id=$1`, sharedID).Scan(&got); err != nil {
		t.Fatalf("select visibility: %v", err)
	}
	if got != "shared" {
		t.Errorf("visibility = %q, want %q", got, "shared")
	}

	// Any other value violates the CHECK constraint.
	badID := uuid.NewString()
	_, err := pool.Exec(ctx,
		`INSERT INTO folders (id, owner_id, name, visibility) VALUES ($1, $2, 'Bad', 'public')`,
		badID, ownerID)
	if err == nil {
		t.Fatal("expected CHECK constraint violation for visibility='public', got nil error")
	}
}
