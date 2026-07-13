package worker_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/digest"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

func setNoteStartedAt(t *testing.T, st *store.Store, noteID string, startedAt time.Time) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE notes SET started_at=$1, updated_at=$1 WHERE id=$2`,
		startedAt, noteID); err != nil {
		t.Fatalf("set note started_at: %v", err)
	}
}

func setActionItemCreatedAt(t *testing.T, st *store.Store, itemID string, createdAt time.Time) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE action_items SET created_at=$1 WHERE id=$2`,
		createdAt, itemID); err != nil {
		t.Fatalf("set action item created_at: %v", err)
	}
}

func seedDigestWebhookRow(t *testing.T, st *store.Store, ownerID, url string, enabled bool) string {
	t.Helper()
	var id string
	if err := st.Pool().QueryRow(context.Background(),
		`INSERT INTO webhooks (owner_id, url, secret, enabled) VALUES ($1,$2,$3,$4) RETURNING id`,
		ownerID, url, "shh-secret", enabled).Scan(&id); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}
	return id
}

func TestSendDigestEnqueuesWebhookDelivery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, err := st.CreateUser(ctx, "owner@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	whID := seedDigestWebhookRow(t, st, owner.ID, "https://example.test/digest", true)

	note, err := st.CreateNote(ctx, owner.ID, "Weekly sync")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(ctx, owner.ID, note.ID, []model.ActionItem{
		{Text: "Ship digest", DueHint: "today"},
	}, nil); err != nil {
		t.Fatalf("ReplaceActionItemsForNote: %v", err)
	}

	anchor := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	setNoteStartedAt(t, st, note.ID, anchor)

	from := anchor.Add(-2 * time.Hour)
	to := anchor.Add(2 * time.Hour)
	if err := worker.SendDigest(ctx, st, owner.ID, from, to); err != nil {
		t.Fatalf("SendDigest: %v", err)
	}

	deliveries, err := st.ListDeliveriesForOwner(ctx, owner.ID, 10)
	if err != nil {
		t.Fatalf("ListDeliveriesForOwner: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliveries))
	}
	got := deliveries[0]
	if got.WebhookID != whID {
		t.Fatalf("webhook_id = %q, want %q", got.WebhookID, whID)
	}

	var payload struct {
		Event           string             `json:"event"`
		OwnerID         string             `json:"owner_id"`
		WindowFrom      time.Time          `json:"window_from"`
		WindowTo        time.Time          `json:"window_to"`
		RecentMeetings  []model.Note       `json:"recent_meetings"`
		OpenActionItems []model.ActionItem `json:"open_action_items"`
		Text            string             `json:"text"`
	}
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Event != "digest.summary" {
		t.Fatalf("event = %q, want digest.summary", payload.Event)
	}
	if payload.OwnerID != owner.ID {
		t.Fatalf("owner_id = %q, want %q", payload.OwnerID, owner.ID)
	}
	if len(payload.RecentMeetings) != 1 || payload.RecentMeetings[0].ID != note.ID {
		t.Fatalf("recent_meetings = %+v, want one entry for note %q", payload.RecentMeetings, note.ID)
	}
	if len(payload.OpenActionItems) != 1 || payload.OpenActionItems[0].Text != "Ship digest" {
		t.Fatalf("open_action_items = %+v, want one open item", payload.OpenActionItems)
	}
	if !strings.Contains(payload.Text, "Weekly sync") {
		t.Fatalf("text = %q, want rendered digest content", payload.Text)
	}
}

func TestSendDigestIncludesNeedsFollowUpItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, err := st.CreateUser(ctx, "owner@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	seedDigestWebhookRow(t, st, owner.ID, "https://example.test/digest", true)

	note, err := st.CreateNote(ctx, owner.ID, "Weekly sync")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(ctx, owner.ID, note.ID, []model.ActionItem{
		{Text: "Stale item"},
		{Text: "Fresh item"},
	}, nil); err != nil {
		t.Fatalf("ReplaceActionItemsForNote: %v", err)
	}

	items, err := st.ListForOwner(ctx, owner.ID, model.ActionItemOpen)
	if err != nil {
		t.Fatalf("ListForOwner: %v", err)
	}
	var staleID, freshID string
	for _, item := range items {
		switch item.Text {
		case "Stale item":
			staleID = item.ID
		case "Fresh item":
			freshID = item.ID
		}
	}
	if staleID == "" || freshID == "" {
		t.Fatalf("seeded action items not found: %+v", items)
	}

	anchor := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
	setNoteStartedAt(t, st, note.ID, anchor)
	setActionItemCreatedAt(t, st, staleID, anchor.Add(-digest.FollowUpThreshold-time.Hour))
	setActionItemCreatedAt(t, st, freshID, anchor.Add(-time.Hour))

	from := anchor.Add(-2 * time.Hour)
	to := anchor.Add(2 * time.Hour)
	if err := worker.SendDigest(ctx, st, owner.ID, from, to); err != nil {
		t.Fatalf("SendDigest: %v", err)
	}

	deliveries, err := st.ListDeliveriesForOwner(ctx, owner.ID, 10)
	if err != nil {
		t.Fatalf("ListDeliveriesForOwner: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliveries))
	}

	var payload struct {
		Event           string             `json:"event"`
		OwnerID         string             `json:"owner_id"`
		WindowFrom      time.Time          `json:"window_from"`
		WindowTo        time.Time          `json:"window_to"`
		RecentMeetings  []model.Note       `json:"recent_meetings"`
		OpenActionItems []model.ActionItem `json:"open_action_items"`
		NeedsFollowUp   []model.ActionItem `json:"needs_follow_up"`
		Text            string             `json:"text"`
	}
	if err := json.Unmarshal(deliveries[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.OpenActionItems) != 2 {
		t.Fatalf("open_action_items = %+v, want 2 items", payload.OpenActionItems)
	}
	if payload.OpenActionItems[0].ID != staleID || payload.OpenActionItems[1].ID != freshID {
		t.Fatalf("open_action_items order = [%s %s], want [%s %s]", payload.OpenActionItems[0].ID, payload.OpenActionItems[1].ID, staleID, freshID)
	}
	if len(payload.NeedsFollowUp) != 1 {
		t.Fatalf("needs_follow_up = %+v, want 1 item", payload.NeedsFollowUp)
	}
	if payload.NeedsFollowUp[0].ID != staleID || payload.NeedsFollowUp[0].Text != "Stale item" {
		t.Fatalf("needs_follow_up[0] = %+v, want stale item %s", payload.NeedsFollowUp[0], staleID)
	}
	if payload.NeedsFollowUp[0].ID == freshID {
		t.Fatalf("needs_follow_up unexpectedly includes fresh item %s", freshID)
	}
}

func TestSendDigestNoEnabledWebhooksIsNoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, err := st.CreateUser(ctx, "owner@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	note, err := st.CreateNote(ctx, owner.ID, "Weekly sync")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := st.ReplaceActionItemsForNote(ctx, owner.ID, note.ID, []model.ActionItem{
		{Text: "Ship digest"},
	}, nil); err != nil {
		t.Fatalf("ReplaceActionItemsForNote: %v", err)
	}

	anchor := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	setNoteStartedAt(t, st, note.ID, anchor)

	from := anchor.Add(-2 * time.Hour)
	to := anchor.Add(2 * time.Hour)
	if err := worker.SendDigest(ctx, st, owner.ID, from, to); err != nil {
		t.Fatalf("SendDigest: %v", err)
	}

	deliveries, err := st.ListDeliveriesForOwner(ctx, owner.ID, 10)
	if err != nil {
		t.Fatalf("ListDeliveriesForOwner: %v", err)
	}
	if len(deliveries) != 0 {
		t.Fatalf("deliveries = %d, want 0", len(deliveries))
	}
}
