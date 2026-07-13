package store

import (
	"context"
	"sort"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

// MeetingCountByDay is the number of meetings started on a calendar day.
type MeetingCountByDay struct {
	Day   time.Time `json:"day"`
	Count int64     `json:"count"`
}

// MeetingHoursByWeek is the total meeting duration for one calendar week.
type MeetingHoursByWeek struct {
	WeekStart time.Time `json:"week_start"`
	Hours     float64   `json:"hours"`
}

// PersonMeetingCount is a person paired with the number of meetings they appear in.
type PersonMeetingCount struct {
	model.Person
	Count int64 `json:"count"`
}

// CompanyMeetingCount is a company paired with the number of meetings linked to its people.
type CompanyMeetingCount struct {
	model.Company
	Count int64 `json:"count"`
}

// FolderMeetingCount is a folder paired with the number of meetings in it.
type FolderMeetingCount struct {
	model.Folder
	Count int64 `json:"count"`
}

type meetingInsightRow struct {
	CreatedAt  time.Time
	DurationMS int64
}

func dayStartUTC(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func weekStartUTC(t time.Time) time.Time {
	utc := t.UTC()
	weekdayOffset := (int(utc.Weekday()) + 6) % 7
	start := utc.AddDate(0, 0, -weekdayOffset)
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
}

func meetingDurationHours(ms int64) float64 {
	return float64(ms) / 3600000.0
}

func bucketMeetingCountsByDay(rows []meetingInsightRow) []MeetingCountByDay {
	if len(rows) == 0 {
		return []MeetingCountByDay{}
	}
	counts := make(map[time.Time]int64, len(rows))
	days := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		day := dayStartUTC(row.CreatedAt)
		if _, ok := counts[day]; !ok {
			days = append(days, day)
		}
		counts[day]++
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].Before(days[j])
	})
	out := make([]MeetingCountByDay, 0, len(days))
	for _, day := range days {
		out = append(out, MeetingCountByDay{Day: day, Count: counts[day]})
	}
	return out
}

func bucketMeetingHoursByWeek(rows []meetingInsightRow) []MeetingHoursByWeek {
	if len(rows) == 0 {
		return []MeetingHoursByWeek{}
	}
	hoursByWeek := make(map[time.Time]float64, len(rows))
	weeks := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		week := weekStartUTC(row.CreatedAt)
		if _, ok := hoursByWeek[week]; !ok {
			weeks = append(weeks, week)
		}
		hoursByWeek[week] += meetingDurationHours(row.DurationMS)
	}
	sort.Slice(weeks, func(i, j int) bool {
		return weeks[i].Before(weeks[j])
	})
	out := make([]MeetingHoursByWeek, 0, len(weeks))
	for _, week := range weeks {
		out = append(out, MeetingHoursByWeek{WeekStart: week, Hours: hoursByWeek[week]})
	}
	return out
}

func sumMeetingHours(rows []meetingInsightRow) float64 {
	var hours float64
	for _, row := range rows {
		hours += meetingDurationHours(row.DurationMS)
	}
	return hours
}

