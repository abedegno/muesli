package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"golang.org/x/oauth2"
)

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "timeout duration containing 401", err: errors.New("context deadline exceeded after 4010ms"), want: false},
		{name: "dial port containing 403", err: errors.New("dial tcp 10.0.0.5:4030: connect: connection refused"), want: false},
		{name: "URL containing 401", err: errors.New("fetch https://calendar.example/events/401-team.ics: connection reset"), want: false},
		{name: "event UID containing 401", err: errors.New("parse event uid-401-planning: invalid DTSTART"), want: false},
		{name: "HTTP 401", err: &calendar.HTTPError{StatusCode: 401, Err: errors.New("denied")}, want: true},
		{name: "HTTP 403", err: fmt.Errorf("wrapped: %w", &calendar.HTTPError{StatusCode: 403, Err: errors.New("denied")}), want: true},
		{name: "OAuth invalid grant", err: &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, want: true},
		{name: "OAuth invalid client", err: &oauth2.RetrieveError{ErrorCode: "invalid_client"}, want: true},
		{name: "OAuth unauthorized client", err: &oauth2.RetrieveError{ErrorCode: "unauthorized_client"}, want: true},
		{name: "OAuth description is not classified", err: &oauth2.RetrieveError{ErrorDescription: "UID invalid_grant-401"}, want: false},
		{name: "unknown transport error", err: errors.New("connection reset by peer"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAuthError(tt.err); got != tt.want {
				t.Fatalf("isAuthError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSyncSourceDispatchAndStatuses(t *testing.T) {
	ctx := context.Background()
	st, cr, ownerID := newCalendarSyncTestState(t)

	originalICS, originalCalDAV := fetchICS, fetchCalDAV
	originalGoogle, originalMicrosoft := fetchGoogle, fetchMicrosoft
	t.Cleanup(func() {
		fetchICS, fetchCalDAV = originalICS, originalCalDAV
		fetchGoogle, fetchMicrosoft = originalGoogle, originalMicrosoft
	})

	called := ""
	fetchICS = func(context.Context, *http.Client, string) ([]calendar.NormalizedEvent, error) {
		called = "ics"
		return nil, nil
	}
	fetchCalDAV = func(context.Context, string, string, string, time.Time, time.Time) ([]calendar.NormalizedEvent, error) {
		called = "caldav"
		return nil, nil
	}
	fetchGoogle = func(context.Context, string, string, string, map[string]bool, time.Time, time.Time) ([]calendar.NormalizedEvent, error) {
		called = "google"
		return nil, nil
	}
	fetchMicrosoft = func(context.Context, string, string, string, map[string]bool, time.Time, time.Time) ([]calendar.NormalizedEvent, error) {
		called = "microsoft"
		return nil, nil
	}

	credentials := map[string]any{
		"ics":       icsCreds{URL: "https://example.test/calendar.ics"},
		"caldav":    caldavCreds{URL: "https://example.test/dav", User: "u", Pass: "p"},
		"google":    googleCreds{RefreshToken: "google-refresh"},
		"microsoft": microsoftCreds{RefreshToken: "microsoft-refresh"},
	}
	for _, kind := range []string{"ics", "caldav", "google", "microsoft"} {
		t.Run(kind+" success", func(t *testing.T) {
			sourceID := createCalendarSyncSource(t, st, cr, ownerID, kind, credentials[kind])
			called = ""
			if err := SyncSource(ctx, st, cr, "google-id", "google-secret", "microsoft-id", "microsoft-secret", sourceID); err != nil {
				t.Fatalf("SyncSource() error = %v", err)
			}
			if called != kind {
				t.Fatalf("dispatched %q fetcher, want %q", called, kind)
			}
			assertSourceStatus(t, st, ownerID, sourceID, "ok")
		})
	}

	// SyncSource's default branch (unknown kind) is deliberately NOT covered here.
	// calendar_sources constrains the column:
	//     kind TEXT NOT NULL CHECK (kind IN ('ics','caldav','google','microsoft'))
	// so no other value can be stored, and SyncSource only ever reads kinds back out
	// of that table. The branch is defence in depth against a future migration
	// widening the constraint, not a reachable state -- a test for it would have to
	// insert a row the database rejects, which is what it did before this comment.

	t.Run("decode credentials", func(t *testing.T) {
		sealed, err := cr.Seal([]byte("not-json"))
		if err != nil {
			t.Fatal(err)
		}
		src, err := st.CreateSource(ctx, ownerID, "ics", "bad credentials", sealed)
		if err != nil {
			t.Fatal(err)
		}
		if err := SyncSource(ctx, st, cr, "", "", "", "", src.ID); err == nil {
			t.Fatal("SyncSource() error = nil, want decode error")
		}
		assertSourceStatus(t, st, ownerID, src.ID, "error")
	})

	t.Run("transient fetch failure", func(t *testing.T) {
		fetchICS = func(context.Context, *http.Client, string) ([]calendar.NormalizedEvent, error) {
			return nil, errors.New("context deadline exceeded after 4010ms")
		}
		sourceID := createCalendarSyncSource(t, st, cr, ownerID, "ics", credentials["ics"])
		if err := SyncSource(ctx, st, cr, "", "", "", "", sourceID); err == nil {
			t.Fatal("SyncSource() error = nil, want fetch error")
		}
		assertSourceStatus(t, st, ownerID, sourceID, "error")
	})

	t.Run("auth fetch failure", func(t *testing.T) {
		fetchICS = func(context.Context, *http.Client, string) ([]calendar.NormalizedEvent, error) {
			return nil, &calendar.HTTPError{StatusCode: http.StatusUnauthorized, Err: errors.New("denied")}
		}
		sourceID := createCalendarSyncSource(t, st, cr, ownerID, "ics", credentials["ics"])
		if err := SyncSource(ctx, st, cr, "", "", "", "", sourceID); err == nil {
			t.Fatal("SyncSource() error = nil, want fetch error")
		}
		assertSourceStatus(t, st, ownerID, sourceID, "auth_error")
	})
}

func TestSyncAllSourcesOnceContinuesAcrossOwners(t *testing.T) {
	ctx := context.Background()
	st, cr, ownerOne := newCalendarSyncTestState(t)
	ownerTwo, err := st.CreateUser(ctx, "calendar-worker-two@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	// Make ONE source fail while staying inside the kind constraint: both are valid
	// `ics` sources, and the stub fails only the first one's URL. Manufacturing the
	// failure with an invalid kind is not possible -- the CHECK constraint rejects
	// the insert before the test can run.
	const failURL = "https://example.test/fail.ics"
	originalICS := fetchICS
	t.Cleanup(func() { fetchICS = originalICS })
	fetchICS = func(_ context.Context, _ *http.Client, url string) ([]calendar.NormalizedEvent, error) {
		if url == failURL {
			return nil, errors.New("fetch failed for this source")
		}
		return nil, nil
	}

	failingID := createCalendarSyncSource(t, st, cr, ownerOne, "ics", icsCreds{URL: failURL})
	successID := createCalendarSyncSource(t, st, cr, ownerTwo.ID, "ics", icsCreds{URL: "https://example.test/ok.ics"})
	syncAllSourcesOnce(ctx, st, cr, "", "", "", "")
	assertSourceStatus(t, st, ownerOne, failingID, "error")
	assertSourceStatus(t, st, ownerTwo.ID, successID, "ok")
}

func TestMarkSourceStatusSwallowsStoreError(t *testing.T) {
	st, _, _ := newCalendarSyncTestState(t)
	original := errors.New("original fetch failure")
	returned := func() error {
		markSourceStatus(context.Background(), st, "missing-source", "error")
		return original
	}()
	if !errors.Is(returned, original) {
		t.Fatalf("caller error = %v, want original error", returned)
	}
}

func newCalendarSyncTestState(t *testing.T) (*store.Store, *crypto.Crypto, string) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	owner, err := st.CreateUser(context.Background(), "calendar-worker@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := crypto.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return st, cr, owner.ID
}

func createCalendarSyncSource(t *testing.T, st *store.Store, cr *crypto.Crypto, ownerID, kind string, credentials any) string {
	t.Helper()
	plaintext, err := json.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cr.Seal(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	src, err := st.CreateSource(context.Background(), ownerID, kind, kind+" test", sealed)
	if err != nil {
		t.Fatal(err)
	}
	return src.ID
}

func assertSourceStatus(t *testing.T, st *store.Store, ownerID, sourceID, want string) {
	t.Helper()
	sources, err := st.ListSources(context.Background(), ownerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range sources {
		if src.ID == sourceID {
			if src.Status != want {
				t.Fatalf("source status = %q, want %q", src.Status, want)
			}
			return
		}
	}
	t.Fatalf("source %s not found", sourceID)
}
