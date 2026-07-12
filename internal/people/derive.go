package people

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// uniqueAttendeesFromEvents flattens attendees across calendar events and
// deduplicates them by email address.
func uniqueAttendeesFromEvents(events []model.CalendarEvent) []model.Attendee {
	attendees := make([]model.Attendee, 0)
	for _, ev := range events {
		attendees = append(attendees, ev.Attendees...)
	}
	return DedupByEmail(attendees)
}

// DerivePeople turns calendar attendees into people and company records.
// It is intentionally best-effort: one bad attendee should not stop the
// rest from being derived.
func DerivePeople(ctx context.Context, st *store.Store, ownerID string) error {
	now := time.Now()
	events, err := st.ListEvents(ctx, ownerID, now.Add(-365*24*time.Hour), now.Add(365*24*time.Hour))
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}

	attendees := uniqueAttendeesFromEvents(events)
	var errs []error

	for _, attendee := range attendees {
		email := attendee.Email
		name := attendee.Name

		var companyID *string
		if domain, isCompany := CompanyDomain(email); isCompany {
			company, err := st.UpsertCompany(ctx, ownerID, domain, "")
			if err != nil {
				slog.ErrorContext(ctx, "derive people: upsert company", "owner_id", ownerID, "email", email, "domain", domain, "error", err)
				errs = append(errs, fmt.Errorf("upsert company %q: %w", domain, err))
			} else {
				companyID = &company.ID
			}
		}

		if _, err := st.UpsertPerson(ctx, ownerID, email, name, companyID); err != nil {
			slog.ErrorContext(ctx, "derive people: upsert person", "owner_id", ownerID, "email", email, "error", err)
			errs = append(errs, fmt.Errorf("upsert person %q: %w", email, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
