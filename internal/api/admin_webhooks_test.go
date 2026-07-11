package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newWebhookDeliveriesTestServer returns a server, store, and raw pool for
// seeding webhook/delivery rows directly via SQL.
func newWebhookDeliveriesTestServer(t *testing.T) (*api.Server, *store.Store, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	return api.NewServer(api.Deps{Store: st}), st, pool
}

// seedWebhookAndDelivery seeds a webhook owned by ownerID and one delivery
// for it with the given status, returning the delivery id.
func seedWebhookAndDelivery(t *testing.T, pool *pgxpool.Pool, ownerID, status string) string {
	t.Helper()
	ctx := context.Background()
	var whID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO webhooks (owner_id, url, secret, enabled) VALUES ($1,$2,$3,true) RETURNING id`,
		ownerID, "https://example.test/hook", "shh").Scan(&whID); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}
	var deliveryID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, payload, status) VALUES ($1,'{}'::jsonb,$2) RETURNING id`,
		whID, status).Scan(&deliveryID); err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	return deliveryID
}

// createSecondUser creates an additional user directly via the store (not
// through /api/setup, which only allows the first account) with a known
// password so the test can log in as them.
func createSecondUser(t *testing.T, st *store.Store, email, password string) string {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u, err := st.CreateUser(context.Background(), email, hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

func TestListWebhookDeliveriesOwnerScoped(t *testing.T) {
	srv, st, pool := newWebhookDeliveriesTestServer(t)
	hdr := setupLoginHdr(t, srv, "owner@example.com")

	// The setup account's id — needed to seed a delivery it owns.
	u, err := st.GetUserByEmail(context.Background(), "owner@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	mine := seedWebhookAndDelivery(t, pool, u.ID, model.DeliveryFailed)

	otherID := createSecondUser(t, st, "other@example.com", "password123")
	seedWebhookAndDelivery(t, pool, otherID, model.DeliveryFailed)

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/webhook-deliveries", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var deliveries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &deliveries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0]["id"] != mine {
		t.Fatalf("expected only the caller's own delivery, got: %s", rec.Body)
	}
}

func TestRetryWebhookDeliveryFailedReturns202AndQueues(t *testing.T) {
	srv, st, pool := newWebhookDeliveriesTestServer(t)
	hdr := setupLoginHdr(t, srv, "owner2@example.com")
	u, err := st.GetUserByEmail(context.Background(), "owner2@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	id := seedWebhookAndDelivery(t, pool, u.ID, model.DeliveryFailed)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/webhook-deliveries/"+id+"/retry", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "queued" {
		t.Fatalf("unexpected response: %s", rec.Body)
	}

	// The row itself flips to pending.
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM webhook_deliveries WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if status != model.DeliveryPending {
		t.Fatalf("status = %q, want pending", status)
	}
}

func TestRetryWebhookDeliveryDeliveredReturns200Idempotent(t *testing.T) {
	srv, st, pool := newWebhookDeliveriesTestServer(t)
	hdr := setupLoginHdr(t, srv, "owner3@example.com")
	u, err := st.GetUserByEmail(context.Background(), "owner3@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	id := seedWebhookAndDelivery(t, pool, u.ID, model.DeliveryDelivered)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/webhook-deliveries/"+id+"/retry", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "already_delivered" {
		t.Fatalf("unexpected response: %s", rec.Body)
	}
}

func TestRetryWebhookDeliveryWrongOwnerReturns404(t *testing.T) {
	srv, st, pool := newWebhookDeliveriesTestServer(t)
	hdr := setupLoginHdr(t, srv, "owner4@example.com")

	otherID := createSecondUser(t, st, "other4@example.com", "password123")
	id := seedWebhookAndDelivery(t, pool, otherID, model.DeliveryFailed)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/webhook-deliveries/"+id+"/retry", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body %s", rec.Code, rec.Body)
	}
}

func TestRetryWebhookDeliveryPendingOrInFlightReturns409(t *testing.T) {
	srv, st, pool := newWebhookDeliveriesTestServer(t)
	hdr := setupLoginHdr(t, srv, "owner5@example.com")
	u, err := st.GetUserByEmail(context.Background(), "owner5@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	pendingID := seedWebhookAndDelivery(t, pool, u.ID, model.DeliveryPending)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/webhook-deliveries/"+pendingID+"/retry", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body %s", rec.Code, rec.Body)
	}

	inFlightID := seedWebhookAndDelivery(t, pool, u.ID, model.DeliveryInFlight)
	rec = doJSON(t, srv, http.MethodPost, "/api/admin/webhook-deliveries/"+inFlightID+"/retry", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body %s", rec.Code, rec.Body)
	}
}
