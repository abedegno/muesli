package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/store"
)

type insightsResponse struct {
	MeetingsPerDay []store.MeetingCountByDay   `json:"meetings_per_day"`
	TotalHours     float64                     `json:"total_hours"`
	HoursPerWeek   []store.MeetingHoursByWeek  `json:"hours_per_week"`
	TopPeople      []store.PersonMeetingCount  `json:"top_people"`
	TopCompanies   []store.CompanyMeetingCount `json:"top_companies"`
	TopFolders     []store.FolderMeetingCount  `json:"top_folders"`
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	uid, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, err := parseInsightsDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	meetingsPerDay, err := s.deps.Store.ListMeetingsPerDay(ctx, uid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	totalHours, err := s.deps.Store.TotalMeetingHours(ctx, uid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hoursPerWeek, err := s.deps.Store.ListMeetingHoursByWeek(ctx, uid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	topPeople, err := s.deps.Store.ListPeopleWithMeetingCount(ctx, uid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	topCompanies, err := s.deps.Store.ListCompaniesWithMeetingCount(ctx, uid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	topFolders, err := s.deps.Store.ListFoldersWithMeetingCount(ctx, uid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := insightsResponse{
		MeetingsPerDay: meetingsPerDay,
		TotalHours:     totalHours,
		HoursPerWeek:   hoursPerWeek,
		TopPeople:      topPeople,
		TopCompanies:   topCompanies,
		TopFolders:     topFolders,
	}
	if resp.MeetingsPerDay == nil {
		resp.MeetingsPerDay = []store.MeetingCountByDay{}
	}
	if resp.HoursPerWeek == nil {
		resp.HoursPerWeek = []store.MeetingHoursByWeek{}
	}
	if resp.TopPeople == nil {
		resp.TopPeople = []store.PersonMeetingCount{}
	}
	if resp.TopCompanies == nil {
		resp.TopCompanies = []store.CompanyMeetingCount{}
	}
	if resp.TopFolders == nil {
		resp.TopFolders = []store.FolderMeetingCount{}
	}

	writeJSON(w, http.StatusOK, resp)
}

func parseInsightsDateRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	to := now

	if raw := strings.TrimSpace(fromRaw); raw != "" {
		parsed, _, err := parseSearchDateRange(raw, "")
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if parsed != nil {
			from = parsed.UTC()
		}
	}

	if raw := strings.TrimSpace(toRaw); raw != "" {
		_, parsed, err := parseSearchDateRange("", raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if parsed != nil {
			to = parsed.UTC()
		}
	}

	return from, to, nil
}
