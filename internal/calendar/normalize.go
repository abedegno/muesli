package calendar

import (
	"time"

	"github.com/abedegno/muesli/internal/model"
)

type NormalizedEvent struct {
	ExternalID      string
	Title           string
	StartsAt        time.Time
	EndsAt          time.Time
	Description     string
	Location        string
	ConferencingURL string
	Attendees       []model.Attendee
}
