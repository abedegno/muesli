package worker

import (
	"testing"
	"time"
)

func TestDigestDue(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	pastDaily := now.Add(-24 * time.Hour)
	pastWeekly := now.Add(-7 * 24 * time.Hour)
	justUnderDaily := now.Add(-23*time.Hour - 59*time.Minute)
	justUnderWeekly := now.Add(-(7*24*time.Hour - time.Minute))

	tests := []struct {
		name     string
		cadence  string
		lastSent *time.Time
		wantDue  bool
	}{
		{name: "off is never due", cadence: "off", wantDue: false},
		{name: "daily with nil last sent is due", cadence: "daily", wantDue: true},
		{name: "weekly with nil last sent is due", cadence: "weekly", wantDue: true},
		{name: "daily at boundary is due", cadence: "daily", lastSent: &pastDaily, wantDue: true},
		{name: "weekly at boundary is due", cadence: "weekly", lastSent: &pastWeekly, wantDue: true},
		{name: "daily before boundary is not due", cadence: "daily", lastSent: &justUnderDaily, wantDue: false},
		{name: "weekly before boundary is not due", cadence: "weekly", lastSent: &justUnderWeekly, wantDue: false},
		{name: "unknown cadence is not due", cadence: "hourly", wantDue: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := digestDue(tt.cadence, tt.lastSent, now); got != tt.wantDue {
				t.Fatalf("digestDue(%q, %v) = %v, want %v", tt.cadence, tt.lastSent, got, tt.wantDue)
			}
		})
	}
}
