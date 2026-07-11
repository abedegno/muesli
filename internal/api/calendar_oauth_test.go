package api

import (
	"testing"
	"time"
)

func TestGoogleOAuthStateStoreIssueAndConsume(t *testing.T) {
	t.Parallel()

	var s googleOAuthStateStore
	state, err := s.issue("user-1", "token-hash", time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if state == "" {
		t.Fatal("expected non-empty state")
	}

	rec, ok := s.consume(state)
	if !ok {
		t.Fatal("expected state to be consumable")
	}
	if rec.userID != "user-1" || rec.tokenHash != "token-hash" {
		t.Fatalf("record = %+v", rec)
	}
	if _, ok := s.consume(state); ok {
		t.Fatal("expected state to be single-use")
	}
}

func TestGoogleOAuthStateStoreExpires(t *testing.T) {
	t.Parallel()

	var s googleOAuthStateStore
	state, err := s.issue("user-1", "token-hash", -time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, ok := s.consume(state); ok {
		t.Fatal("expected expired state to be rejected")
	}
}
