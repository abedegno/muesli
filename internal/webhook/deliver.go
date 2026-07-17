package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
)

// webhookStore is the subset of store.Store methods used by DeliveryWorker.
// Defining it here keeps the worker testable without a real DB.
type webhookStore interface {
	ListPendingDeliveries(ctx context.Context, limit int) ([]model.WebhookDelivery, error)
	ClaimDelivery(ctx context.Context, id uuid.UUID) error
	MarkDelivered(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, lastErr string, nextAttempt *time.Time) error
	ResetInflightDeliveries(ctx context.Context) error
	GetDeliveryWebhook(ctx context.Context, deliveryID uuid.UUID) (model.Webhook, error)
}

// DeliveryWorker polls for pending webhook deliveries and dispatches them
// with exponential backoff retry, guarded against SSRF via a custom transport
// that re-resolves and validates DNS at connect time.
type DeliveryWorker struct {
	store        webhookStore
	pollInterval time.Duration
	client       *http.Client
	// validateURL is the URL-safety check applied before each HTTP call.
	// Defaults to ValidateWebhookURL; override in tests to bypass SSRF checks
	// against httptest servers.
	validateURL func(rawURL string) error
}

// NewDeliveryWorker creates a DeliveryWorker that polls every pollInterval.
// Use 5*time.Second for production; smaller values are fine for tests.
func NewDeliveryWorker(store webhookStore, pollInterval time.Duration) *DeliveryWorker {
	return &DeliveryWorker{
		store:        store,
		pollInterval: pollInterval,
		client:       newGuardedClient(),
		validateURL:  ValidateWebhookURL,
	}
}

// Run recovers any in-flight deliveries left from a previous crash, then
// polls for pending work until ctx is cancelled.
func (w *DeliveryWorker) Run(ctx context.Context) error {
	// Recover in-flight deliveries from a prior crash before entering the loop.
	if err := w.store.ResetInflightDeliveries(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(ctx, "webhook worker: reset in-flight deliveries", "error", err)
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.poll(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "webhook worker: poll error", "error", err)
			}
		}
	}
}

// poll fetches up to 10 pending deliveries and processes each one.
func (w *DeliveryWorker) poll(ctx context.Context) error {
	deliveries, err := w.store.ListPendingDeliveries(ctx, 10)
	if err != nil {
		return fmt.Errorf("list pending deliveries: %w", err)
	}
	for _, d := range deliveries {
		w.dispatch(ctx, d)
	}
	return nil
}

// dispatch attempts a single webhook delivery.
func (w *DeliveryWorker) dispatch(ctx context.Context, d model.WebhookDelivery) {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		slog.ErrorContext(ctx, "webhook worker: invalid delivery ID", "id", d.ID, "error", err)
		return
	}

	// 1. Claim the delivery to prevent other workers from duplicating it.
	if err := w.store.ClaimDelivery(ctx, id); err != nil {
		// Another worker may have already claimed it; not fatal.
		slog.WarnContext(ctx, "webhook worker: claim delivery failed", "id", id, "error", err)
		return
	}

	// 2. Load the parent webhook for its URL and configuration.
	wh, err := w.store.GetDeliveryWebhook(ctx, id)
	if err != nil {
		w.fail(ctx, id, d, fmt.Sprintf("get webhook: %v", err))
		return
	}

	// 3. Re-validate the URL (guards against DNS rebinding and stale config).
	if err := w.validateURL(wh.URL); err != nil {
		slog.WarnContext(ctx, "webhook worker: SSRF check failed, failing delivery",
			"id", id, "url", wh.URL, "error", err)
		w.giveUp(ctx, id, d, fmt.Sprintf("ssrf validation: %v", err))
		return
	}

	// 4. Build and send the HTTP POST.
	payload := json.RawMessage(d.Payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(payload))
	if err != nil {
		w.fail(ctx, id, d, fmt.Sprintf("build request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "muesli-webhooks/1")
	if wh.Secret != "" {
		req.Header.Set("X-Muesli-Signature", signPayload(wh.Secret, d.Payload))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		w.fail(ctx, id, d, fmt.Sprintf("http: %v", err))
		return
	}
	resp.Body.Close()

	// 5. Interpret the response.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if mErr := w.store.MarkDelivered(ctx, id); mErr != nil {
			slog.ErrorContext(ctx, "webhook worker: mark delivered", "id", id, "error", mErr)
		} else {
			slog.InfoContext(ctx, "webhook worker: delivered",
				"id", id, "status", resp.StatusCode, "attempts", d.Attempts+1)
		}
		return
	}

	w.fail(ctx, id, d, fmt.Sprintf("non-2xx response: %d", resp.StatusCode))
}

