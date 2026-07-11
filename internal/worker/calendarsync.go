package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/store"
)

// calendarSyncInterval is how often the background calendar scheduler
// re-syncs every configured source.
const calendarSyncInterval = 15 * time.Minute

// calendarSyncWindow is how far back and forward SyncSource fetches events.
const (
	calendarSyncLookback  = 24 * time.Hour
	calendarSyncLookahead = 14 * 24 * time.Hour
)

type icsCreds struct {
	URL string `json:"url"`
}

type caldavCreds struct {
	URL  string `json:"url"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

type googleCreds struct {
	RefreshToken string `json:"refresh_token"`
}

type microsoftCreds struct {
	RefreshToken string `json:"refresh_token"`
}

// SyncSource fetches upstream events for a single calendar source, upserts
// them, prunes anything no longer present upstream, and records the
// resulting sync status on the source row.
func SyncSource(ctx context.Context, st *store.Store, cr *crypto.Crypto, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret, sourceID string) error {
	kind, sealedCreds, ownerID, err := st.GetSourceCreds(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("sync source %s: %w", sourceID, err)
	}

	plaintext, err := cr.Open(sealedCreds)
	if err != nil {
		markSourceStatus(ctx, st, sourceID, "auth_error")
		return fmt.Errorf("sync source %s: %w", sourceID, err)
	}

	from := time.Now().Add(-calendarSyncLookback)
	to := time.Now().Add(calendarSyncLookahead)

	var events []calendar.NormalizedEvent
	var selected map[string]bool
	if kind == "google" || kind == "microsoft" {
		selected, err = st.GetSourceSelectedCalendars(ctx, sourceID)
		if err != nil {
			status := "error"
			if isAuthError(err) {
				status = "auth_error"
			}
			markSourceStatus(ctx, st, sourceID, status)
			return fmt.Errorf("sync source %s: load selected calendars: %w", sourceID, err)
		}
	}
	switch kind {
	case "ics":
		var creds icsCreds
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			markSourceStatus(ctx, st, sourceID, "error")
			return fmt.Errorf("sync source %s: decode ics credentials: %w", sourceID, err)
		}
		events, err = calendar.FetchICS(ctx, http.DefaultClient, creds.URL)
	case "caldav":
		var creds caldavCreds
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			markSourceStatus(ctx, st, sourceID, "error")
			return fmt.Errorf("sync source %s: decode caldav credentials: %w", sourceID, err)
		}
		events, err = calendar.FetchCalDAV(ctx, creds.URL, creds.User, creds.Pass, from, to)
	case "google":
		var creds googleCreds
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			markSourceStatus(ctx, st, sourceID, "error")
			return fmt.Errorf("sync source %s: decode google credentials: %w", sourceID, err)
		}
		events, err = calendar.FetchGoogle(ctx, googleClientID, googleClientSecret, creds.RefreshToken, selected, from, to)
	case "microsoft":
		var creds microsoftCreds
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			markSourceStatus(ctx, st, sourceID, "error")
			return fmt.Errorf("sync source %s: decode microsoft credentials: %w", sourceID, err)
		}
		events, err = calendar.FetchMicrosoft(ctx, microsoftClientID, microsoftClientSecret, creds.RefreshToken, selected, from, to)
	default:
		markSourceStatus(ctx, st, sourceID, "error")
		return fmt.Errorf("sync source %s: unknown source kind %q", sourceID, kind)
	}
	if err != nil {
		status := "error"
		if isAuthError(err) {
			status = "auth_error"
		}
		markSourceStatus(ctx, st, sourceID, status)
		return fmt.Errorf("sync source %s: %w", sourceID, err)
	}

	if err := st.UpsertEvents(ctx, ownerID, sourceID, events); err != nil {
		return fmt.Errorf("sync source %s: upsert events: %w", sourceID, err)
	}
	if err := st.PruneEvents(ctx, sourceID, calendar.DiffExternalIDs(events)); err != nil {
		return fmt.Errorf("sync source %s: prune events: %w", sourceID, err)
	}

	if err := st.UpdateSourceStatus(ctx, sourceID, "ok", time.Now()); err != nil {
		return fmt.Errorf("sync source %s: update status: %w", sourceID, err)
	}
	return nil
}

// markSourceStatus records a sync failure status, swallowing its own error:
// the caller already has a more specific error to return and a failure to
// persist the status must not shadow it.
func markSourceStatus(ctx context.Context, st *store.Store, sourceID, status string) {
	if err := st.UpdateSourceStatus(ctx, sourceID, status, time.Now()); err != nil {
		slog.ErrorContext(ctx, "calendar sync: update status", "source_id", sourceID, "status", status, "error", err)
	}
}

// isAuthError applies a documented heuristic to classify a fetch error as an
// auth failure: the ICS/CalDAV fetchers wrap the upstream HTTP status text
// (e.g. "401 Unauthorized", "403 Forbidden") rather than exposing a typed
// status, so we look for those markers in the wrapped error chain. Anything
// else (network errors, parse errors, ambiguous CalDAV discovery failures)
// defaults to the generic "error" status rather than over-fitting this check.
func isAuthError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "invalid_client") ||
		strings.Contains(msg, "unauthorized_client")
}

// syncAllSourcesOnce syncs every calendar source across all owners once,
// logging and swallowing each source's error so one bad source never blocks
// or crashes the rest.
func syncAllSourcesOnce(ctx context.Context, st *store.Store, cr *crypto.Crypto, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret string) {
	ids, err := st.ListAllSourceIDs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "calendar sync: list sources", "error", err)
		return
	}
	for _, id := range ids {
		if err := SyncSource(ctx, st, cr, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret, id); err != nil {
			slog.ErrorContext(ctx, "calendar sync", "source_id", id, "error", err)
		}
	}
}

// StartCalendarScheduler runs a calendar sync pass immediately and then every
// calendarSyncInterval until ctx is cancelled.
func StartCalendarScheduler(ctx context.Context, st *store.Store, cr *crypto.Crypto, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret string) {
	syncAllSourcesOnce(ctx, st, cr, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret)
	t := time.NewTicker(calendarSyncInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			syncAllSourcesOnce(ctx, st, cr, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret)
		}
	}
}
