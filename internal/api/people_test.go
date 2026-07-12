package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func newPeopleTestServer(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	return api.NewServer(api.Deps{Store: st, Crypto: cr}), st
}

func peopleAuthHeader(t *testing.T, st *store.Store, email string) (string, map[string]string) {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, email, "h")
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

func decodePeopleResponse(t *testing.T, rec *http.Response) []struct {
	model.Person
	Company *model.Company `json:"company,omitempty"`
} {
	t.Helper()
	var out []struct {
		model.Person
		Company *model.Company `json:"company,omitempty"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode people response: %v", err)
	}
	return out
}

func TestPeopleListOwnerScopingAndCompanyAttachment(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "people-owner@example.com")
	otherID, _ := peopleAuthHeader(t, st, "people-other@example.com")

	company, err := st.UpsertCompany(context.Background(), ownerID, "example.com", "Example")
	if err != nil {
		t.Fatalf("upsert company: %v", err)
	}
	otherCompany, err := st.UpsertCompany(context.Background(), otherID, "other.com", "Other")
	if err != nil {
		t.Fatalf("upsert other company: %v", err)
	}

	withCompany, err := st.UpsertPerson(context.Background(), ownerID, "alice@example.com", "Alice", &company.ID)
	if err != nil {
		t.Fatalf("upsert owner person with company: %v", err)
	}
	withoutCompany, err := st.UpsertPerson(context.Background(), ownerID, "bob@example.com", "Bob", nil)
	if err != nil {
		t.Fatalf("upsert owner person without company: %v", err)
	}
	otherPerson, err := st.UpsertPerson(context.Background(), otherID, "outside@example.com", "Outside", &otherCompany.ID)
	if err != nil {
		t.Fatalf("upsert other owner's person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/people", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d body %s", rec.Code, rec.Body)
	}

	listed := decodePeopleResponse(t, rec.Result())
	if len(listed) != 2 {
		t.Fatalf("expected 2 people for owner, got %d: %+v", len(listed), listed)
	}

	seenCompany := false
	seenNoCompany := false
	for _, person := range listed {
		if person.OwnerID != ownerID {
			t.Fatalf("found person for wrong owner: %+v", person.Person)
		}
		if person.ID == otherPerson.ID {
			t.Fatalf("cross-owner person leaked into list: %+v", person.Person)
		}
		switch person.ID {
		case withCompany.ID:
			if person.Company == nil {
				t.Fatalf("expected company on %s, got nil", person.ID)
			}
			if person.Company.ID != company.ID || person.Company.Domain != company.Domain || person.Company.Name != company.Name {
				t.Fatalf("unexpected attached company: %+v", person.Company)
			}
			seenCompany = true
		case withoutCompany.ID:
			if person.Company != nil {
				t.Fatalf("expected nil company on %s, got %+v", person.ID, person.Company)
			}
			seenNoCompany = true
		default:
			t.Fatalf("unexpected person returned: %+v", person.Person)
		}
	}
	if !seenCompany || !seenNoCompany {
		t.Fatalf("missing expected records: company=%v no-company=%v", seenCompany, seenNoCompany)
	}
}

func TestPeopleGetPersonReturns404ForCrossOwnerAndMissing(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "people-get-owner@example.com")
	_, otherHdr := peopleAuthHeader(t, st, "people-get-other@example.com")

	person, err := st.UpsertPerson(context.Background(), ownerID, "present@example.com", "Present", nil)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/people/"+person.ID, nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner get status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/people/00000000-0000-0000-0000-000000000000", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing person status %d, want 404; body %s", rec.Code, rec.Body)
	}
}

func TestPeopleGetPersonReturnsCompany(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "people-one@example.com")

	company, err := st.UpsertCompany(context.Background(), ownerID, "example.com", "Example")
	if err != nil {
		t.Fatalf("upsert company: %v", err)
	}
	person, err := st.UpsertPerson(context.Background(), ownerID, "alice@example.com", "Alice", &company.ID)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/people/"+person.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status %d body %s", rec.Code, rec.Body)
	}

	var out struct {
		model.Person
		Company *model.Company `json:"company,omitempty"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode person response: %v", err)
	}
	if out.ID != person.ID || out.OwnerID != ownerID || out.PrimaryEmail != person.PrimaryEmail || out.DisplayName != person.DisplayName {
		t.Fatalf("unexpected person payload: %+v", out.Person)
	}
	if out.Company == nil {
		t.Fatalf("expected company, got nil")
	}
	if out.Company.ID != company.ID || out.Company.OwnerID != ownerID || out.Company.Domain != company.Domain || out.Company.Name != company.Name {
		t.Fatalf("unexpected company payload: %+v", out.Company)
	}
}

