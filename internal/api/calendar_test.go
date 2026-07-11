package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI
// runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func newCalendarTestServer(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	return api.NewServer(api.Deps{Store: st, Crypto: cr}), st
}

// calendarAuthHeader sets up a fresh user via /api/setup + /api/login and
// returns the bearer-auth header map for subsequent requests.
func calendarAuthHeader(t *testing.T, srv *api.Server, email string) map[string]string {
	t.Helper()
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return map[string]string{"Authorization": "Bearer " + login.Token}
}

// calendarAuthHeaderForOtherUser mints a session token directly via the
// store for a second user, bypassing /api/setup (which only provisions the
// first admin account).
func calendarAuthHeaderForOtherUser(t *testing.T, st *store.Store, email string) map[string]string {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, email, "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(ctx, u.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + raw}
}

// TestCalendarSourcesOwnerScoping verifies that one user's calendar sources
// are invisible to and unmodifiable by another user.
func TestCalendarSourcesOwnerScoping(t *testing.T) {
	t.Parallel()
	srv, st := newCalendarTestServer(t)
	ownerHdr := calendarAuthHeader(t, srv, "cal-owner@example.com")
	otherHdr := calendarAuthHeaderForOtherUser(t, st, "cal-other@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/calendar/sources",
		map[string]string{"kind": "ics", "display_name": "Team", "url": "https://example.invalid/cal.ics"}, ownerHdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create source status %d body %s", rec.Code, rec.Body)
	}
	var src struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &src); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if src.ID == "" {
		t.Fatalf("expected a source id, got empty")
	}

	// Owner sees their own source.
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/sources", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner list status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), src.ID) {
		t.Fatalf("owner list missing source: %s", rec.Body.String())
	}

	// Other user's list must not include it.
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/sources", nil, otherHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("other list status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), src.ID) {
		t.Fatalf("other user's list leaked owner's source: %s", rec.Body.String())
	}

	// Other user cannot select calendars on the owner's source.
	rec = doJSON(t, srv, http.MethodPost, "/api/calendar/sources/"+src.ID+"/select",
		map[string]any{"selected": map[string]bool{"cal-1": true}}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner select status %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Other user cannot delete the owner's source.
	rec = doJSON(t, srv, http.MethodDelete, "/api/calendar/sources/"+src.ID, nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete status %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Owner's source must still exist afterwards.
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/sources", nil, ownerHdr)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), src.ID) {
		t.Fatalf("owner source disappeared after cross-owner attempts: status %d body %s", rec.Code, rec.Body)
	}

	// Owner can select and delete their own source.
	rec = doJSON(t, srv, http.MethodPost, "/api/calendar/sources/"+src.ID+"/select",
		map[string]any{"selected": map[string]bool{"cal-1": true}}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner select status %d body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/calendar/sources/"+src.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner delete status %d body %s", rec.Code, rec.Body)
	}
}

// TestCalendarSourceNeverLeaksCredentials asserts that no secret value
// POSTed when creating a source is ever echoed back, either in the create
// response or in a subsequent list response.
func TestCalendarSourceNeverLeaksCredentials(t *testing.T) {
	t.Parallel()
	srv, _ := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "cal-secret@example.com")

	const secretURL = "https://example.invalid/very-secret-path-9f8e7d6c.ics"
	const secretUser = "super-secret-user-1a2b3c"
	const secretPass = "super-secret-pass-4d5e6f"

	rec := doJSON(t, srv, http.MethodPost, "/api/calendar/sources",
		map[string]string{
			"kind":         "caldav",
			"display_name": "Secret Calendar",
			"url":          secretURL,
			"user":         secretUser,
			"pass":         secretPass,
		}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create source status %d body %s", rec.Code, rec.Body)
	}
	createBody := rec.Body.String()
	for _, secret := range []string{secretURL, secretUser, secretPass} {
		if strings.Contains(createBody, secret) {
			t.Fatalf("create response leaked secret %q: %s", secret, createBody)
		}
	}
	for _, field := range []string{"credentials", "sealed", "\"url\"", "\"user\"", "\"pass\""} {
		if strings.Contains(createBody, field) {
			t.Fatalf("create response contains forbidden field %q: %s", field, createBody)
		}
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/sources", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	listBody := rec.Body.String()
	for _, secret := range []string{secretURL, secretUser, secretPass} {
		if strings.Contains(listBody, secret) {
			t.Fatalf("list response leaked secret %q: %s", secret, listBody)
		}
	}
	for _, field := range []string{"credentials", "sealed"} {
		if strings.Contains(listBody, field) {
			t.Fatalf("list response contains forbidden field %q: %s", field, listBody)
		}
	}
}

// TestCreateCalendarSourceRejectsBadKind asserts that only "ics" and
// "caldav" are accepted, even though the DB CHECK constraint also allows
// "google"/"microsoft" - those are out of scope for this slice.
func TestCreateCalendarSourceRejectsBadKind(t *testing.T) {
	t.Parallel()
	srv, _ := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "cal-badkind@example.com")

	for _, kind := range []string{"google", "microsoft", "bogus", ""} {
		rec := doJSON(t, srv, http.MethodPost, "/api/calendar/sources",
			map[string]string{"kind": kind, "display_name": "X", "url": "https://example.invalid/cal.ics"}, hdr)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("kind=%q status %d, want 400; body %s", kind, rec.Code, rec.Body)
		}
	}
}

// TestCreateCalendarSourceRequiresURL asserts an empty url is rejected.
func TestCreateCalendarSourceRequiresURL(t *testing.T) {
	t.Parallel()
	srv, _ := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "cal-nourl@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/calendar/sources",
		map[string]string{"kind": "ics", "display_name": "X", "url": ""}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty url status %d, want 400; body %s", rec.Code, rec.Body)
	}
}

// TestCalendarRefreshAndEvents exercises the refresh (202, immediate
// response - does not wait on the async sync) and events-list endpoints,
// including default date-window behavior and RFC3339 parse errors. It
// intentionally never asserts on source status/last_synced_at, since those
// are set by the async SyncSource goroutine kicked in the background and
// racing this test would be flaky; the fake unreachable URL is expected to
// fail that background sync, which is fine.
func TestCalendarRefreshAndEvents(t *testing.T) {
	t.Parallel()
	srv, _ := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "cal-refresh@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/calendar/sources",
		map[string]string{"kind": "ics", "display_name": "Team", "url": "https://example.invalid/cal.ics"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create source status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/calendar/refresh", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("refresh status %d, want 202; body %s", rec.Code, rec.Body)
	}
	var accepted struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &accepted)
	if accepted.Status != "accepted" {
		t.Fatalf("refresh status field = %q, want accepted", accepted.Status)
	}

	// Default from/to window: no query params.
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/events", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("events status %d body %s", rec.Code, rec.Body)
	}
	var events []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events yet (async sync hasn't necessarily run/succeeded), got %d", len(events))
	}

	// Bad from/to must 400.
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/events?from=not-a-date", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad from status %d, want 400; body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/events?from=2026-01-01T00:00:00Z&to=not-a-date", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad to status %d, want 400; body %s", rec.Code, rec.Body)
	}

	// Valid explicit window.
	rec = doJSON(t, srv, http.MethodGet, "/api/calendar/events?from=2026-01-01T00:00:00Z&to=2026-01-08T00:00:00Z", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("explicit window status %d body %s", rec.Code, rec.Body)
	}
}
