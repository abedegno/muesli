package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	earlier := from.Add(-time.Hour)
	mid := from.Add(6 * time.Hour)
	later := from.Add(12 * time.Hour)
	boundary := to

	tests := []struct {
		name            string
		ownerID         string
		from            time.Time
		to              time.Time
		notes           []model.Note
		actionItems     []model.ActionItem
		wantErr         bool
		wantMeetings    []string
		wantActionIDs   []string
		wantEmptySlices bool
	}{
		{
			name:    "started at and created at fallback inside window",
			ownerID: "owner-1",
			from:    from,
			to:      to,
			notes: []model.Note{
				{ID: "n1", OwnerID: "owner-1", Title: "Started note", StartedAt: &mid, CreatedAt: earlier},
				{ID: "n2", OwnerID: "owner-1", Title: "Created fallback", CreatedAt: later},
				{ID: "n3", OwnerID: "owner-1", Title: "Outside window", CreatedAt: earlier},
			},
			actionItems:   []model.ActionItem{{ID: "a1", OwnerID: "owner-1", Text: "Keep this", Status: model.ActionItemOpen, CreatedAt: mid}},
			wantMeetings:  []string{"Created fallback", "Started note"},
			wantActionIDs: []string{"a1"},
		},
		{
			name:    "to exclusive boundary",
			ownerID: "owner-1",
			from:    from,
			to:      to,
			notes: []model.Note{
				{ID: "n1", OwnerID: "owner-1", Title: "Boundary", CreatedAt: boundary},
				{ID: "n2", OwnerID: "owner-1", Title: "Inside", CreatedAt: to.Add(-time.Second)},
			},
			wantMeetings: []string{"Inside"},
		},
		{
			name:    "mismatched owner dropped",
			ownerID: "owner-1",
			from:    from,
			to:      to,
			notes: []model.Note{
				{ID: "n1", OwnerID: "owner-2", Title: "Other owner", CreatedAt: mid},
				{ID: "n2", OwnerID: "owner-1", Title: "Mine", CreatedAt: mid},
			},
			actionItems: []model.ActionItem{
				{ID: "a1", OwnerID: "owner-2", Text: "Other owner", Status: model.ActionItemOpen, CreatedAt: mid},
				{ID: "a2", OwnerID: "owner-1", Text: "Mine", Status: model.ActionItemOpen, CreatedAt: mid},
			},
			wantMeetings:  []string{"Mine"},
			wantActionIDs: []string{"a2"},
		},
		{
			name:    "open and done action items filtered",
			ownerID: "owner-1",
			from:    from,
			to:      to,
			actionItems: []model.ActionItem{
				{ID: "a1", OwnerID: "owner-1", Text: "Open item", Status: model.ActionItemOpen, CreatedAt: mid},
				{ID: "a2", OwnerID: "owner-1", Text: "Done item", Status: model.ActionItemDone, CreatedAt: earlier},
			},
			wantActionIDs: []string{"a1"},
		},
		{
			name:    "deterministic ordering and deleted notes excluded",
			ownerID: "owner-1",
			from:    from,
			to:      to,
			notes: []model.Note{
				{ID: "b", OwnerID: "owner-1", Title: "Later same time", CreatedAt: mid},
				{ID: "a", OwnerID: "owner-1", Title: "Earlier same time", StartedAt: &mid, CreatedAt: earlier},
				{ID: "z", OwnerID: "owner-1", Title: "Deleted", CreatedAt: later, DeletedAt: &later},
			},
			actionItems: []model.ActionItem{
				{ID: "b", OwnerID: "owner-1", Text: "Later", Status: model.ActionItemOpen, CreatedAt: later},
				{ID: "a", OwnerID: "owner-1", Text: "Earlier", Status: model.ActionItemOpen, CreatedAt: earlier},
				{ID: "c", OwnerID: "owner-1", Text: "Same time alpha", Status: model.ActionItemOpen, CreatedAt: mid},
				{ID: "d", OwnerID: "owner-1", Text: "Same time beta", Status: model.ActionItemOpen, CreatedAt: mid},
			},
			wantMeetings:  []string{"Earlier same time", "Later same time"},
			wantActionIDs: []string{"a", "c", "d", "b"},
		},
		{
			name:    "empty result returns non-nil slices",
			ownerID: "owner-1",
			from:    from,
			to:      to,
			notes: []model.Note{
				{ID: "n1", OwnerID: "owner-1", Title: "Outside before", CreatedAt: earlier},
				{ID: "n2", OwnerID: "owner-2", Title: "Wrong owner", CreatedAt: mid},
			},
			actionItems: []model.ActionItem{
				{ID: "a1", OwnerID: "owner-1", Text: "Done", Status: model.ActionItemDone, CreatedAt: mid},
			},
			wantMeetings:    []string{},
			wantActionIDs:   []string{},
			wantEmptySlices: true,
		},
		{
			name:    "from after to errors",
			ownerID: "owner-1",
			from:    to,
			to:      from,
			wantErr: true,
		},
		{
			name:    "empty owner errors",
			from:    from,
			to:      to,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Build(tc.ownerID, tc.from, tc.to, tc.notes, tc.actionItems)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if got.OwnerID != tc.ownerID {
				t.Fatalf("OwnerID = %q, want %q", got.OwnerID, tc.ownerID)
			}
			if !got.WindowFrom.Equal(tc.from) {
				t.Fatalf("WindowFrom = %v, want %v", got.WindowFrom, tc.from)
			}
			if !got.WindowTo.Equal(tc.to) {
				t.Fatalf("WindowTo = %v, want %v", got.WindowTo, tc.to)
			}
			if tc.wantEmptySlices {
				if got.RecentMeetings == nil {
					t.Fatal("RecentMeetings is nil")
				}
				if got.OpenActionItems == nil {
					t.Fatal("OpenActionItems is nil")
				}
			}
			if len(got.RecentMeetings) != len(tc.wantMeetings) {
				t.Fatalf("RecentMeetings len = %d, want %d", len(got.RecentMeetings), len(tc.wantMeetings))
			}
			for i, wantTitle := range tc.wantMeetings {
				if got.RecentMeetings[i].Title != wantTitle {
					t.Fatalf("RecentMeetings[%d].Title = %q, want %q", i, got.RecentMeetings[i].Title, wantTitle)
				}
			}
			if len(got.OpenActionItems) != len(tc.wantActionIDs) {
				t.Fatalf("OpenActionItems len = %d, want %d", len(got.OpenActionItems), len(tc.wantActionIDs))
			}
			for i, wantID := range tc.wantActionIDs {
				if got.OpenActionItems[i].ID != wantID {
					t.Fatalf("OpenActionItems[%d].ID = %q, want %q", i, got.OpenActionItems[i].ID, wantID)
				}
			}
		})
	}
}

func TestRender(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, time.July, 10, 12, 30, 0, 0, time.UTC)
	d := Digest{
		OwnerID:    "owner-1",
		WindowFrom: time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC),
		WindowTo:   time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC),
		RecentMeetings: []model.Note{
			{ID: "n1", Title: "Weekly sync", StartedAt: &started, CreatedAt: started},
		},
		OpenActionItems: []model.ActionItem{
			{ID: "a1", Text: "Ship the doc", DueHint: "Friday"},
		},
	}

	body := Render(d)
	for _, want := range []string{
		"Digest",
		"Weekly sync",
		"2026-07-10T12:30:00Z",
		"Ship the doc",
		"Friday",
		"Recent Meetings",
		"Open Action Items",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("render output missing %q:\n%s", want, body)
		}
	}

	empty := Render(Digest{OwnerID: "owner-1", WindowFrom: d.WindowFrom, WindowTo: d.WindowTo})
	for _, want := range []string{"No recent meetings.", "No open action items."} {
		if !strings.Contains(empty, want) {
			t.Fatalf("empty render output missing %q:\n%s", want, empty)
		}
	}
}
