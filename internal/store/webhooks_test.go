package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/google/uuid"
)

// seedWebhookRow inserts a webhook row directly via SQL (there is no
// registration-API store method yet — EXT01e — so tests write the row
// straight to the table, per the migration 0016 schema: id/owner_id/url/
// secret/enabled with created_at/updated_at defaults).
func seedWebhookRow(t *testing.T, st *store.Store, ownerID, url string, enabled bool) string {
	t.Helper()
	var id string
	err := st.Pool().QueryRow(context.Background(),
		`INSERT INTO webhooks (owner_id, url, secret, enabled) VALUES ($1,$2,$3,$4) RETURNING id`,
		ownerID, url, "shh-secret", enabled).Scan(&id)
	if err != nil {
		t.Fatalf("seed webhook: %v", err)
	}
	return id
}

func TestListEnabledWebhooksForOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	other, _ := st.CreateUser(ctx, "other@example.com", "h")

	enabledID := seedWebhookRow(t, st, owner.ID, "https://example.test/enabled", true)
	seedWebhookRow(t, st, owner.ID, "https://example.test/disabled", false)
	seedWebhookRow(t, st, other.ID, "https://example.test/other-owner", true)

	got, err := st.ListEnabledWebhooksForOwner(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListEnabledWebhooksForOwner: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d webhooks, want 1 (only the enabled one for this owner)", len(got))
	}
	if got[0].ID != enabledID || !got[0].Enabled || got[0].OwnerID != owner.ID {
		t.Fatalf("unexpected webhook: %+v", got[0])
	}

	// An owner with no webhooks at all gets an empty, non-nil slice.
	third, _ := st.CreateUser(ctx, "third@example.com", "h")
	none, err := st.ListEnabledWebhooksForOwner(ctx, third.ID)
	if err != nil {
		t.Fatalf("ListEnabledWebhooksForOwner (no webhooks): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("got %d webhooks, want 0", len(none))
	}
}

func TestCreateDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	whID := seedWebhookRow(t, st, owner.ID, "https://example.test/hook", true)

	payload, _ := json.Marshal(map[string]string{"event": "note.completed"})
	if err := st.CreateDelivery(ctx, whID, payload); err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}

	deliveries, err := st.ListPendingDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliveries))
	}
	d := deliveries[0]
	if d.WebhookID != whID {
		t.Fatalf("webhook_id = %q, want %q", d.WebhookID, whID)
	}
	if d.Status != model.DeliveryPending {
		t.Fatalf("status = %q, want %q", d.Status, model.DeliveryPending)
	}
	if d.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (DB default)", d.Attempts)
	}
	if d.MaxAttempts != 5 {
		t.Fatalf("max_attempts = %d, want 5 (DB default per migration 0016)", d.MaxAttempts)
	}
	var got map[string]string
	if err := json.Unmarshal(d.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["event"] != "note.completed" {
		t.Fatalf("payload = %+v, want event=note.completed", got)
	}
}

// seedDeliveryRow inserts a delivery row directly via SQL with an explicit
// status/attempts/last_error, for exercising RetryDelivery state transitions.
func seedDeliveryRow(t *testing.T, st *store.Store, webhookID, status string, attempts int, lastError *string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := st.Pool().QueryRow(context.Background(),
		`INSERT INTO webhook_deliveries (webhook_id, payload, status, attempts, last_error)
		 VALUES ($1, '{}'::jsonb, $2, $3, $4)
		 RETURNING id`,
		webhookID, status, attempts, lastError).Scan(&id)
	if err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	return id
}

// seedDeliveryRowAt is seedDeliveryRow with an explicit created_at, so tests
// asserting ORDER BY created_at DESC aren't at the mercy of clock resolution.
func seedDeliveryRowAt(t *testing.T, st *store.Store, webhookID, status string, createdAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := st.Pool().QueryRow(context.Background(),
		`INSERT INTO webhook_deliveries (webhook_id, payload, status, created_at, updated_at)
		 VALUES ($1, '{}'::jsonb, $2, $3, $3)
		 RETURNING id`,
		webhookID, status, createdAt).Scan(&id)
	if err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	return id
}

func TestListDeliveriesForOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	other, _ := st.CreateUser(ctx, "other@example.com", "h")

	whID := seedWebhookRow(t, st, owner.ID, "https://example.test/hook", true)
	otherWhID := seedWebhookRow(t, st, other.ID, "https://example.test/other-hook", true)

	base := testutil.NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)).Now()
	first := seedDeliveryRowAt(t, st, whID, model.DeliveryPending, base)
	second := seedDeliveryRowAt(t, st, whID, model.DeliveryFailed, base.Add(time.Minute))
	seedDeliveryRowAt(t, st, otherWhID, model.DeliveryPending, base.Add(2*time.Minute)) // belongs to a different owner

	got, err := st.ListDeliveriesForOwner(ctx, owner.ID, 10)
	if err != nil {
		t.Fatalf("ListDeliveriesForOwner: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d deliveries, want 2 (only this owner's)", len(got))
	}
	// Most-recent-first: second (created later) comes before first.
	if got[0].ID != second.String() || got[1].ID != first.String() {
		t.Fatalf("unexpected order: %+v", got)
	}

	// limit is respected.
	limited, err := st.ListDeliveriesForOwner(ctx, owner.ID, 1)
	if err != nil {
		t.Fatalf("ListDeliveriesForOwner (limit=1): %v", err)
	}
	if len(limited) != 1 || limited[0].ID != second.String() {
		t.Fatalf("unexpected limited result: %+v", limited)
	}

	// An owner with no deliveries at all gets an empty, non-nil slice.
	third, _ := st.CreateUser(ctx, "third@example.com", "h")
	none, err := st.ListDeliveriesForOwner(ctx, third.ID, 10)
	if err != nil {
		t.Fatalf("ListDeliveriesForOwner (no deliveries): %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("got %+v, want empty non-nil slice", none)
	}
}