func TestPeopleGetPersonRejectsMalformedID(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	_, ownerHdr := peopleAuthHeader(t, st, "people-badid@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/people/not-a-uuid", nil, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status %d, want 400; body %s", rec.Code, rec.Body)
	}
}

func TestPeopleUpdatePersonPatchAndClearCompany(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "people-update-owner@example.com")
	_, otherHdr := peopleAuthHeader(t, st, "people-update-other@example.com")
	otherOwnerID, _ := peopleAuthHeader(t, st, "people-update-company-owner@example.com")

	companyOne, err := st.UpsertCompany(context.Background(), ownerID, "one.example", "One")
	if err != nil {
		t.Fatalf("upsert company one: %v", err)
	}
	companyTwo, err := st.UpsertCompany(context.Background(), ownerID, "two.example", "Two")
	if err != nil {
		t.Fatalf("upsert company two: %v", err)
	}
	otherCompany, err := st.UpsertCompany(context.Background(), otherOwnerID, "other.example", "Other")
	if err != nil {
		t.Fatalf("upsert other company: %v", err)
	}
	person, err := st.UpsertPerson(context.Background(), ownerID, "person@example.com", "Original", &companyOne.ID)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPatch, "/api/people/"+person.ID, map[string]any{"display_name": "Renamed"}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch name status %d body %s", rec.Code, rec.Body)
	}
	var out struct {
		model.Person
		Company *model.Company `json:"company,omitempty"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if out.DisplayName != "Renamed" {
		t.Fatalf("expected renamed person, got %+v", out.Person)
	}
	if out.Company == nil || out.Company.ID != companyOne.ID {
		t.Fatalf("name-only patch should keep company, got %+v", out.Company)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/people/"+person.ID, map[string]any{"company_id": companyTwo.ID}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch company status %d body %s", rec.Code, rec.Body)
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode company patch response: %v", err)
	}
	if out.DisplayName != "Renamed" {
		t.Fatalf("company-only patch should keep name, got %+v", out.Person)
	}
	if out.Company == nil || out.Company.ID != companyTwo.ID {
		t.Fatalf("expected reassigned company, got %+v", out.Company)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/people/"+person.ID, map[string]any{"company_id": nil}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear company status %d body %s", rec.Code, rec.Body)
	}
	out = struct {
		model.Person
		Company *model.Company `json:"company,omitempty"`
	}{}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode clear patch response: %v", err)
	}
	if out.Company != nil {
		t.Fatalf("company_id null should clear company, got %+v", out.Company)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/people/"+person.ID, map[string]any{"company_id": otherCompany.ID}, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner company patch status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/people/"+person.ID, map[string]any{"company_id": "00000000-0000-0000-0000-000000000000"}, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing company patch status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/people/"+person.ID, map[string]any{"display_name": "Denied"}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner patch status %d, want 404; body %s", rec.Code, rec.Body)
	}
}

func TestPeopleMergePersonRepointsAliasesAndReturnsSurvivor(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "people-merge-owner@example.com")
	_, otherHdr := peopleAuthHeader(t, st, "people-merge-other@example.com")
	otherOwnerID, _ := peopleAuthHeader(t, st, "people-merge-target-owner@example.com")

	survivor, err := st.UpsertPerson(context.Background(), ownerID, "survivor@example.com", "Survivor", nil)
	if err != nil {
		t.Fatalf("upsert survivor: %v", err)
	}
	loser, err := st.UpsertPerson(context.Background(), ownerID, "loser@example.com", "Loser", nil)
	if err != nil {
		t.Fatalf("upsert loser: %v", err)
	}
	note, err := st.CreateNote(context.Background(), ownerID, "Meeting")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.UpsertSpeakerAlias(context.Background(), ownerID, note.ID, "SPEAKER_00", "Loser"); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(context.Background(), ownerID, note.ID, "SPEAKER_00", &loser.ID); err != nil {
		t.Fatalf("link alias to loser: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/people/"+loser.ID+"/merge", map[string]any{"into": survivor.ID}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("merge status %d body %s", rec.Code, rec.Body)
	}
	var out struct {
		model.Person
		Company *model.Company `json:"company,omitempty"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode merge response: %v", err)
	}
	if out.ID != survivor.ID {
		t.Fatalf("expected survivor response, got %+v", out.Person)
	}

	aliases, err := st.ListSpeakerAliases(context.Background(), ownerID, note.ID)
	if err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].PersonID == nil || *aliases[0].PersonID != survivor.ID {
		t.Fatalf("expected alias repointed to survivor, got %+v", aliases)
	}
	if _, err := st.GetPerson(context.Background(), ownerID, loser.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected loser person to be deleted, got %v", err)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/people/"+survivor.ID+"/merge", map[string]any{"into": survivor.ID}, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("same-id merge status %d, want 400; body %s", rec.Code, rec.Body)
	}

	otherPerson, err := st.UpsertPerson(context.Background(), otherOwnerID, "other@example.com", "Other", nil)
	if err != nil {
		t.Fatalf("upsert other owner person: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/people/"+loser.ID+"/merge", map[string]any{"into": otherPerson.ID}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner merge status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/people/"+loser.ID+"/merge", map[string]any{"into": otherPerson.ID}, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner target merge status %d, want 404; body %s", rec.Code, rec.Body)
	}
}

