package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestDigestSettingsLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	owner, err := st.CreateUser(ctx, "owner@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "other@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}

	t.Run("default get returns off", func(t *testing.T) {
		cfg, err := st.GetDigestConfig(ctx, owner.ID)
		if err != nil {
			t.Fatalf("GetDigestConfig: %v", err)
		}
		if cfg.OwnerID != owner.ID || cfg.Cadence != model.DigestCadenceOff {
			t.Fatalf("cfg = %+v, want default off for owner", cfg)
		}
		if cfg.LastSentAt != nil || !cfg.UpdatedAt.IsZero() {
			t.Fatalf("cfg timestamps = %+v, want zero values on missing row", cfg)
		}
	})

	t.Run("bad cadence is rejected", func(t *testing.T) {
		if _, err := st.SetDigestConfig(ctx, owner.ID, "hourly"); !errors.Is(err, store.ErrInvalidState) {
			t.Fatalf("SetDigestConfig bad cadence err = %v, want ErrInvalidState", err)
		}
	})

	t.Run("upsert preserves last_sent_at", func(t *testing.T) {
		cfg, err := st.SetDigestConfig(ctx, owner.ID, model.DigestCadenceDaily)
		if err != nil {
			t.Fatalf("SetDigestConfig daily: %v", err)
		}
		if cfg.Cadence != model.DigestCadenceDaily {
			t.Fatalf("cadence = %q, want daily", cfg.Cadence)
		}

		sentAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := st.MarkDigestSent(ctx, owner.ID, sentAt); err != nil {
			t.Fatalf("MarkDigestSent: %v", err)
		}

		cfg, err = st.SetDigestConfig(ctx, owner.ID, model.DigestCadenceWeekly)
		if err != nil {
			t.Fatalf("SetDigestConfig weekly: %v", err)
		}
		if cfg.Cadence != model.DigestCadenceWeekly {
			t.Fatalf("cadence = %q, want weekly", cfg.Cadence)
		}
		if cfg.LastSentAt == nil || !cfg.LastSentAt.Equal(sentAt) {
			t.Fatalf("last_sent_at = %v, want %v preserved", cfg.LastSentAt, sentAt)
		}
	})

	t.Run("list enabled excludes off", func(t *testing.T) {
		if _, err := st.SetDigestConfig(ctx, other.ID, model.DigestCadenceOff); err != nil {
			t.Fatalf("SetDigestConfig off: %v", err)
		}
		third, err := st.CreateUser(ctx, "third@example.com", "h")
		if err != nil {
			t.Fatalf("CreateUser third: %v", err)
		}
		if _, err := st.SetDigestConfig(ctx, third.ID, model.DigestCadenceDaily); err != nil {
			t.Fatalf("SetDigestConfig third daily: %v", err)
		}

		got, err := st.ListEnabledDigestConfigs(ctx)
		if err != nil {
			t.Fatalf("ListEnabledDigestConfigs: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d enabled configs, want 2", len(got))
		}
		if got[0].OwnerID == other.ID || got[1].OwnerID == other.ID {
			t.Fatalf("off cadence leaked into enabled list: %+v", got)
		}
	})

	t.Run("mark digest sent", func(t *testing.T) {
		sentAt := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
		if err := st.MarkDigestSent(ctx, owner.ID, sentAt); err != nil {
			t.Fatalf("MarkDigestSent: %v", err)
		}
		cfg, err := st.GetDigestConfig(ctx, owner.ID)
		if err != nil {
			t.Fatalf("GetDigestConfig: %v", err)
		}
		if cfg.LastSentAt == nil || !cfg.LastSentAt.Equal(sentAt) {
			t.Fatalf("last_sent_at = %v, want %v", cfg.LastSentAt, sentAt)
		}
	})
}
