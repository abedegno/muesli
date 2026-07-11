package api

import (
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/config"
)

func TestMicrosoftOAuthStateStoreIssueAndConsume(t *testing.T) {
	t.Parallel()

	var s microsoftOAuthStateStore
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

func TestMicrosoftOAuthStateStoreExpires(t *testing.T) {
	t.Parallel()

	var s microsoftOAuthStateStore
	state, err := s.issue("user-1", "token-hash", -time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, ok := s.consume(state); ok {
		t.Fatal("expected expired state to be rejected")
	}
}

func TestMicrosoftOAuthConfigUsesAzureADAndOfflineAccess(t *testing.T) {
	srv := &Server{deps: Deps{Config: config.Config{
		MicrosoftOAuthClientID:     "client-id",
		MicrosoftOAuthClientSecret: "client-secret",
		MicrosoftOAuthRedirectURL:  "https://example.test/api/calendar/oauth/microsoft/callback",
	}}}

	cfg, ok := srv.microsoftOAuthConfig()
	if !ok {
		t.Fatal("expected microsoft oauth config to be available")
	}
	if cfg.ClientID != "client-id" || cfg.ClientSecret != "client-secret" {
		t.Fatalf("config client values = %q %q", cfg.ClientID, cfg.ClientSecret)
	}
	if cfg.RedirectURL != "https://example.test/api/calendar/oauth/microsoft/callback" {
		t.Fatalf("redirect url = %q", cfg.RedirectURL)
	}
	if got, want := cfg.Scopes, []string{"offline_access", microsoftOAuthScope}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("scopes = %#v, want %#v", got, want)
	}
}