func TestPeopleDeletePersonUnlinksAliases(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "people-delete-owner@example.com")
	_, otherHdr := peopleAuthHeader(t, st, "people-delete-other@example.com")

	person, err := st.UpsertPerson(context.Background(), ownerID, "delete@example.com", "Delete", nil)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	note, err := st.CreateNote(context.Background(), ownerID, "Meeting")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.UpsertSpeakerAlias(context.Background(), ownerID, note.ID, "SPEAKER_00", "Delete"); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(context.Background(), ownerID, note.ID, "SPEAKER_00", &person.ID); err != nil {
		t.Fatalf("link alias to person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodDelete, "/api/people/"+person.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body)
	}
	if _, err := st.GetPerson(context.Background(), ownerID, person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected person to be deleted, got %v", err)
	}

	aliases, err := st.ListSpeakerAliases(context.Background(), ownerID, note.ID)
	if err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].PersonID != nil {
		t.Fatalf("expected alias person_id to be nulled, got %+v", aliases)
	}

	if _, err := st.GetNote(context.Background(), ownerID, note.ID); err != nil {
		t.Fatalf("note should remain: %v", err)
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/people/"+person.ID, nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete status %d, want 404; body %s", rec.Code, rec.Body)
	}
}

func TestPeoplePersonNotesEndpoint(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "person-notes-owner@example.com")
	otherID, otherHdr := peopleAuthHeader(t, st, "person-notes-other@example.com")

	person, err := st.UpsertPerson(context.Background(), ownerID, "person.activity@example.com", "Person", nil)
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	emptyPerson, err := st.UpsertPerson(context.Background(), ownerID, "empty.activity@example.com", "Empty", nil)
	if err != nil {
		t.Fatalf("upsert empty person: %v", err)
	}

	source, err := st.CreateSource(context.Background(), ownerID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := st.UpsertEvents(context.Background(), ownerID, source.ID, []calendar.NormalizedEvent{{
		ExternalID: "evt-person-notes",
		Title:      "Person notes",
		StartsAt:   noteEventHandlerTestBase,
		EndsAt:     noteEventHandlerTestBase.Add(30 * time.Minute),
		Attendees:  []model.Attendee{{Email: "PERSON.ACTIVITY@EXAMPLE.COM", Name: "Person"}},
	}}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	events, err := st.ListEvents(context.Background(), ownerID, noteEventHandlerTestBase.Add(-time.Hour), noteEventHandlerTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	eventNote, err := st.CreateNote(context.Background(), ownerID, "Event note")
	if err != nil {
		t.Fatalf("create event note: %v", err)
	}
	if err := st.UpdateNoteBody(context.Background(), ownerID, eventNote.ID, "event body"); err != nil {
		t.Fatalf("update event body: %v", err)
	}
	if _, err := st.AddNoteTag(context.Background(), ownerID, eventNote.ID, "work"); err != nil {
		t.Fatalf("add event tag: %v", err)
	}
	folder, err := st.CreateFolder(context.Background(), ownerID, "Projects", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := st.AddNoteFolder(context.Background(), ownerID, eventNote.ID, folder.ID); err != nil {
		t.Fatalf("add note folder: %v", err)
	}
	if err := st.SetNoteEvent(context.Background(), ownerID, eventNote.ID, events[0].ID); err != nil {
		t.Fatalf("set note event: %v", err)
	}

	aliasNote, err := st.CreateNote(context.Background(), ownerID, "Alias note")
	if err != nil {
		t.Fatalf("create alias note: %v", err)
	}
	if err := st.UpdateNoteBody(context.Background(), ownerID, aliasNote.ID, "alias body"); err != nil {
		t.Fatalf("update alias body: %v", err)
	}
	if err := st.UpsertSpeakerAlias(context.Background(), ownerID, aliasNote.ID, "SPEAKER_00", "Person"); err != nil {
		t.Fatalf("upsert alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(context.Background(), ownerID, aliasNote.ID, "SPEAKER_00", &person.ID); err != nil {
		t.Fatalf("set alias person: %v", err)
	}

	otherSource, err := st.CreateSource(context.Background(), otherID, "ics", "Other Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create other source: %v", err)
	}
	if err := st.UpsertEvents(context.Background(), otherID, otherSource.ID, []calendar.NormalizedEvent{{
		ExternalID: "evt-other-person-notes",
		Title:      "Other person notes",
		StartsAt:   noteEventHandlerTestBase,
		EndsAt:     noteEventHandlerTestBase.Add(15 * time.Minute),
		Attendees:  []model.Attendee{{Email: "PERSON.ACTIVITY@EXAMPLE.COM", Name: "Person"}},
	}}); err != nil {
		t.Fatalf("upsert other event: %v", err)
	}
	otherEvents, err := st.ListEvents(context.Background(), otherID, noteEventHandlerTestBase.Add(-time.Hour), noteEventHandlerTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("list other events: %v", err)
	}
	otherOwnerNote, err := st.CreateNote(context.Background(), otherID, "Other owner note")
	if err != nil {
		t.Fatalf("create other owner note: %v", err)
	}
	if err := st.SetNoteEvent(context.Background(), otherID, otherOwnerNote.ID, otherEvents[0].ID); err != nil {
		t.Fatalf("set other owner note event: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/people/"+person.ID+"/notes", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("notes status %d body %s", rec.Code, rec.Body)
	}
	var notes []model.Note
	if err := json.NewDecoder(rec.Body).Decode(&notes); err != nil {
		t.Fatalf("decode notes response: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d: %+v", len(notes), notes)
	}
	if notes[0].CreatedAt.Before(notes[1].CreatedAt) {
		t.Fatalf("notes not ordered by created_at desc: %+v", notes)
	}
	seenEvent := false
	seenAlias := false
	for _, note := range notes {
		if note.OwnerID != ownerID {
			t.Fatalf("found note for wrong owner: %+v", note)
		}
		if note.ID == otherOwnerNote.ID {
			t.Fatalf("cross-owner note leaked into person notes: %+v", note)
		}
		switch note.ID {
		case eventNote.ID:
			seenEvent = true
			if note.Snippet == "" || len(note.Tags) != 1 || len(note.FolderIDs) != 1 {
				t.Fatalf("expected completed event note payload, got %+v", note)
			}
		case aliasNote.ID:
			seenAlias = true
			if note.Snippet == "" || note.Tags == nil || note.FolderIDs == nil {
				t.Fatalf("expected completed alias note payload, got %+v", note)
			}
		default:
			t.Fatalf("unexpected note returned: %+v", note)
		}
	}
	if !seenEvent || !seenAlias {
		t.Fatalf("missing expected notes: event=%v alias=%v", seenEvent, seenAlias)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/people/"+emptyPerson.ID+"/notes", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty person notes status %d body %s", rec.Code, rec.Body)
	}
	var emptyNotes []model.Note
	if err := json.NewDecoder(rec.Body).Decode(&emptyNotes); err != nil {
		t.Fatalf("decode empty notes: %v", err)
	}
	if emptyNotes == nil || len(emptyNotes) != 0 {
		t.Fatalf("expected empty array, got %+v", emptyNotes)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/people/"+person.ID+"/notes", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner notes status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/people/00000000-0000-0000-0000-000000000000/notes", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing person notes status %d, want 404; body %s", rec.Code, rec.Body)
	}
}

// TestPeopleRefresh is DB-backed and CI-only: it verifies the endpoint
// returns 401 without auth and 202 once authenticated, but it does not wait
// for the background derivation goroutine to finish.
func TestPeopleRefresh(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	_, ownerHdr := peopleAuthHeader(t, st, "people-refresh@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/people/refresh", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated refresh status %d, want 401; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/people/refresh", nil, ownerHdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("authenticated refresh status %d, want 202; body %s", rec.Code, rec.Body)
	}
	var accepted struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if accepted.Status != "accepted" {
		t.Fatalf("refresh status field = %q, want accepted", accepted.Status)
	}
}

func TestCompaniesListOwnerScopingAndPeopleCount(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "companies-owner@example.com")
	otherID, _ := peopleAuthHeader(t, st, "companies-other@example.com")

	_, err := st.UpsertCompany(context.Background(), ownerID, "zero.example", "Zero")
	if err != nil {
		t.Fatalf("upsert empty company: %v", err)
	}
	oneCompany, err := st.UpsertCompany(context.Background(), ownerID, "one.example", "One")
	if err != nil {
		t.Fatalf("upsert one company: %v", err)
	}
	twoCompany, err := st.UpsertCompany(context.Background(), ownerID, "two.example", "Two")
	if err != nil {
		t.Fatalf("upsert two company: %v", err)
	}
	if _, err := st.UpsertPerson(context.Background(), ownerID, "alice@example.com", "Alice", &oneCompany.ID); err != nil {
		t.Fatalf("upsert one person: %v", err)
	}
	if _, err := st.UpsertPerson(context.Background(), ownerID, "bob@example.com", "Bob", &twoCompany.ID); err != nil {
		t.Fatalf("upsert two person 1: %v", err)
	}
	if _, err := st.UpsertPerson(context.Background(), ownerID, "bobby@example.com", "Bobby", &twoCompany.ID); err != nil {
		t.Fatalf("upsert two person 2: %v", err)
	}
	otherCompany, err := st.UpsertCompany(context.Background(), otherID, "other.example", "Other")
	if err != nil {
		t.Fatalf("upsert other company: %v", err)
	}
	if _, err := st.UpsertPerson(context.Background(), otherID, "outside@example.com", "Outside", &otherCompany.ID); err != nil {
		t.Fatalf("upsert other person's person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/companies", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list companies status %d body %s", rec.Code, rec.Body)
	}
	var listed []struct {
		model.Company
		PeopleCount int64 `json:"people_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode companies response: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 companies, got %d: %+v", len(listed), listed)
	}
	if listed[0].Domain != "one.example" || listed[0].PeopleCount != 1 {
		t.Fatalf("unexpected first company: %+v", listed[0])
	}
	if listed[1].Domain != "two.example" || listed[1].PeopleCount != 2 {
		t.Fatalf("unexpected second company: %+v", listed[1])
	}
	if listed[2].Domain != "zero.example" || listed[2].PeopleCount != 0 {
		t.Fatalf("unexpected third company: %+v", listed[2])
	}
	for _, company := range listed {
		if company.OwnerID != ownerID {
			t.Fatalf("found company for wrong owner: %+v", company.Company)
		}
	}

	if rec = doJSON(t, srv, http.MethodGet, "/api/companies/"+twoCompany.ID, nil, ownerHdr); rec.Code != http.StatusOK {
		t.Fatalf("get company status %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		model.Company
		People []model.Person `json:"people"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode company response: %v", err)
	}
	if got.ID != twoCompany.ID || got.OwnerID != ownerID {
		t.Fatalf("unexpected company payload: %+v", got.Company)
	}
	if len(got.People) != 2 {
		t.Fatalf("expected 2 people, got %d: %+v", len(got.People), got.People)
	}
	if got.People[0].DisplayName != "Bob" || got.People[1].DisplayName != "Bobby" {
		t.Fatalf("unexpected people ordering: %+v", got.People)
	}
	for _, person := range got.People {
		if person.CompanyID == nil || *person.CompanyID != twoCompany.ID {
			t.Fatalf("unexpected person company: %+v", person)
		}
	}
}

func TestCompaniesGetOwnerScopingAndValidation(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "companies-get-owner@example.com")
	_, otherHdr := peopleAuthHeader(t, st, "companies-get-other@example.com")

	company, err := st.UpsertCompany(context.Background(), ownerID, "example.com", "Example")
	if err != nil {
		t.Fatalf("upsert company: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/companies/"+company.ID, nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner get status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/companies/00000000-0000-0000-0000-000000000000", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing company status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/companies/not-a-uuid", nil, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status %d, want 400; body %s", rec.Code, rec.Body)
	}
}

func TestCompaniesMergeMovesPeopleAndReturnsSurvivor(t *testing.T) {
	t.Parallel()
	srv, st := newPeopleTestServer(t)
	ownerID, ownerHdr := peopleAuthHeader(t, st, "companies-merge-owner@example.com")
	_, otherHdr := peopleAuthHeader(t, st, "companies-merge-other@example.com")
	otherOwnerID, _ := peopleAuthHeader(t, st, "companies-merge-third@example.com")

	fromCompany, err := st.UpsertCompany(context.Background(), ownerID, "from.example", "From")
	if err != nil {
		t.Fatalf("upsert from company: %v", err)
	}
	intoCompany, err := st.UpsertCompany(context.Background(), ownerID, "into.example", "Into")
	if err != nil {
		t.Fatalf("upsert into company: %v", err)
	}
	otherCompany, err := st.UpsertCompany(context.Background(), otherOwnerID, "other.example", "Other")
	if err != nil {
		t.Fatalf("upsert other company: %v", err)
	}
	if _, err := st.UpsertPerson(context.Background(), ownerID, "alice@from.example", "Alice", &fromCompany.ID); err != nil {
		t.Fatalf("upsert from person: %v", err)
	}
	if _, err := st.UpsertPerson(context.Background(), ownerID, "bob@into.example", "Bob", &intoCompany.ID); err != nil {
		t.Fatalf("upsert into person: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/companies/"+fromCompany.ID+"/merge", map[string]string{"into": intoCompany.ID}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("merge status %d body %s", rec.Code, rec.Body)
	}
	var merged struct {
		model.Company
		People []model.Person `json:"people"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&merged); err != nil {
		t.Fatalf("decode merge response: %v", err)
	}
	if merged.ID != intoCompany.ID || merged.OwnerID != ownerID {
		t.Fatalf("unexpected surviving company: %+v", merged.Company)
	}
	if len(merged.People) != 2 {
		t.Fatalf("expected 2 people after merge, got %d: %+v", len(merged.People), merged.People)
	}
	for _, person := range merged.People {
		if person.CompanyID == nil || *person.CompanyID != intoCompany.ID {
			t.Fatalf("merged person not repointed: %+v", person)
		}
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/companies/"+fromCompany.ID+"/merge", map[string]string{"into": fromCompany.ID}, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("same-company merge status %d, want 400; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/companies/"+fromCompany.ID+"/merge", map[string]string{}, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing body merge status %d, want 400; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/companies/"+fromCompany.ID+"/merge", map[string]string{"into": otherCompany.ID}, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner merge status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/companies/"+fromCompany.ID+"/merge", map[string]string{"into": "00000000-0000-0000-0000-000000000000"}, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing target merge status %d, want 404; body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/companies/"+fromCompany.ID+"/merge", map[string]string{"into": intoCompany.ID}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner source merge status %d, want 404; body %s", rec.Code, rec.Body)
	}
}
