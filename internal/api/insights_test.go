package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

type insightsResponse struct {
	MeetingsPerDay []struct {
		Day   time.Time `json:"day"`
		Count int64     `json:"count"`
	} `json:"meetings_per_day"`
	TotalHours   float64 `json:"total_hours"`
	HoursPerWeek []struct {
		WeekStart time.Time `json:"week_start"`
		Hours     float64   `json:"hours"`
	} `json:"hours_per_week"`
	TopPeople []struct {
		model.Person
		Count int64 `json:"count"`
	} `json:"top_people"`
	TopCompanies []struct {
		model.Company
		Count int64 `json:"count"`
	} `json:"top_companies"`
	TopFolders []struct {
		model.Folder
		Count int64 `json:"count"`
	} `json:"top_folders"`
}

func newInsightsTestServer(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	return api.NewServer(api.Deps{Store: st, Crypto: cr}), st
}

func insightsAuthHeader(t *testing.T, st *store.Store, email string) (string, map[string]string) {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, email, "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(ctx, u.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return u.ID, map[string]string{"Authorization": "Bearer " + raw}
}

func setInsightNoteCreatedAt(t *testing.T, st *store.Store, noteID string, createdAt time.Time) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE notes SET created_at=$1, updated_at=$1 WHERE id=$2`,
		createdAt, noteID); err != nil {
		t.Fatalf("set note created_at: %v", err)
	}
}

func seedInsightMeeting(t *testing.T, st *store.Store, ownerID, title string, createdAt time.Time, durationMS int64, personName, companyDomain, companyName, folderName string) {
	t.Helper()
	ctx := context.Background()

	company, err := st.UpsertCompany(ctx, ownerID, companyDomain, companyName)
	if err != nil {
		t.Fatalf("upsert company: %v", err)
	}
	person, err := st.UpsertPerson(ctx, ownerID, personName+"@"+companyDomain, personName, &company.ID)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	folder, err := st.CreateFolder(ctx, ownerID, folderName, nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, title)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	setInsightNoteCreatedAt(t, st, note.ID, createdAt)
	if durationMS >= 0 {
		if _, err := st.SaveTranscript(ctx, model.Transcript{
			NoteID:            note.ID,
			TranscriberPlugin: "test-transcriber",
			Model:             "test-model",
			Segments: []model.Segment{{
				StartMS: 0,
				EndMS:   int(durationMS),
				Text:    "meeting",
				Source:  "test",
			}},
		}); err != nil {
			t.Fatalf("save transcript: %v", err)
		}
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", personName); err != nil {
		t.Fatalf("upsert speaker alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, note.ID, "SPEAKER_00", &person.ID); err != nil {
		t.Fatalf("set speaker alias person: %v", err)
	}
	if err := st.AddNoteFolder(ctx, ownerID, note.ID, folder.ID); err != nil {
		t.Fatalf("add note folder: %v", err)
	}
}

func decodeInsightsResponse(t *testing.T, rec *http.Response) insightsResponse {
	t.Helper()
	var out insightsResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode insights response: %v", err)
	}
	return out
}

func TestInsightsOwnerScopingAndRangeFiltering(t *testing.T) {
	t.Parallel()

	srv, st := newInsightsTestServer(t)
	ownerID, ownerHdr := insightsAuthHeader(t, st, "insights-owner@example.com")
	otherID, _ := insightsAuthHeader(t, st, "insights-other@example.com")

	seedInsightMeeting(t, st, ownerID, "Owner January A", time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 1800000, "Owner Alpha", "alpha.example", "Alpha", "Alpha Folder")
	seedInsightMeeting(t, st, ownerID, "Owner January B", time.Date(2026, 1, 6, 11, 0, 0, 0, time.UTC), 3600000, "Owner Alpha", "alpha.example", "Alpha", "Alpha Folder")
	seedInsightMeeting(t, st, ownerID, "Owner February", time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), 2700000, "Owner Gamma", "gamma.example", "Gamma", "Gamma Folder")

	seedInsightMeeting(t, st, otherID, "Other January", time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC), 5400000, "Other Alpha", "other.example", "Other", "Other Folder")

	rec := doJSON(t, srv, http.MethodGet, "/api/insights?from=2026-01-05&to=2026-01-06T23:59:59Z", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("insights status %d body %s", rec.Code, rec.Body)
	}

	out := decodeInsightsResponse(t, rec.Result())
	if len(out.MeetingsPerDay) != 2 {
		t.Fatalf("meetings_per_day len = %d, want 2: %+v", len(out.MeetingsPerDay), out.MeetingsPerDay)
	}
	if !out.MeetingsPerDay[0].Day.Equal(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) || out.MeetingsPerDay[0].Count != 1 {
		t.Fatalf("meetings_per_day[0] = %+v, want Jan 5 count 1", out.MeetingsPerDay[0])
	}
	if !out.MeetingsPerDay[1].Day.Equal(time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)) || out.MeetingsPerDay[1].Count != 1 {
		t.Fatalf("meetings_per_day[1] = %+v, want Jan 6 count 1", out.MeetingsPerDay[1])
	}
	if out.TotalHours != 1.5 {
		t.Fatalf("total_hours = %v, want 1.5", out.TotalHours)
	}
	if len(out.HoursPerWeek) != 1 || !out.HoursPerWeek[0].WeekStart.Equal(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) || out.HoursPerWeek[0].Hours != 1.5 {
		t.Fatalf("hours_per_week = %+v, want week of Jan 5 with 1.5 hours", out.HoursPerWeek)
	}
	if len(out.TopPeople) != 1 || out.TopPeople[0].DisplayName != "Owner Alpha" || out.TopPeople[0].Count != 2 {
		t.Fatalf("top_people = %+v, want Owner Alpha count 2", out.TopPeople)
	}
	if len(out.TopCompanies) != 1 || out.TopCompanies[0].Domain != "alpha.example" || out.TopCompanies[0].Count != 2 {
		t.Fatalf("top_companies = %+v, want alpha.example count 2", out.TopCompanies)
	}
	if len(out.TopFolders) != 1 || out.TopFolders[0].Name != "Alpha Folder" || out.TopFolders[0].Count != 2 {
		t.Fatalf("top_folders = %+v, want Alpha Folder count 2", out.TopFolders)
	}
	for _, people := range out.TopPeople {
		if people.DisplayName == "Other Alpha" {
			t.Fatalf("cross-owner person leaked into insights: %+v", out.TopPeople)
		}
	}
	for _, company := range out.TopCompanies {
		if company.Domain == "other.example" {
			t.Fatalf("cross-owner company leaked into insights: %+v", out.TopCompanies)
		}
	}
	for _, folder := range out.TopFolders {
		if folder.Name == "Other Folder" {
			t.Fatalf("cross-owner folder leaked into insights: %+v", out.TopFolders)
		}
	}
}

func TestInsightsEmptyRangeReturnsZeros(t *testing.T) {
	t.Parallel()

	srv, st := newInsightsTestServer(t)
	_, ownerHdr := insightsAuthHeader(t, st, "insights-empty@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/insights", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("insights empty status %d body %s", rec.Code, rec.Body)
	}

	out := decodeInsightsResponse(t, rec.Result())
	if out.TotalHours != 0 {
		t.Fatalf("total_hours = %v, want 0", out.TotalHours)
	}
	if len(out.MeetingsPerDay) != 0 {
		t.Fatalf("meetings_per_day len = %d, want 0", len(out.MeetingsPerDay))
	}
	if len(out.HoursPerWeek) != 0 {
		t.Fatalf("hours_per_week len = %d, want 0", len(out.HoursPerWeek))
	}
	if len(out.TopPeople) != 0 {
		t.Fatalf("top_people len = %d, want 0", len(out.TopPeople))
	}
	if len(out.TopCompanies) != 0 {
		t.Fatalf("top_companies len = %d, want 0", len(out.TopCompanies))
	}
	if len(out.TopFolders) != 0 {
		t.Fatalf("top_folders len = %d, want 0", len(out.TopFolders))
	}
}