// fail handles a transient failure: requeue with backoff or give up.
func (w *DeliveryWorker) fail(ctx context.Context, id uuid.UUID, d model.WebhookDelivery, errStr string) {
	nextAttempts := d.Attempts + 1
	if nextAttempts >= d.MaxAttempts {
		w.giveUp(ctx, id, d, errStr)
		return
	}
	// Exponential backoff: base 5s, 2× per attempt, cap 1h.
	backoff := time.Duration(5<<uint(d.Attempts)) * time.Second
	const maxBackoff = time.Hour
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	nextTime := time.Now().Add(backoff)
	if err := w.store.MarkFailed(ctx, id, errStr, &nextTime); err != nil {
		slog.ErrorContext(ctx, "webhook worker: requeue delivery", "id", id, "error", err)
		return
	}
	slog.WarnContext(ctx, "webhook worker: delivery failed, will retry",
		"id", id, "attempts", nextAttempts, "next", nextTime, "error", errStr)
}

// giveUp permanently fails a delivery (no future retries).
func (w *DeliveryWorker) giveUp(ctx context.Context, id uuid.UUID, d model.WebhookDelivery, errStr string) {
	if err := w.store.MarkFailed(ctx, id, errStr, nil); err != nil {
		slog.ErrorContext(ctx, "webhook worker: mark failed", "id", id, "error", err)
		return
	}
	slog.ErrorContext(ctx, "webhook worker: delivery permanently failed",
		"id", id, "attempts", d.Attempts+1, "error", errStr)
}

// signPayload returns the outbound webhook signature for the exact bytes posted.
// The scheme is HMAC-SHA256 keyed by the per-webhook secret, hex-encoded and
// prefixed with "sha256=" so downstream consumers can verify the payload.
func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// newGuardedClient returns an http.Client whose transport re-resolves DNS at
// connect time and validates each resolved IP against IsPrivateIP, preventing
// SSRF via DNS rebinding. The first safe IP is dialled directly so the OS
// never performs a second DNS lookup.
func newGuardedClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: newGuardedTransport(),
	}
}

func newGuardedTransport() *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("webhook transport: split host/port %q: %w", addr, err)
			}

			// Re-resolve at connect time (prevents DNS rebinding window after pre-validation).
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("webhook transport: DNS lookup %q: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("webhook transport: DNS lookup %q returned no addresses", host)
			}

			// Validate every resolved IP; find first safe one to dial.
			var safeIP string
			for _, raw := range ips {
				ip := net.ParseIP(raw)
				if ip == nil {
					continue
				}
				if IsPrivateIP(ip) {
					return nil, fmt.Errorf("webhook transport: %q resolved to private/reserved IP %s", host, ip)
				}
				if safeIP == "" {
					safeIP = raw
				}
			}
			if safeIP == "" {
				return nil, fmt.Errorf("webhook transport: no safe IP found for %q", host)
			}

			// Dial the IP directly so the OS never re-resolves DNS a second time.
			dialer := &net.Dialer{}
			return dialer.DialContext(ctx, network, net.JoinHostPort(safeIP, port))
		},
	}
}
