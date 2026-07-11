package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func decodeCalendarSelected(raw string) (map[string]bool, error) {
	out := map[string]bool{}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]bool{}
	}
	return out, nil
}

func decodeCalendarAttendees(raw string) ([]model.Attendee, error) {
	if raw == "" {
		return []model.Attendee{}, nil
	}
	var out []model.Attendee
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Attendee{}
	}
	return out, nil
}

func marshalJSONB(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Store) CreateSource(ctx context.Context, ownerID, kind, displayName string, sealedCreds string) (model.CalendarSource, error) {
	id := uuid.NewString()
	var src model.CalendarSource
	var selectedRaw string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO calendar_sources (id, owner_id, kind, display_name, credentials)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, owner_id, kind, display_name, selected_calendars::text, status, last_synced_at, created_at`,
		id, ownerID, kind, displayName, sealedCreds).
		Scan(&src.ID, &src.OwnerID, &src.Kind, &src.DisplayName, &selectedRaw, &src.Status, &src.LastSyncedAt, &src.CreatedAt)
	if err != nil {
		return model.CalendarSource{}, err
	}
	src.SelectedCalendars, err = decodeCalendarSelected(selectedRaw)
	if err != nil {
		return model.CalendarSource{}, err
	}
	return src, nil
}

func (s *Store) ListSources(ctx context.Context, ownerID string) ([]model.CalendarSource, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, kind, display_name, selected_calendars::text, status, last_synced_at, created_at
		 FROM calendar_sources
		 WHERE owner_id=$1
		 ORDER BY created_at DESC, id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CalendarSource{}
	for rows.Next() {
		var src model.CalendarSource
		var selectedRaw string
		if err := rows.Scan(&src.ID, &src.OwnerID, &src.Kind, &src.DisplayName, &selectedRaw, &src.Status, &src.LastSyncedAt, &src.CreatedAt); err != nil {
			return nil, err
		}
		src.SelectedCalendars, err = decodeCalendarSelected(selectedRaw)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// ListAllSourceIDs returns the IDs of every calendar source across all
// owners. Unlike ListSources (owner-scoped, for request handlers), this
// backs the background sync scheduler, which must enumerate every source
// regardless of owner. It selects only id, never credentials.
func (s *Store) ListAllSourceIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM calendar_sources ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) GetSourceCreds(ctx context.Context, sourceID string) (kind string, sealedCreds string, ownerID string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT kind, credentials, owner_id
		 FROM calendar_sources
		 WHERE id=$1`, sourceID).
		Scan(&kind, &sealedCreds, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", ErrNotFound
	}
	return kind, sealedCreds, ownerID, err
}

func (s *Store) GetSourceSelectedCalendars(ctx context.Context, sourceID string) (map[string]bool, error) {
	var raw string
	err := s.pool.QueryRow(ctx,
		`SELECT selected_calendars::text
		 FROM calendar_sources
		 WHERE id=$1`, sourceID).
		Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeCalendarSelected(raw)
}

func (s *Store) UpdateSourceStatus(ctx context.Context, sourceID, status string, syncedAt time.Time) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE calendar_sources
		 SET status=$2, last_synced_at=$3
		 WHERE id=$1`,
		sourceID, status, syncedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetSelectedCalendars(ctx context.Context, ownerID, sourceID string, sel map[string]bool) error {
	if sel == nil {
		sel = map[string]bool{}
	}
	stored, err := marshalJSONB(sel)
	if err != nil {
		return err
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE calendar_sources
		 SET selected_calendars=$3::jsonb
		 WHERE id=$1 AND owner_id=$2`,
		sourceID, ownerID, stored)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSource(ctx context.Context, ownerID, sourceID string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM calendar_sources
		 WHERE id=$1 AND owner_id=$2`,
		sourceID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpsertEvents(ctx context.Context, ownerID, sourceID string, evs []calendar.NormalizedEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var owned bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM calendar_sources WHERE id=$1 AND owner_id=$2)`,
		sourceID, ownerID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}

	for _, ev := range evs {
		attendees := ev.Attendees
		if attendees == nil {
			attendees = []model.Attendee{}
		}
		storedAttendees, err := marshalJSONB(attendees)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO calendar_events (
			   id, owner_id, source_id, external_id, title, starts_at, ends_at,
			   description, location, conferencing_url, attendees
			 )
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)
			 ON CONFLICT (source_id, external_id) DO UPDATE SET
			   owner_id = EXCLUDED.owner_id,
			   title = EXCLUDED.title,
			   starts_at = EXCLUDED.starts_at,
			   ends_at = EXCLUDED.ends_at,
			   description = EXCLUDED.description,
			   location = EXCLUDED.location,
			   conferencing_url = EXCLUDED.conferencing_url,
			   attendees = EXCLUDED.attendees,
			   updated_at = now()`,
			uuid.NewString(), ownerID, sourceID, ev.ExternalID, ev.Title, ev.StartsAt, ev.EndsAt,
			ev.Description, ev.Location, ev.ConferencingURL, storedAttendees)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) PruneEvents(ctx context.Context, sourceID string, keepExternalIDs []string) error {
	if len(keepExternalIDs) == 0 {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM calendar_events WHERE source_id=$1`,
			sourceID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM calendar_events
		 WHERE source_id=$1
		   AND NOT (external_id = ANY($2::text[]))`,
		sourceID, keepExternalIDs)
	return err
}

func (s *Store) ListEvents(ctx context.Context, ownerID string, from, to time.Time) ([]model.CalendarEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, source_id, external_id, title, starts_at, ends_at,
		        description, location, conferencing_url, attendees::text, updated_at
		 FROM calendar_events
		 WHERE owner_id=$1
		   AND starts_at <= $3
		   AND ends_at >= $2
		 ORDER BY starts_at ASC, external_id ASC`,
		ownerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CalendarEvent{}
	for rows.Next() {
		var ev model.CalendarEvent
		var attendeesRaw string
		if err := rows.Scan(&ev.ID, &ev.OwnerID, &ev.SourceID, &ev.ExternalID, &ev.Title, &ev.StartsAt, &ev.EndsAt,
			&ev.Description, &ev.Location, &ev.ConferencingURL, &attendeesRaw, &ev.UpdatedAt); err != nil {
			return nil, err
		}
		ev.Attendees, err = decodeCalendarAttendees(attendeesRaw)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
