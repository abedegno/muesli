package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
)

// --------------------------------------------------------------------------
// In-memory test double for webhookStore
// --------------------------------------------------------------------------

type memStore struct {
	mu          sync.Mutex
	webhooks    map[string]model.Webhook
	deliveries  map[string]model.WebhookDelivery
	resetCalled int
}

func newMemStore() *memStore {
	return &memStore{
		webhooks:   make(map[string]model.Webhook),
		deliveries: make(map[string]model.WebhookDelivery),
	}
}

func (m *memStore) addWebhook(wh model.Webhook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.webhooks[wh.ID] = wh
}

func (m *memStore) addDelivery(d model.WebhookDelivery) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveries[d.ID] = d
}

func (m *memStore) getDelivery(id string) (model.WebhookDelivery, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[id]
	return d, ok
}

func (m *memStore) ListPendingDeliveries(_ context.Context, limit int) ([]model.WebhookDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.WebhookDelivery
	for _, d := range m.deliveries {
		if d.Status == model.DeliveryPending {
			out = append(out, d)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *memStore) ClaimDelivery(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[id.String()]
	if !ok {
		return errNotFound
	}
	d.Status = model.DeliveryInFlight
	m.deliveries[id.String()] = d
	return nil
}

func (m *memStore) MarkDelivered(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[id.String()]
	if !ok {
		return errNotFound
	}
	d.Status = model.DeliveryDelivered
	d.Attempts++
	m.deliveries[id.String()] = d
	return nil
}

func (m *memStore) MarkFailed(_ context.Context, id uuid.UUID, lastErr string, nextAttempt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[id.String()]
	if !ok {
		return errNotFound
	}
	d.Attempts++
	d.LastError = lastErr
	if nextAttempt == nil {
		d.Status = model.DeliveryFailed
		d.NextAttemptAt = nil
	} else {
		// In the in-memory store, re-queue immediately (ignore time; the real
		// DB enforces next_attempt_at <= NOW(), but the memStore ListPendingDeliveries
		// ignores the time check so tests don't need wall-clock delays).
		d.Status = model.DeliveryPending
		d.NextAttemptAt = nextAttempt
	}
	m.deliveries[id.String()] = d
	return nil
}

func (m *memStore) ResetInflightDeliveries(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetCalled++
	for id, d := range m.deliveries {
		if d.Status == model.DeliveryInFlight {
			d.Status = model.DeliveryPending
			m.deliveries[id] = d
		}
	}
	return nil
}

func (m *memStore) GetDeliveryWebhook(_ context.Context, deliveryID uuid.UUID) (model.Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[deliveryID.String()]
	if !ok {
		return model.Webhook{}, errNotFound
	}
	wh, ok := m.webhooks[d.WebhookID]
	if !ok {
		return model.Webhook{}, errNotFound
	}
	return wh, nil
}

// errNotFound is a sentinel used by the in-memory store.
var errNotFound = &storeErr{"not found"}

type storeErr struct{ msg string }

func (e *storeErr) Error() string { return e.msg }

// --------------------------------------------------------------------------
// Helper: build a delivery with a fresh UUID.
// --------------------------------------------------------------------------

func makeDelivery(webhookID string, maxAttempts int) model.WebhookDelivery {
	payload, _ := json.Marshal(map[string]string{"event": "test"})
	return model.WebhookDelivery{
		ID:          uuid.New().String(),
		WebhookID:   webhookID,
		Payload:     payload,
		Status:      model.DeliveryPending,
		Attempts:    0,
		MaxAttempts: maxAttempts,
	}
}

// newTestWorker creates a DeliveryWorker wired for tests:
//   - Very short poll interval so tests complete quickly.
//   - URL validation is bypassed (httptest.NewServer uses 127.0.0.1 which the
//     real ValidateWebhookURL correctly rejects; tests cover the validate path
//     separately via validate_test.go and TestGiveUpOnSSRF).
//   - A plain http.Client is used instead of the guarded transport so the
//     worker can actually POST to the loopback httptest server.
func newTestWorker(st webhookStore) *DeliveryWorker {
	return &DeliveryWorker{
		store:        st,
		pollInterval: 5 * time.Millisecond,
		client:       &http.Client{Timeout: 5 * time.Second},
		validateURL:  func(_ string) error { return nil }, // bypass for localhost
	}
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestSignPayload(t *testing.T) {
	const want = "sha256=9d16f6d63cf01c65ee5bc82fd3b8dc3f0ee9398963696447b4e31b8fe78ca50a"
	const secret = "topsecret"
	body := []byte(`{"event":"test"}`)

	got := signPayload(secret, body)
	if got != want {
		t.Fatalf("signPayload() = %q, want %q", got, want)
	}
	if again := signPayload(secret, body); again != got {
		t.Fatalf("signPayload() is not deterministic: first %q, second %q", got, again)
	}
	if changed := signPayload("different", body); changed == got {
		t.Fatalf("signPayload() did not change when secret changed: %q", changed)
	}
	if changed := signPayload(secret, []byte(`{"event":"other"}`)); changed == got {
		t.Fatalf("signPayload() did not change when body changed: %q", changed)
	}
	if empty := signPayload("", body); empty == "" {
		t.Fatal("signPayload() with empty secret returned an empty string")
	}
}

// TestDeliverySuccess verifies a single successful delivery:
// the server returns 200, the delivery is marked delivered after 1 attempt.
func TestDeliverySuccess(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st := newMemStore()
	wh := model.Webhook{ID: uuid.New().String(), URL: srv.URL + "/hook", Enabled: true}
	st.addWebhook(wh)
	d := makeDelivery(wh.ID, 5)
	st.addDelivery(d)

	worker := newTestWorker(st)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()

	// Wait until the delivery is marked delivered.
	for {
		got, ok := st.getDelivery(d.ID)
		if ok && got.Status == model.DeliveryDelivered {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for delivery to be marked delivered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done

	got, _ := st.getDelivery(d.ID)
	if got.Status != model.DeliveryDelivered {
		t.Fatalf("status = %q, want %q", got.Status, model.DeliveryDelivered)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.Attempts)
	}
	if hits.Load() != 1 {
		t.Fatalf("server hit count = %d, want 1", hits.Load())
	}
}

func TestDeliverySignsPayload(t *testing.T) {
	var hits atomic.Int32
	var gotBody []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotBody = append([]byte(nil), body...)
		gotSig = r.Header.Get("X-Muesli-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st := newMemStore()
	secret := "topsecret"
	wh := model.Webhook{ID: uuid.New().String(), URL: srv.URL + "/hook", Secret: secret, Enabled: true}
	st.addWebhook(wh)
	d := makeDelivery(wh.ID, 5)
	st.addDelivery(d)

	worker := newTestWorker(st)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()

	for {
		got, ok := st.getDelivery(d.ID)
		if ok && got.Status == model.DeliveryDelivered {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for signed delivery to be marked delivered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if hits.Load() != 1 {
		t.Fatalf("server hit count = %d, want 1", hits.Load())
	}
	if want := signPayload(secret, gotBody); gotSig != want {
		t.Fatalf("signature header = %q, want %q", gotSig, want)
	}
}

func TestDeliverySkipsEmptySecretSignature(t *testing.T) {
	var hits atomic.Int32
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if body, err := io.ReadAll(r.Body); err != nil {
			t.Fatalf("read request body: %v", err)
		} else if !bytes.Equal(body, []byte(`{"event":"test"}`)) {
			t.Fatalf("request body = %q, want %q", body, []byte(`{"event":"test"}`))
		}
		gotSig = r.Header.Get("X-Muesli-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st := newMemStore()
	wh := model.Webhook{ID: uuid.New().String(), URL: srv.URL + "/hook", Enabled: true}
	st.addWebhook(wh)
	d := makeDelivery(wh.ID, 5)
	st.addDelivery(d)

	worker := newTestWorker(st)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()

	for {
		got, ok := st.getDelivery(d.ID)
		if ok && got.Status == model.DeliveryDelivered {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for delivery with empty secret to be marked delivered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if hits.Load() != 1 {
		t.Fatalf("server hit count = %d, want 1", hits.Load())
	}
	if gotSig != "" {
		t.Fatalf("signature header = %q, want empty string", gotSig)
	}
}

// TestRetryThenSuccess verifies that a 500 response causes a pending requeue,
// and a subsequent poll that gets 200 marks the delivery delivered after 2 attempts.
func TestRetryThenSuccess(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st := newMemStore()
	wh := model.Webhook{ID: uuid.New().String(), URL: srv.URL + "/hook", Enabled: true}
	st.addWebhook(wh)
	d := makeDelivery(wh.ID, 5)
	st.addDelivery(d)

	worker := newTestWorker(st)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()

	// Wait until the delivery is marked delivered.
	for {
		got, ok := st.getDelivery(d.ID)
		if ok && got.Status == model.DeliveryDelivered {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for delivery to be marked delivered after retry")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done

	got, _ := st.getDelivery(d.ID)
	if got.Status != model.DeliveryDelivered {
		t.Fatalf("status = %q, want %q", got.Status, model.DeliveryDelivered)
	}
	if got.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", got.Attempts)
	}
	if hits.Load() != 2 {
		t.Fatalf("server hit count = %d, want 2", hits.Load())
	}
}

// TestGiveUp verifies that after max_attempts failures the delivery is
// permanently marked failed with nil next_attempt_at.
func TestGiveUp(t *testing.T) {
	const maxAttempts = 3
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	st := newMemStore()
	wh := model.Webhook{ID: uuid.New().String(), URL: srv.URL + "/hook", Enabled: true}
	st.addWebhook(wh)
	d := makeDelivery(wh.ID, maxAttempts)
	st.addDelivery(d)

	worker := newTestWorker(st)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()

	// Wait until the delivery is permanently failed.
	for {
		got, ok := st.getDelivery(d.ID)
		if ok && got.Status == model.DeliveryFailed {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for delivery to be permanently failed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done

	got, _ := st.getDelivery(d.ID)
	if got.Status != model.DeliveryFailed {
		t.Fatalf("status = %q, want %q", got.Status, model.DeliveryFailed)
	}
	if got.NextAttemptAt != nil {
		t.Fatalf("next_attempt_at = %v, want nil (give up)", got.NextAttemptAt)
	}
	if got.Attempts != maxAttempts {
		t.Fatalf("attempts = %d, want %d", got.Attempts, maxAttempts)
	}
}
