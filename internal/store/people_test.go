package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	peoplelogic "github.com/abedegno/muesli/internal/people"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPeopleStoreWithOwner(t *testing.T) (*store.Store, string, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "people-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return st, u.ID, pool
}

func TestPeopleStoreCompanies(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	company, err := st.UpsertCompany(ctx, ownerID, "example.com", "Example")
	if err != nil {
		t.Fatalf("upsert company: %v", err)
	}
	if company.ID == "" || company.OwnerID != ownerID || company.Domain != "example.com" || company.Name != "Example" {
		t.Fatalf("unexpected company: %+v", company)
	}

	updated, err := st.UpsertCompany(ctx, ownerID, "example.com", "Example LLC")
	if err != nil {
		t.Fatalf("upsert existing company: %v", err)
	}
	if updated.ID != company.ID {
		t.Fatalf("expected upsert to preserve company ID, got %q want %q", updated.ID, company.ID)
	}
	if updated.Name != "Example LLC" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}

	kept, err := st.UpsertCompany(ctx, ownerID, "example.com", "")
	if err != nil {
		t.Fatalf("upsert empty company name: %v", err)
	}
	if kept.ID != company.ID {
		t.Fatalf("expected upsert to preserve company ID, got %q want %q", kept.ID, company.ID)
	}
	if kept.Name != "Example LLC" {
		t.Fatalf("expected existing name to be kept, got %q", kept.Name)
	}

	if _, err := st.UpsertCompany(ctx, ownerID, "acme.com", "Acme"); err != nil {
		t.Fatalf("upsert second company: %v", err)
	}
	if _, err := st.UpsertCompany(ctx, ownerID, "zeta.com", "Zeta"); err != nil {
		t.Fatalf("upsert third company: %v", err)
	}

	listed, err := st.ListCompanies(ctx, ownerID)
	if err != nil {
		t.Fatalf("list companies: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 companies, got %d: %+v", len(listed), listed)
	}
	if listed[0].Domain != "acme.com" || listed[1].Domain != "example.com" || listed[2].Domain != "zeta.com" {
		t.Fatalf("unexpected ordering: %+v", listed)
	}

	otherOwner, err := st.CreateUser(ctx, "other-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if _, err := st.UpsertCompany(ctx, otherOwner.ID, "other.com", "Other"); err != nil {
		t.Fatalf("upsert other owner's company: %v", err)
	}

	listed, err = st.ListCompanies(ctx, ownerID)
	if err != nil {
		t.Fatalf("relist companies: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected owner-scoped list to exclude other owner, got %d: %+v", len(listed), listed)
	}
	for _, company := range listed {
		if company.OwnerID != ownerID {
			t.Fatalf("found company for wrong owner: %+v", company)
		}
	}
}