func TestRetryDeliveryDeliveredIsIdempotentNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	whID := seedWebhookRow(t, st, owner.ID, "https://example.test/hook", true)
	last := "some old error"
	id := seedDeliveryRow(t, st, whID, model.DeliveryDelivered, 3, &last)

	status, err := st.RetryDelivery(ctx, owner.ID, id)
	if err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	if status != model.DeliveryDelivered {
		t.Fatalf("status = %q, want %q", status, model.DeliveryDelivered)
	}

	// Row is unchanged.
	var gotStatus, gotLastError string
	var gotAttempts int
	if err := st.Pool().QueryRow(ctx,
		`SELECT status, attempts, COALESCE(last_error,'') FROM webhook_deliveries WHERE id=$1`,
		id).Scan(&gotStatus, &gotAttempts, &gotLastError); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if gotStatus != model.DeliveryDelivered || gotAttempts != 3 || gotLastError != last {
		t.Fatalf("row changed: status=%q attempts=%d last_error=%q", gotStatus, gotAttempts, gotLastError)
	}
}

func TestRetryDeliveryFailedRequeues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	whID := seedWebhookRow(t, st, owner.ID, "https://example.test/hook", true)
	last := "connection refused"
	id := seedDeliveryRow(t, st, whID, model.DeliveryFailed, 5, &last)

	status, err := st.RetryDelivery(ctx, owner.ID, id)
	if err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	if status != model.DeliveryPending {
		t.Fatalf("status = %q, want %q", status, model.DeliveryPending)
	}

	var gotStatus, gotLastError string
	var gotAttempts int
	var gotNextAttempt *string
	if err := st.Pool().QueryRow(ctx,
		`SELECT status, attempts, COALESCE(last_error,''), next_attempt_at::text FROM webhook_deliveries WHERE id=$1`,
		id).Scan(&gotStatus, &gotAttempts, &gotLastError, &gotNextAttempt); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if gotStatus != model.DeliveryPending {
		t.Fatalf("status = %q, want pending", gotStatus)
	}
	if gotAttempts != 0 {
		t.Fatalf("attempts = %d, want 0 (reset)", gotAttempts)
	}
	if gotLastError != "" {
		t.Fatalf("last_error = %q, want cleared", gotLastError)
	}
	if gotNextAttempt != nil {
		t.Fatalf("next_attempt_at = %v, want NULL", *gotNextAttempt)
	}
}

func TestRetryDeliveryPendingOrInFlightIsInvalidState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	whID := seedWebhookRow(t, st, owner.ID, "https://example.test/hook", true)

	pendingID := seedDeliveryRow(t, st, whID, model.DeliveryPending, 0, nil)
	if _, err := st.RetryDelivery(ctx, owner.ID, pendingID); !errors.Is(err, store.ErrInvalidState) {
		t.Fatalf("pending: err = %v, want ErrInvalidState", err)
	}

	inFlightID := seedDeliveryRow(t, st, whID, model.DeliveryInFlight, 1, nil)
	if _, err := st.RetryDelivery(ctx, owner.ID, inFlightID); !errors.Is(err, store.ErrInvalidState) {
		t.Fatalf("in_flight: err = %v, want ErrInvalidState", err)
	}
}

func TestRetryDeliveryNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")

	if _, err := st.RetryDelivery(ctx, owner.ID, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRetryDeliveryWrongOwnerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, _ := st.CreateUser(ctx, "owner@example.com", "h")
	intruder, _ := st.CreateUser(ctx, "intruder@example.com", "h")
	whID := seedWebhookRow(t, st, owner.ID, "https://example.test/hook", true)
	id := seedDeliveryRow(t, st, whID, model.DeliveryFailed, 5, nil)

	if _, err := st.RetryDelivery(ctx, intruder.ID, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (not leaking another owner's delivery)", err)
	}

	// The delivery itself must be unaffected by the failed cross-owner attempt.
	var gotStatus string
	if err := st.Pool().QueryRow(ctx, `SELECT status FROM webhook_deliveries WHERE id=$1`, id).Scan(&gotStatus); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if gotStatus != model.DeliveryFailed {
		t.Fatalf("status = %q, want unchanged failed", gotStatus)
	}
}
