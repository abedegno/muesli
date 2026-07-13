package store

import (
	"testing"
	"time"
)

func TestInsightsPureRollups(t *testing.T) {
	rows := []meetingInsightRow{
		{CreatedAt: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), DurationMS: 1800000},
		{CreatedAt: time.Date(2026, 1, 5, 15, 0, 0, 0, time.UTC), DurationMS: 3600000},
		{CreatedAt: time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), DurationMS: 0},
		{CreatedAt: time.Date(2026, 1, 12, 11, 0, 0, 0, time.UTC), DurationMS: 5400000},
	}

	tests := []struct {
		name string
		kind string
		rows []meetingInsightRow
	}{
		{name: "counts by day", kind: "day", rows: rows},
		{name: "hours by week", kind: "week", rows: rows},
		{name: "total hours", kind: "total", rows: rows},
		{name: "empty day buckets", kind: "day-empty", rows: nil},
		{name: "empty week buckets", kind: "week-empty", rows: nil},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			switch tc.kind {
			case "day":
				got := bucketMeetingCountsByDay(tc.rows)
				want := []MeetingCountByDay{
					{Day: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Count: 2},
					{Day: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), Count: 1},
					{Day: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Count: 1},
				}
				assertMeetingCountByDaySlice(t, got, want)
			case "week":
				got := bucketMeetingHoursByWeek(tc.rows)
				want := []MeetingHoursByWeek{
					{WeekStart: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Hours: 1.5},
					{WeekStart: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Hours: 1.5},
				}
				assertMeetingHoursByWeekSlice(t, got, want)
			case "total":
				if got := sumMeetingHours(tc.rows); got != 3.0 {
					t.Fatalf("sumMeetingHours() = %v, want 3.0", got)
				}
			case "day-empty":
				if got := bucketMeetingCountsByDay(tc.rows); got == nil || len(got) != 0 {
					t.Fatalf("bucketMeetingCountsByDay(nil) = %#v, want non-nil empty slice", got)
				}
			case "week-empty":
				if got := bucketMeetingHoursByWeek(tc.rows); got == nil || len(got) != 0 {
					t.Fatalf("bucketMeetingHoursByWeek(nil) = %#v, want non-nil empty slice", got)
				}
			default:
				t.Fatalf("unknown case kind %q", tc.kind)
			}
		})
	}
}

func assertMeetingCountByDaySlice(t *testing.T, got, want []MeetingCountByDay) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil slice, want non-nil slice")
	}
	if len(got) != len(want) {
		t.Fatalf("got len %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) || got[i].Count != want[i].Count {
			t.Fatalf("got[%d]=%+v, want %+v", i, got[i], want[i])
		}
	}
}

func assertMeetingHoursByWeekSlice(t *testing.T, got, want []MeetingHoursByWeek) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil slice, want non-nil slice")
	}
	if len(got) != len(want) {
		t.Fatalf("got len %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].WeekStart.Equal(want[i].WeekStart) || got[i].Hours != want[i].Hours {
			t.Fatalf("got[%d]=%+v, want %+v", i, got[i], want[i])
		}
	}
}
