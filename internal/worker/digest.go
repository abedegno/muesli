package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/digest"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

type digestDeliveryPayload struct {
	Event           string             `json:"event"`
	OwnerID         string             `json:"owner_id"`
	WindowFrom      time.Time          `json:"window_from"`
	WindowTo        time.Time          `json:"window_to"`
	RecentMeetings  []model.Note       `json:"recent_meetings"`
	OpenActionItems []model.ActionItem `json:"open_action_items"`
	NeedsFollowUp   []model.ActionItem `json:"needs_follow_up"`
	Text            string             `json:"text"`
}

// SendDigest assembles a digest for owner and enqueues one delivery per
// enabled webhook through the existing webhook delivery queue.
func SendDigest(ctx context.Context, st *store.Store, owner string, from, to time.Time) error {
	notes, err := st.ListNotes(ctx, owner, store.ListNotesFilter{})
	if err != nil {
		return err
	}
	actionItems, err := st.ListForOwner(ctx, owner, "")
	if err != nil {
		return err
	}

	d, err := digest.Build(owner, from, to, notes, actionItems)
	if err != nil {
		return err
	}

	webhooks, err := st.ListEnabledWebhooksForOwner(ctx, owner)
	if err != nil {
		return err
	}
	if len(webhooks) == 0 {
		return nil
	}

	payload := digestDeliveryPayload{
		Event:           "digest.summary",
		OwnerID:         d.OwnerID,
		WindowFrom:      d.WindowFrom.UTC(),
		WindowTo:        d.WindowTo.UTC(),
		RecentMeetings:  d.RecentMeetings,
		OpenActionItems: d.OpenActionItems,
		NeedsFollowUp:   d.NeedsFollowUp,
		Text:            digest.Render(d),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for _, wh := range webhooks {
		if err := st.CreateDelivery(ctx, wh.ID, body); err != nil {
			slog.WarnContext(ctx, "digest: enqueue webhook delivery", "error", err, "owner_id", owner, "webhook_id", wh.ID)
		}
	}
	return nil
}
