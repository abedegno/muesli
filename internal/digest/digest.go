package digest

import (
	"errors"
	"sort"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

// FollowUpThreshold is the minimum age an open action item must reach before
// it is considered a follow-up reminder candidate.
const FollowUpThreshold = 7 * 24 * time.Hour

// Digest is a pure in-memory assembly of a periodic owner digest.
type Digest struct {
	OwnerID         string
	WindowFrom      time.Time
	WindowTo        time.Time
	RecentMeetings  []model.Note
	OpenActionItems []model.ActionItem
	NeedsFollowUp   []model.ActionItem
}

// Build assembles a digest from already-fetched notes and action items.
func Build(ownerID string, from, to time.Time, notes []model.Note, actionItems []model.ActionItem) (Digest, error) {
	if ownerID == "" {
		return Digest{}, errors.New("ownerID is required")
	}
	if to.Before(from) {
		return Digest{}, errors.New("window end must not be before window start")
	}

	recentMeetings := make([]model.Note, 0, len(notes))
	for _, note := range notes {
		if note.OwnerID != ownerID {
			continue
		}
		if note.DeletedAt != nil {
			continue
		}
		effectiveTime := noteEffectiveTime(note)
		if effectiveTime.Before(from) || !effectiveTime.Before(to) {
			continue
		}
		recentMeetings = append(recentMeetings, note)
	}
	sort.Slice(recentMeetings, func(i, j int) bool {
		ti := noteEffectiveTime(recentMeetings[i])
		tj := noteEffectiveTime(recentMeetings[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return recentMeetings[i].ID < recentMeetings[j].ID
	})

	openActionItems := make([]model.ActionItem, 0, len(actionItems))
	for _, item := range actionItems {
		if item.OwnerID != ownerID {
			continue
		}
		if item.Status != model.ActionItemOpen {
			continue
		}
		openActionItems = append(openActionItems, item)
	}
	sort.Slice(openActionItems, func(i, j int) bool {
		if !openActionItems[i].CreatedAt.Equal(openActionItems[j].CreatedAt) {
			return openActionItems[i].CreatedAt.Before(openActionItems[j].CreatedAt)
		}
		return openActionItems[i].ID < openActionItems[j].ID
	})

	needsFollowUp := make([]model.ActionItem, 0, len(openActionItems))
	for _, item := range openActionItems {
		if to.Sub(item.CreatedAt) >= FollowUpThreshold {
			needsFollowUp = append(needsFollowUp, item)
		}
	}

	return Digest{
		OwnerID:         ownerID,
		WindowFrom:      from,
		WindowTo:        to,
		RecentMeetings:  recentMeetings,
		OpenActionItems: openActionItems,
		NeedsFollowUp:   needsFollowUp,
	}, nil
}

func noteEffectiveTime(note model.Note) time.Time {
	if note.StartedAt != nil {
		return *note.StartedAt
	}
	return note.CreatedAt
}
