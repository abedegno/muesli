package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInsightsStoreAggregations(t *testing.T) {
	t.Parallel()

	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()

	alphaCompany, err := st.UpsertCompany(ctx, ownerID, "alpha.example", "Alpha")
	if err != nil {
		t.Fatalf("create alpha company: %v", err)
	}
	betaCompany, err := st.UpsertCompany(ctx, ownerID, "beta.example", "Beta")
	if err != nil {
		t.Fatalf("create beta company: %v", err)
	}

	alice, err := st.UpsertPerson(ctx, ownerID, "alice@alpha.example", "Alice", ptrString(alphaCompany.ID))
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := st.UpsertPerson(ctx, ownerID, "bob@beta.example", "Bob", ptrString(betaCompany.ID))
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	alphaFolder, err := st.CreateFolder(ctx, ownerID, "Alpha", nil)
	if err != nil {
		t.Fatalf("create alpha folder: %v", err)
	}
	betaFolder, err := st.CreateFolder(ctx, ownerID, "Beta", nil)
	if err != nil {
		t.Fatalf("create beta folder: %v", err)
	}

	n1 := createInsightNote(t, st, pool, ownerID, "Note 1", time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), 1800000, alice.ID, alphaFolder.ID)
	createInsightNote(t, st, pool, ownerID, "Note 2", time.Date(2026, 1, 5, 15, 0, 0, 0, time.UTC), 3600000, alice.ID, alphaFolder.ID)
	createInsightNote(t, st, pool, ownerID, "Note 3", time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 0, bob.ID, betaFolder.ID)
	createInsightNote(t, st, pool, ownerID, "Note 4", time.Date(2026, 1, 12, 11, 0, 0, 0, time.UTC), 5400000, bob.ID, betaFolder.ID)
	trashed := createInsightNote(t, st, pool, ownerID, "Trashed", time.Date(2026, 1, 7, 8, 0, 0, 0, time.UTC), 7200000, alice.ID, alphaFolder.ID)
	if err := st.DeleteNote(ctx, ownerID, trashed.ID); err != nil {
		t.Fatalf("trash note: %v", err)
	}

	from := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)

	byDay, err := st.ListMeetingsPerDay(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list meetings per day: %v", err)
	}
	assertMeetingCountByDaySliceDB(t, byDay, []store.MeetingCountByDay{
		{Day: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Count: 2},
		{Day: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), Count: 1},
		{Day: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Count: 1},
	})

	totalHours, err := st.TotalMeetingHours(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("total meeting hours: %v", err)
	}
	if totalHours != 3.0 {
		t.Fatalf("total meeting hours = %v, want 3.0", totalHours)
	}

	byWeek, err := st.ListMeetingHoursByWeek(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list meeting hours by week: %v", err)
	}
	assertMeetingHoursByWeekSliceDB(t, byWeek, []store.MeetingHoursByWeek{
		{WeekStart: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Hours: 1.5},
		{WeekStart: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Hours: 1.5},
	})

	people, err := st.ListPeopleWithMeetingCount(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list people with meeting count: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("people len = %d, want 2: %+v", len(people), people)
	}
	if people[0].DisplayName != "Alice" || people[0].Count != 2 {
		t.Fatalf("people[0] = %+v, want Alice count 2", people[0])
	}
	if people[1].DisplayName != "Bob" || people[1].Count != 2 {
		t.Fatalf("people[1] = %+v, want Bob count 2", people[1])
	}

	companies, err := st.ListCompaniesWithMeetingCount(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list companies with meeting count: %v", err)
	}
	if len(companies) != 2 {
		t.Fatalf("companies len = %d, want 2: %+v", len(companies), companies)
	}
	if companies[0].Domain != "alpha.example" || companies[0].Count != 2 {
		t.Fatalf("companies[0] = %+v, want alpha.example count 2", companies[0])
	}
	if companies[1].Domain != "beta.example" || companies[1].Count != 2 {
		t.Fatalf("companies[1] = %+v, want beta.example count 2", companies[1])
	}

	folders, err := st.ListFoldersWithMeetingCount(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list folders with meeting count: %v", err)
	}
	if len(folders) != 2 {
		t.Fatalf("folders len = %d, want 2: %+v", len(folders), folders)
	}
	if folders[0].Name != "Alpha" || folders[0].Count != 2 {
		t.Fatalf("folders[0] = %+v, want Alpha count 2", folders[0])
	}
	if folders[1].Name != "Beta" || folders[1].Count != 2 {
		t.Fatalf("folders[1] = %+v, want Beta count 2", folders[1])
	}

	otherOwner, err := st.CreateUser(ctx, "insights-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if got, err := st.ListMeetingsPerDay(ctx, otherOwner.ID, from, to); err != nil {
		t.Fatalf("other owner list per day: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("other owner per day len = %d, want 0", len(got))
	}
	if got, err := st.ListPeopleWithMeetingCount(ctx, otherOwner.ID, from, to); err != nil {
		t.Fatalf("other owner people: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("other owner people len = %d, want 0", len(got))
	}

	if _, err := st.GetNote(ctx, ownerID, n1.ID); err != nil {
		t.Fatalf("sanity get note: %v", err)
	}
}

func TestInsightsStoreNotesWithNoTranscriptCountAsZeroHours(t *testing.T) {
	t.Parallel()

	st, ownerID, pool := newStoreWithOwner(t)
	ctx := context.Background()
	note, err := st.CreateNote(ctx, ownerID, "No transcript")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	setNoteCreatedAt(t, pool, note.ID, from)

	hours, err := st.TotalMeetingHours(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("total meeting hours: %v", err)
	}
	if hours != 0 {
		t.Fatalf("total meeting hours = %v, want 0", hours)
	}

	byDay, err := st.ListMeetingsPerDay(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list meetings per day: %v", err)
	}
	if len(byDay) != 1 || !byDay[0].Day.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || byDay[0].Count != 1 {
		t.Fatalf("unexpected per-day rollup: %+v", byDay)
	}

	if err := st.DeleteNote(ctx, ownerID, note.ID); err != nil {
		t.Fatalf("trash note: %v", err)
	}
}

func createInsightNote(t *testing.T, st *store.Store, pool *pgxpool.Pool, ownerID, title string, createdAt time.Time, durationMS int, personID, folderID string) model.Note {
	t.Helper()
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, title)
	if err != nil {
		t.Fatalf("create note %q: %v", title, err)
	}
	setNoteCreatedAt(t, pool, note.ID, createdAt)
	if durationMS > 0 {
		seedTranscript(t, st, note.ID, durationMS)
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", title); err != nil {
		t.Fatalf("upsert speaker alias %q: %v", title, err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, note.ID, "SPEAKER_00", &personID); err != nil {
		t.Fatalf("set speaker alias person %q: %v", title, err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, folderID); err != nil {
		t.Fatalf("add note folder %q: %v", title, err)
	}
	return note
}

func seedTranscript(t *testing.T, st *store.Store, noteID string, durationMS int) {
	t.Helper()
	ctx := context.Background()
	tr := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "test-transcriber",
		Model:             "test-model",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: durationMS, Text: "hello", Source: "test"},
		},
		ReviewState: model.ReviewStateCompleted,
	}
	if _, err := st.SaveTranscript(ctx, tr); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
}

func setNoteCreatedAt(t *testing.T, pool *pgxpool.Pool, noteID string, createdAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE notes SET created_at=$1, updated_at=$1 WHERE id=$2`,
		createdAt, noteID); err != nil {
		t.Fatalf("set note created_at: %v", err)
	}
}

func assertMeetingCountByDaySliceDB(t *testing.T, got, want []store.MeetingCountByDay) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil slice, want non-nil slice")
	}
	if len(got) != len(want) {
		t.Fatalf("got len %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) || got[i].Count != want[i].Count {
			t.Fatalf("got[%d]=%+v, want %+v", i, got[i], want[i])
		}
	}
}

func assertMeetingHoursByWeekSliceDB(t *testing.T, got, want []store.MeetingHoursByWeek) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil slice, want non-nil slice")
	}
	if len(got) != len(want) {
		t.Fatalf("got len %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].WeekStart.Equal(want[i].WeekStart) || got[i].Hours != want[i].Hours {
			t.Fatalf("got[%d]=%+v, want %+v", i, got[i], want[i])
		}
	}
}
