package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func digestDue(cadence string, lastSentAt *time.Time, now time.Time) bool {
	switch cadence {
	case model.DigestCadenceDaily:
		if lastSentAt == nil {
			return true
		}
		return now.Sub(*lastSentAt) >= 24*time.Hour
	case model.DigestCadenceWeekly:
		if lastSentAt == nil {
			return true
		}
		return now.Sub(*lastSentAt) >= 7*24*time.Hour
	default:
		return false
	}
}

func digestWindow(cadence string) time.Duration {
	switch cadence {
	case model.DigestCadenceDaily:
		return 24 * time.Hour
	case model.DigestCadenceWeekly:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

func runDigestSchedulerOnce(ctx context.Context, st *store.Store, now time.Time) {
	cfgs, err := st.ListEnabledDigestConfigs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "digest scheduler: list configs", "error", err)
		return
	}
	for _, cfg := range cfgs {
		if !digestDue(cfg.Cadence, cfg.LastSentAt, now) {
			continue
		}
		from := now.Add(-digestWindow(cfg.Cadence))
		if cfg.LastSentAt != nil {
			from = *cfg.LastSentAt
		}
		if err := SendDigest(ctx, st, cfg.OwnerID, from, now); err != nil {
			slog.ErrorContext(ctx, "digest scheduler: send", "error", err, "owner_id", cfg.OwnerID, "cadence", cfg.Cadence)
			continue
		}
		if err := st.MarkDigestSent(ctx, cfg.OwnerID, now); err != nil {
			slog.ErrorContext(ctx, "digest scheduler: mark sent", "error", err, "owner_id", cfg.OwnerID, "cadence", cfg.Cadence)
		}
	}
}

// RunDigestScheduler runs the digest sweep immediately and then at each tick
// until ctx is cancelled.
func RunDigestScheduler(ctx context.Context, st *store.Store, tickInterval time.Duration) {
	runDigestSchedulerOnce(ctx, st, time.Now().UTC())
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runDigestSchedulerOnce(ctx, st, time.Now().UTC())
		}
	}
}
