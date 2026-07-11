package model

import "time"

type Attendee struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Response string `json:"response"`
}

type CalendarSource struct {
	ID                string          `json:"id"`
	OwnerID           string          `json:"owner_id"`
	Kind              string          `json:"kind"`
	DisplayName       string          `json:"display_name"`
	SelectedCalendars map[string]bool `json:"selected_calendars"`
	Status            string          `json:"status"`
	LastSyncedAt      *time.Time      `json:"last_synced_at"`
	CreatedAt         time.Time       `json:"created_at"`
}

type CalendarEvent struct {
	ID              string     `json:"id"`
	OwnerID         string     `json:"owner_id"`
	SourceID        string     `json:"source_id"`
	ExternalID      string     `json:"external_id"`
	Title           string     `json:"title"`
	StartsAt        time.Time  `json:"starts_at"`
	EndsAt          time.Time  `json:"ends_at"`
	Description     string     `json:"description"`
	Location        string     `json:"location"`
	ConferencingURL string     `json:"conferencing_url"`
	Attendees       []Attendee `json:"attendees"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
