package people

import (
	"context"

	"github.com/abedegno/muesli/internal/store"
)

// LinkNoteSpeakers best-effort links note speaker aliases to existing people.
// It preserves any aliases that already have a person link and returns the
// first hard store error encountered while still attempting the remaining
// aliases.
func LinkNoteSpeakers(ctx context.Context, st *store.Store, ownerID, noteID string) error {
	aliases, aliasErr := st.ListSpeakerAliases(ctx, ownerID, noteID)

	people, peopleErr := st.PeopleForNoteEvent(ctx, ownerID, noteID)
	if peopleErr == nil && len(people) == 0 {
		people, peopleErr = st.ListPeople(ctx, ownerID, "")
	}

	var firstErr error
	if aliasErr != nil {
		firstErr = aliasErr
	}
	if firstErr == nil && peopleErr != nil {
		firstErr = peopleErr
	}

	for _, alias := range aliases {
		if alias.AliasName == "" || alias.PersonID != nil {
			continue
		}

		personID, ok := MatchPersonByName(alias.AliasName, people)
		if !ok {
			continue
		}

		if err := st.SetSpeakerAliasPerson(ctx, ownerID, noteID, alias.SpeakerLabel, &personID); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