func TestPeopleStoreCompaniesWithPeopleCountAndCompanyLookup(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	_, err := st.UpsertCompany(ctx, ownerID, "zero.example", "Zero")
	if err != nil {
		t.Fatalf("create zero company: %v", err)
	}
	companyOne, err := st.UpsertCompany(ctx, ownerID, "one.example", "One")
	if err != nil {
		t.Fatalf("create one company: %v", err)
	}
	companyTwo, err := st.UpsertCompany(ctx, ownerID, "two.example", "Two")
	if err != nil {
		t.Fatalf("create two company: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, ownerID, "alice@example.com", "Alice", ptrString(companyOne.ID)); err != nil {
		t.Fatalf("seed one person: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, ownerID, "bob@example.com", "Bob", ptrString(companyTwo.ID)); err != nil {
		t.Fatalf("seed first two person: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, ownerID, "bobby@example.com", "Bobby", ptrString(companyTwo.ID)); err != nil {
		t.Fatalf("seed second two person: %v", err)
	}

	otherOwner, err := st.CreateUser(ctx, "other-count@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	otherCompany, err := st.UpsertCompany(ctx, otherOwner.ID, "other.example", "Other")
	if err != nil {
		t.Fatalf("create other company: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, otherOwner.ID, "outside@example.com", "Outside", ptrString(otherCompany.ID)); err != nil {
		t.Fatalf("seed other owner's person: %v", err)
	}

	listed, err := st.ListCompaniesWithPeopleCount(ctx, ownerID)
	if err != nil {
		t.Fatalf("list companies with counts: %v", err)
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
			t.Fatalf("found company for wrong owner: %+v", company)
		}
	}

	got, err := st.GetCompany(ctx, ownerID, companyTwo.ID)
	if err != nil {
		t.Fatalf("get company: %v", err)
	}
	if got.ID != companyTwo.ID || got.OwnerID != ownerID || got.Domain != companyTwo.Domain || got.Name != companyTwo.Name {
		t.Fatalf("unexpected company lookup: %+v", got)
	}

	people, err := st.ListPeopleByCompany(ctx, ownerID, companyTwo.ID)
	if err != nil {
		t.Fatalf("list people by company: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("expected 2 people for company, got %d: %+v", len(people), people)
	}
	if people[0].DisplayName != "Bob" || people[1].DisplayName != "Bobby" {
		t.Fatalf("unexpected people ordering: %+v", people)
	}
	for _, person := range people {
		if person.CompanyID == nil || *person.CompanyID != companyTwo.ID {
			t.Fatalf("unexpected person company: %+v", person)
		}
	}

	if _, err := st.GetCompany(ctx, ownerID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing company: want ErrNotFound, got %v", err)
	}
	if _, err := st.GetCompany(ctx, otherOwner.ID, companyTwo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner company get: want ErrNotFound, got %v", err)
	}
}

func ptrString(s string) *string {
	return &s
}

func TestPeopleStorePeople(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	companyOne, err := st.UpsertCompany(ctx, ownerID, "one.example", "One")
	if err != nil {
		t.Fatalf("create first company: %v", err)
	}
	companyTwo, err := st.UpsertCompany(ctx, ownerID, "two.example", "Two")
	if err != nil {
		t.Fatalf("create second company: %v", err)
	}

	person, err := st.UpsertPerson(ctx, ownerID, "  Mixed@Example.Com  ", "Alice", ptrString(companyOne.ID))
	if err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	if person.ID == "" || person.OwnerID != ownerID || person.PrimaryEmail != "mixed@example.com" || person.DisplayName != "Alice" {
		t.Fatalf("unexpected person: %+v", person)
	}
	if person.CompanyID == nil || *person.CompanyID != companyOne.ID {
		t.Fatalf("expected first company to be stored, got %+v", person.CompanyID)
	}

	same, err := st.UpsertPerson(ctx, ownerID, "mixed@example.com", "", nil)
	if err != nil {
		t.Fatalf("upsert same person with blank fields: %v", err)
	}
	if same.ID != person.ID {
		t.Fatalf("expected upsert to preserve person ID, got %q want %q", same.ID, person.ID)
	}
	if same.DisplayName != "Alice" {
		t.Fatalf("blank display name should keep existing value, got %q", same.DisplayName)
	}
	if same.CompanyID == nil || *same.CompanyID != companyOne.ID {
		t.Fatalf("nil company should keep existing company, got %+v", same.CompanyID)
	}

	swapped, err := st.UpsertPerson(ctx, ownerID, "mixed@example.com", "", ptrString(companyTwo.ID))
	if err != nil {
		t.Fatalf("upsert same person with new company: %v", err)
	}
	if swapped.ID != person.ID {
		t.Fatalf("expected upsert to preserve person ID, got %q want %q", swapped.ID, person.ID)
	}
	if swapped.DisplayName != "Alice" {
		t.Fatalf("blank display name should keep existing value, got %q", swapped.DisplayName)
	}
	if swapped.CompanyID == nil || *swapped.CompanyID != companyTwo.ID {
		t.Fatalf("non-nil company should overwrite existing company, got %+v", swapped.CompanyID)
	}

	if _, err := st.UpsertPerson(ctx, ownerID, "amy2@example.com", "Amy", nil); err != nil {
		t.Fatalf("upsert amy2: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, ownerID, "amy1@example.com", "Amy", nil); err != nil {
		t.Fatalf("upsert amy1: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, ownerID, "zed@example.com", "Zed", nil); err != nil {
		t.Fatalf("upsert zed: %v", err)
	}

	otherOwner, err := st.CreateUser(ctx, "other-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, otherOwner.ID, "outside@example.com", "Aaron", nil); err != nil {
		t.Fatalf("upsert other owner's person: %v", err)
	}

	listed, err := st.ListPeople(ctx, ownerID)
	if err != nil {
		t.Fatalf("list people: %v", err)
	}
	if len(listed) != 4 {
		t.Fatalf("expected 4 people for owner, got %d: %+v", len(listed), listed)
	}
	if listed[0].DisplayName != "Alice" || listed[0].PrimaryEmail != "mixed@example.com" {
		t.Fatalf("unexpected first person: %+v", listed[0])
	}
	if listed[1].DisplayName != "Amy" || listed[1].PrimaryEmail != "amy1@example.com" {
		t.Fatalf("unexpected second person ordering: %+v", listed[1])
	}
	if listed[2].DisplayName != "Amy" || listed[2].PrimaryEmail != "amy2@example.com" {
		t.Fatalf("unexpected third person ordering: %+v", listed[2])
	}
	if listed[3].DisplayName != "Zed" || listed[3].PrimaryEmail != "zed@example.com" {
		t.Fatalf("unexpected fourth person ordering: %+v", listed[3])
	}
	for _, p := range listed {
		if p.OwnerID != ownerID {
			t.Fatalf("found person for wrong owner: %+v", p)
		}
	}

	otherList, err := st.ListPeople(ctx, otherOwner.ID)
	if err != nil {
		t.Fatalf("list other owner's people: %v", err)
	}
	if len(otherList) != 1 || otherList[0].OwnerID != otherOwner.ID {
		t.Fatalf("unexpected other owner's list: %+v", otherList)
	}
}

func TestPeopleStoreGetPersonNotFound(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	person, err := st.UpsertPerson(ctx, ownerID, "present@example.com", "Present", nil)
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}

	if _, err := st.GetPerson(ctx, ownerID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing person: want ErrNotFound, got %v", err)
	}

	otherOwner, err := st.CreateUser(ctx, "other-get@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if _, err := st.GetPerson(ctx, otherOwner.ID, person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner get: want ErrNotFound, got %v", err)
	}
}

// This test is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.
func TestPeopleForNoteEventScopesSpeakerMatching(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "event-scope-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	first, err := st.UpsertPerson(ctx, owner.ID, "chris.one@example.com", "Chris", nil)
	if err != nil {
		t.Fatalf("create first person: %v", err)
	}
	second, err := st.UpsertPerson(ctx, owner.ID, "chris.two@example.com", "Chris", nil)
	if err != nil {
		t.Fatalf("create second person: %v", err)
	}

	source, err := st.CreateSource(ctx, owner.ID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := st.UpsertEvents(ctx, owner.ID, source.ID, []calendar.NormalizedEvent{{
		ExternalID: "evt-1",
		Title:      "Team sync",
		StartsAt:   noteEventTestBase,
		EndsAt:     noteEventTestBase.Add(30 * time.Minute),
		Attendees: []model.Attendee{
			{Email: "CHRIS.ONE@EXAMPLE.COM", Name: "Chris", Response: "accepted"},
		},
	}}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	events, err := st.ListEvents(ctx, owner.ID, noteEventTestBase.Add(-time.Hour), noteEventTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}

	note, err := st.CreateNote(ctx, owner.ID, "Meeting note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.SetNoteEvent(ctx, owner.ID, note.ID, events[0].ID); err != nil {
		t.Fatalf("set note event: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, owner.ID, note.ID, "SPEAKER_00", "Chris"); err != nil {
		t.Fatalf("upsert speaker alias: %v", err)
	}

	fullPeople, err := st.ListPeople(ctx, owner.ID)
	if err != nil {
		t.Fatalf("list people: %v", err)
	}
	if gotID, ok := peoplelogic.MatchPersonByName("Chris", fullPeople); ok {
		t.Fatalf("expected org-wide matching to stay ambiguous, got %q", gotID)
	}

	scopedPeople, err := st.PeopleForNoteEvent(ctx, owner.ID, note.ID)
	if err != nil {
		t.Fatalf("people for note event: %v", err)
	}
	if len(scopedPeople) != 1 {
		t.Fatalf("expected exactly 1 scoped person, got %d: %+v", len(scopedPeople), scopedPeople)
	}
	if scopedPeople[0].ID != first.ID {
		t.Fatalf("expected scoped person %q, got %+v", first.ID, scopedPeople[0])
	}
	if gotID, ok := peoplelogic.MatchPersonByName("Chris", scopedPeople); !ok || gotID != first.ID {
		t.Fatalf("expected scoped match to resolve to %q, got (%q, %v)", first.ID, gotID, ok)
	}

	if err := peoplelogic.LinkNoteSpeakers(ctx, st, owner.ID, note.ID); err != nil {
		t.Fatalf("link note speakers: %v", err)
	}
	aliases, err := st.ListSpeakerAliases(ctx, owner.ID, note.ID)
	if err != nil {
		t.Fatalf("list speaker aliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d: %+v", len(aliases), aliases)
	}
	if aliases[0].PersonID == nil || *aliases[0].PersonID != first.ID {
		t.Fatalf("expected alias linked to %q, got %+v", first.ID, aliases[0])
	}
	if *aliases[0].PersonID == second.ID {
		t.Fatalf("alias linked to the wrong person: %+v", aliases[0])
	}
}