func (s *Store) listMeetingInsightRows(ctx context.Context, ownerID string, from, to time.Time) ([]meetingInsightRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT n.created_at, COALESCE(MAX(ts.end_ms), 0) AS duration_ms
		 FROM notes n
		 LEFT JOIN transcripts tr ON tr.note_id = n.id
		 LEFT JOIN transcript_segments ts ON ts.transcript_id = tr.id
		 WHERE n.owner_id = $1
		   AND n.deleted_at IS NULL
		   AND n.created_at >= $2
		   AND n.created_at < $3
		 GROUP BY n.id, n.created_at
		 ORDER BY n.created_at ASC, n.id ASC`,
		ownerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []meetingInsightRow{}
	for rows.Next() {
		var row meetingInsightRow
		if err := rows.Scan(&row.CreatedAt, &row.DurationMS); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListMeetingsPerDay returns the number of live notes started on each calendar
// day in [from,to), ordered by day ascending.
func (s *Store) ListMeetingsPerDay(ctx context.Context, ownerID string, from, to time.Time) ([]MeetingCountByDay, error) {
	rows, err := s.listMeetingInsightRows(ctx, ownerID, from, to)
	if err != nil {
		return nil, err
	}
	out := bucketMeetingCountsByDay(rows)
	if out == nil {
		out = []MeetingCountByDay{}
	}
	return out, nil
}

// TotalMeetingHours returns the total meeting duration in hours for live notes
// in [from,to). Notes with no transcript contribute 0 duration.
func (s *Store) TotalMeetingHours(ctx context.Context, ownerID string, from, to time.Time) (float64, error) {
	rows, err := s.listMeetingInsightRows(ctx, ownerID, from, to)
	if err != nil {
		return 0, err
	}
	return sumMeetingHours(rows), nil
}

// ListMeetingHoursByWeek returns the total meeting duration for each calendar
// week in [from,to), ordered by week start ascending.
func (s *Store) ListMeetingHoursByWeek(ctx context.Context, ownerID string, from, to time.Time) ([]MeetingHoursByWeek, error) {
	rows, err := s.listMeetingInsightRows(ctx, ownerID, from, to)
	if err != nil {
		return nil, err
	}
	out := bucketMeetingHoursByWeek(rows)
	if out == nil {
		out = []MeetingHoursByWeek{}
	}
	return out, nil
}

// ListPeopleWithMeetingCount returns owner-scoped people who were linked from
// note speaker aliases within [from,to), ordered by meeting count descending.
func (s *Store) ListPeopleWithMeetingCount(ctx context.Context, ownerID string, from, to time.Time) ([]PersonMeetingCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, p.owner_id, p.primary_email, p.display_name, p.company_id, p.first_seen_at, p.updated_at,
		        COUNT(DISTINCT n.id) AS meeting_count
		 FROM people p
		 JOIN note_speaker_aliases nsa ON nsa.person_id = p.id AND nsa.owner_id = p.owner_id
		 JOIN notes n ON n.id = nsa.note_id
		               AND n.owner_id = p.owner_id
		               AND n.deleted_at IS NULL
		 WHERE p.owner_id = $1
		   AND n.created_at >= $2
		   AND n.created_at < $3
		 GROUP BY p.id, p.owner_id, p.primary_email, p.display_name, p.company_id, p.first_seen_at, p.updated_at
		 ORDER BY meeting_count DESC, lower(p.display_name), lower(p.primary_email), p.id`,
		ownerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PersonMeetingCount{}
	for rows.Next() {
		var person PersonMeetingCount
		if err := rows.Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt, &person.Count); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []PersonMeetingCount{}
	}
	return out, nil
}

// ListCompaniesWithMeetingCount returns owner-scoped companies paired with the
// number of meetings linked to their people within [from,to), ordered by count
// descending. People without a company are ignored.
func (s *Store) ListCompaniesWithMeetingCount(ctx context.Context, ownerID string, from, to time.Time) ([]CompanyMeetingCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.owner_id, c.domain, c.name, c.created_at, c.updated_at,
		        COUNT(DISTINCT n.id) AS meeting_count
		 FROM people p
		 JOIN note_speaker_aliases nsa ON nsa.person_id = p.id AND nsa.owner_id = p.owner_id
		 JOIN notes n ON n.id = nsa.note_id
		               AND n.owner_id = p.owner_id
		               AND n.deleted_at IS NULL
		 LEFT JOIN companies c ON c.id = p.company_id AND c.owner_id = p.owner_id
		 WHERE p.owner_id = $1
		   AND n.created_at >= $2
		   AND n.created_at < $3
		   AND c.id IS NOT NULL
		 GROUP BY c.id, c.owner_id, c.domain, c.name, c.created_at, c.updated_at
		 ORDER BY meeting_count DESC, lower(c.domain), lower(c.name), c.id`,
		ownerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CompanyMeetingCount{}
	for rows.Next() {
		var company CompanyMeetingCount
		if err := rows.Scan(&company.ID, &company.OwnerID, &company.Domain, &company.Name, &company.CreatedAt, &company.UpdatedAt, &company.Count); err != nil {
			return nil, err
		}
		out = append(out, company)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []CompanyMeetingCount{}
	}
	return out, nil
}

// ListFoldersWithMeetingCount returns owner-scoped folders paired with the
// number of live notes in them within [from,to), ordered by count descending.
func (s *Store) ListFoldersWithMeetingCount(ctx context.Context, ownerID string, from, to time.Time) ([]FolderMeetingCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.name, f.parent_id, f.created_at,
		        COUNT(DISTINCT n.id) AS meeting_count
		 FROM folders f
		 JOIN note_folders nf ON nf.folder_id = f.id
		 JOIN notes n ON n.id = nf.note_id
		              AND n.owner_id = f.owner_id
		              AND n.deleted_at IS NULL
		 WHERE f.owner_id = $1
		   AND f.deleted_at IS NULL
		   AND n.created_at >= $2
		   AND n.created_at < $3
		 GROUP BY f.id, f.name, f.parent_id, f.created_at
		 ORDER BY meeting_count DESC, lower(f.name), f.id`,
		ownerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FolderMeetingCount{}
	for rows.Next() {
		var folder FolderMeetingCount
		if err := rows.Scan(&folder.ID, &folder.Name, &folder.ParentID, &folder.CreatedAt, &folder.Count); err != nil {
			return nil, err
		}
		out = append(out, folder)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []FolderMeetingCount{}
	}
	return out, nil
}
