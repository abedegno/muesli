package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestDBErrorReturns500 verifies that when the store returns a non-validation
// DB error (e.g. table not found), the handler responds with HTTP 500 and a
// generic "internal error" body — no pgx internals exposed.
func TestDBErrorReturns500(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	// Register a user and obtain a session token.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "sec07a@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "sec07a@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	if login.Token == "" {
		t.Fatal("login failed: no token returned")
	}
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Drop the smart_lists table to simulate an unexpected DB error.
	// The auth middleware only touches the tokens table, so auth still works.
	ctx := context.Background()
	if _, err := st.Pool().Exec(ctx, "DROP TABLE IF EXISTS smart_lists CASCADE"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	// POST with a syntactically valid body — passes in-process validation but
	// the INSERT now hits a missing table (a real pgx error).
	body := json.RawMessage(`{"name":"X","rule":{"op":"and","children":[{"field":"title","operator":"contains","value":"x"}]}}`)
	r := doJSON(t, srv, http.MethodPost, "/api/smart-lists", body, hdr)
	if r.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d; body=%s", r.Code, r.Body.String())
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if out.Error != "internal error" {
		t.Fatalf("want error=\"internal error\", got %q (pgx internals leaked?)", out.Error)
	}
}

// TestValidationErrorReturns400 verifies that a validation failure (e.g. an
// unknown rule field) returns HTTP 400 with a human-readable message.
func TestValidationErrorReturns400(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	// Register a user and obtain a session token.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "sec07b@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "sec07b@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	if login.Token == "" {
		t.Fatal("login failed: no token returned")
	}
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// POST with an invalid rule: "bad_field" is not a recognised field.
	body := json.RawMessage(`{"name":"X","rule":{"op":"and","children":[{"field":"bad_field","operator":"contains","value":"x"}]}}`)
	r := doJSON(t, srv, http.MethodPost, "/api/smart-lists", body, hdr)
	if r.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", r.Code, r.Body.String())
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if out.Error == "internal error" {
		t.Fatalf("validation error should not say \"internal error\", got %q", out.Error)
	}
	if out.Error == "" {
		t.Fatal("want a non-empty, human-readable validation message")
	}
}
