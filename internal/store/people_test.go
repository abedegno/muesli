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
	otherCompany, err := st.UpsertCompany(ctx, otherOwner.ID, "two.example", "Other")
	if err != nil {
		t.Fatalf("create other company: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, otherOwner.ID, "outside@example.com", "Outside", ptrString(otherCompany.ID)); err != nil {
		t.Fatalf("seed other owner's person: %v", err)
	}

	listed, err := st.ListCompaniesWithPeopleCount(ctx, ownerID, "")
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

	byName, err := st.ListCompaniesWithPeopleCount(ctx, ownerID, "tWo")
	if err != nil {
		t.Fatalf("list companies by name search: %v", err)
	}
	if len(byName) != 1 || byName[0].ID != companyTwo.ID {
		t.Fatalf("expected name search to return company two, got %+v", byName)
	}

	byDomain, err := st.ListCompaniesWithPeopleCount(ctx, ownerID, "WO.EX")
	if err != nil {
		t.Fatalf("list companies by domain search: %v", err)
	}
	if len(byDomain) != 1 || byDomain[0].ID != companyTwo.ID {
		t.Fatalf("expected domain search to return company two, got %+v", byDomain)
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

func TestPeopleStoreMergeCompanies(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	fromCompany, err := st.UpsertCompany(ctx, ownerID, "from.example", "From")
	if err != nil {
		t.Fatalf("create from company: %v", err)
	}
	intoCompany, err := st.UpsertCompany(ctx, ownerID, "into.example", "Into")
	if err != nil {
		t.Fatalf("create into company: %v", err)
	}
	fromPersonOne, err := st.UpsertPerson(ctx, ownerID, "one@from.example", "One", ptrString(fromCompany.ID))
	if err != nil {
		t.Fatalf("create from person one: %v", err)
	}
	fromPersonTwo, err := st.UpsertPerson(ctx, ownerID, "two@from.example", "Two", ptrString(fromCompany.ID))
	if err != nil {
		t.Fatalf("create from person two: %v", err)
	}
	intoPerson, err := st.UpsertPerson(ctx, ownerID, "one@into.example", "Into", ptrString(intoCompany.ID))
	if err != nil {
		t.Fatalf("create into person: %v", err)
	}

	if err := st.MergeCompanies(ctx, ownerID, fromCompany.ID, intoCompany.ID); err != nil {
		t.Fatalf("merge companies: %v", err)
	}

	if _, err := st.GetCompany(ctx, ownerID, fromCompany.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("from company should be deleted, got %v", err)
	}
	merged, err := st.GetCompany(ctx, ownerID, intoCompany.ID)
	if err != nil {
		t.Fatalf("get into company after merge: %v", err)
	}
	if merged.ID != intoCompany.ID {
		t.Fatalf("unexpected surviving company: %+v", merged)
	}

	people, err := st.ListPeopleByCompany(ctx, ownerID, intoCompany.ID)
	if err != nil {
		t.Fatalf("list people by merged company: %v", err)
	}
	if len(people) != 3 {
		t.Fatalf("expected 3 people after merge, got %d: %+v", len(people), people)
	}
	for _, person := range people {
		if person.CompanyID == nil || *person.CompanyID != intoCompany.ID {
			t.Fatalf("person not repointed to surviving company: %+v", person)
		}
	}

	if got, err := st.GetPerson(ctx, ownerID, fromPersonOne.ID); err != nil {
		t.Fatalf("get merged person one: %v", err)
	} else if got.CompanyID == nil || *got.CompanyID != intoCompany.ID {
		t.Fatalf("person one company mismatch: %+v", got)
	}
	if got, err := st.GetPerson(ctx, ownerID, fromPersonTwo.ID); err != nil {
		t.Fatalf("get merged person two: %v", err)
	} else if got.CompanyID == nil || *got.CompanyID != intoCompany.ID {
		t.Fatalf("person two company mismatch: %+v", got)
	}
	if got, err := st.GetPerson(ctx, ownerID, intoPerson.ID); err != nil {
		t.Fatalf("get surviving person: %v", err)
	} else if got.CompanyID == nil || *got.CompanyID != intoCompany.ID {
		t.Fatalf("surviving person company mismatch: %+v", got)
	}

	otherOwner, err := st.CreateUser(ctx, "merge-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	otherCompany, err := st.UpsertCompany(ctx, otherOwner.ID, "other.example", "Other")
	if err != nil {
		t.Fatalf("create other company: %v", err)
	}

	if err := st.MergeCompanies(ctx, ownerID, fromCompany.ID, otherCompany.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner into company: want ErrNotFound, got %v", err)
	}
	if err := st.MergeCompanies(ctx, otherOwner.ID, otherCompany.ID, intoCompany.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner from company: want ErrNotFound, got %v", err)
	}
	if err := st.MergeCompanies(ctx, ownerID, fromCompany.ID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing into company: want ErrNotFound, got %v", err)
	}
	if err := st.MergeCompanies(ctx, ownerID, "00000000-0000-0000-0000-000000000000", intoCompany.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing from company: want ErrNotFound, got %v", err)
	}
	if err := st.MergeCompanies(ctx, ownerID, intoCompany.ID, intoCompany.ID); !errors.Is(err, store.ErrInvalidMerge) {
		t.Fatalf("same-company merge: want ErrInvalidMerge, got %v", err)
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
	if _, err := st.UpsertPerson(ctx, otherOwner.ID, "outside@example.com", "Amy Outside", nil); err != nil {
		t.Fatalf("upsert other owner's person: %v", err)
	}

	listed, err := st.ListPeople(ctx, ownerID, "")
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

	byName, err := st.ListPeople(ctx, ownerID, "aMy")
	if err != nil {
		t.Fatalf("list people by name search: %v", err)
	}
	if len(byName) != 2 {
		t.Fatalf("expected 2 amy matches, got %d: %+v", len(byName), byName)
	}
	for _, p := range byName {
		if p.DisplayName != "Amy" {
			t.Fatalf("unexpected name search result: %+v", p)
		}
	}

	byEmail, err := st.ListPeople(ctx, ownerID, "mIxEd@ExAmPlE")
	if err != nil {
		t.Fatalf("list people by email search: %v", err)
	}
	if len(byEmail) != 1 || byEmail[0].PrimaryEmail != "mixed@example.com" {
		t.Fatalf("expected mixed email match, got %+v", byEmail)
	}

	otherList, err := st.ListPeople(ctx, otherOwner.ID, "")
	if err != nil {
		t.Fatalf("list other owner's people: %v", err)
	}
	if len(otherList) != 1 || otherList[0].OwnerID != otherOwner.ID {
		t.Fatalf("unexpected other owner's list: %+v", otherList)
	}
}

func TestPeopleStoreUpdatePerson(t *testing.T) {
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
	person, err := st.UpsertPerson(ctx, ownerID, "alice@example.com", "Alice", ptrString(companyOne.ID))
	if err != nil {
		t.Fatalf("create person: %v", err)
	}

	updated, err := st.UpdatePerson(ctx, ownerID, person.ID, ptrString("Alicia"), nil, false)
	if err != nil {
		t.Fatalf("update display name: %v", err)
	}
	if updated.DisplayName != "Alicia" {
		t.Fatalf("expected display name to change, got %+v", updated)
	}
	if updated.CompanyID == nil || *updated.CompanyID != companyOne.ID {
		t.Fatalf("display-name-only update should keep company, got %+v", updated.CompanyID)
	}

	updated, err = st.UpdatePerson(ctx, ownerID, person.ID, nil, ptrString(companyTwo.ID), false)
	if err != nil {
		t.Fatalf("update company: %v", err)
	}
	if updated.DisplayName != "Alicia" {
		t.Fatalf("company-only update should keep display name, got %+v", updated)
	}
	if updated.CompanyID == nil || *updated.CompanyID != companyTwo.ID {
		t.Fatalf("expected company to change, got %+v", updated.CompanyID)
	}

	updated, err = st.UpdatePerson(ctx, ownerID, person.ID, nil, nil, true)
	if err != nil {
		t.Fatalf("clear company: %v", err)
	}
	if updated.CompanyID != nil {
		t.Fatalf("expected company to clear, got %+v", updated.CompanyID)
	}

	otherOwner, err := st.CreateUser(ctx, "other-update@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if _, err := st.UpdatePerson(ctx, otherOwner.ID, person.ID, ptrString("X"), nil, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner update: want ErrNotFound, got %v", err)
	}
	if _, err := st.UpdatePerson(ctx, ownerID, "00000000-0000-0000-0000-000000000000", ptrString("X"), nil, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing update: want ErrNotFound, got %v", err)
	}
}

func TestPeopleStoreMergePeople(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	fromPerson, err := st.UpsertPerson(ctx, ownerID, "from@example.com", "From", nil)
	if err != nil {
		t.Fatalf("create from person: %v", err)
	}
	intoPerson, err := st.UpsertPerson(ctx, ownerID, "into@example.com", "Into", nil)
	if err != nil {
		t.Fatalf("create into person: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, "Alias note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", "From speaker"); err != nil {
		t.Fatalf("upsert alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, note.ID, "SPEAKER_00", &fromPerson.ID); err != nil {
		t.Fatalf("set alias person: %v", err)
	}

	merged, err := st.MergePeople(ctx, ownerID, fromPerson.ID, intoPerson.ID)
	if err != nil {
		t.Fatalf("merge people: %v", err)
	}
	if merged.ID != intoPerson.ID {
		t.Fatalf("expected surviving person to be returned, got %+v", merged)
	}
	if _, err := st.GetPerson(ctx, ownerID, fromPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("from person should be deleted, got %v", err)
	}
	if got, err := st.GetPerson(ctx, ownerID, intoPerson.ID); err != nil {
		t.Fatalf("get into person: %v", err)
	} else if got.ID != intoPerson.ID {
		t.Fatalf("unexpected into person: %+v", got)
	}

	aliases, err := st.ListSpeakerAliases(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias after merge, got %d: %+v", len(aliases), aliases)
	}
	if aliases[0].PersonID == nil || *aliases[0].PersonID != intoPerson.ID {
		t.Fatalf("expected alias to point at survivor, got %+v", aliases[0])
	}
	if gotNote, err := st.GetNote(ctx, ownerID, note.ID); err != nil {
		t.Fatalf("note should survive merge: %v", err)
	} else if gotNote.ID != note.ID {
		t.Fatalf("unexpected note after merge: %+v", gotNote)
	}

	if _, err := st.MergePeople(ctx, ownerID, fromPerson.ID, fromPerson.ID); !errors.Is(err, store.ErrInvalidMerge) {
		t.Fatalf("same-person merge: want ErrInvalidMerge, got %v", err)
	}

	otherOwner, err := st.CreateUser(ctx, "merge-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	otherPerson, err := st.UpsertPerson(ctx, otherOwner.ID, "other@example.com", "Other", nil)
	if err != nil {
		t.Fatalf("create other person: %v", err)
	}
	if _, err := st.MergePeople(ctx, otherOwner.ID, otherPerson.ID, intoPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner source merge: want ErrNotFound, got %v", err)
	}
	if _, err := st.MergePeople(ctx, ownerID, intoPerson.ID, otherPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner target merge: want ErrNotFound, got %v", err)
	}
}

func TestPeopleStoreDeletePerson(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	person, err := st.UpsertPerson(ctx, ownerID, "delete@example.com", "Delete", nil)
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, "Delete note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", "Delete speaker"); err != nil {
		t.Fatalf("upsert alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, note.ID, "SPEAKER_00", &person.ID); err != nil {
		t.Fatalf("set alias person: %v", err)
	}

	if err := st.DeletePerson(ctx, ownerID, person.ID); err != nil {
		t.Fatalf("delete person: %v", err)
	}
	if _, err := st.GetPerson(ctx, ownerID, person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted person should be gone, got %v", err)
	}
	aliases, err := st.ListSpeakerAliases(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list aliases after delete: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected alias row to remain, got %+v", aliases)
	}
	if aliases[0].PersonID != nil {
		t.Fatalf("expected alias person link to be cleared, got %+v", aliases[0])
	}
	if gotNote, err := st.GetNote(ctx, ownerID, note.ID); err != nil {
		t.Fatalf("note should survive delete: %v", err)
	} else if gotNote.ID != note.ID {
		t.Fatalf("unexpected note after delete: %+v", gotNote)
	}

	otherOwner, err := st.CreateUser(ctx, "delete-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if err := st.DeletePerson(ctx, otherOwner.ID, person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner delete: want ErrNotFound, got %v", err)
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

func TestPeopleStoreUpdatePersonPartialAndCompanyClear(t *testing.T) {
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
	person, err := st.UpsertPerson(ctx, ownerID, "person@example.com", "Original", ptrString(companyOne.ID))
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}

	renamed, err := st.UpdatePerson(ctx, ownerID, person.ID, ptrString("Renamed"), nil, false)
	if err != nil {
		t.Fatalf("rename person: %v", err)
	}
	if renamed.DisplayName != "Renamed" {
		t.Fatalf("unexpected renamed person: %+v", renamed)
	}
	if renamed.CompanyID == nil || *renamed.CompanyID != companyOne.ID {
		t.Fatalf("display-name-only patch should keep company, got %+v", renamed.CompanyID)
	}

	reassigned, err := st.UpdatePerson(ctx, ownerID, person.ID, nil, ptrString(companyTwo.ID), false)
	if err != nil {
		t.Fatalf("reassign company: %v", err)
	}
	if reassigned.DisplayName != "Renamed" {
		t.Fatalf("company-only patch should keep display name, got %+v", reassigned)
	}
	if reassigned.CompanyID == nil || *reassigned.CompanyID != companyTwo.ID {
		t.Fatalf("expected reassigned company, got %+v", reassigned.CompanyID)
	}

	cleared, err := st.UpdatePerson(ctx, ownerID, person.ID, nil, nil, true)
	if err != nil {
		t.Fatalf("clear company: %v", err)
	}
	if cleared.CompanyID != nil {
		t.Fatalf("company_id:null should clear company, got %+v", cleared.CompanyID)
	}

	otherOwner, err := st.CreateUser(ctx, "other-update@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if _, err := st.UpdatePerson(ctx, otherOwner.ID, person.ID, ptrString("Other"), nil, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner update: want ErrNotFound, got %v", err)
	}
	if _, err := st.UpdatePerson(ctx, ownerID, "00000000-0000-0000-0000-000000000000", ptrString("Missing"), nil, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing person update: want ErrNotFound, got %v", err)
	}
	if _, err := st.UpdatePerson(ctx, ownerID, person.ID, nil, ptrString("00000000-0000-0000-0000-000000000000"), false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing company update: want ErrNotFound, got %v", err)
	}
}

func TestPeopleStoreMergePeopleRepointsAliasesAndDeletesLoser(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	survivor, err := st.UpsertPerson(ctx, ownerID, "survivor@example.com", "Survivor", nil)
	if err != nil {
		t.Fatalf("seed survivor: %v", err)
	}
	loser, err := st.UpsertPerson(ctx, ownerID, "loser@example.com", "Loser", nil)
	if err != nil {
		t.Fatalf("seed loser: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, "Meeting")
	if err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", "Loser"); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, note.ID, "SPEAKER_00", ptrString(loser.ID)); err != nil {
		t.Fatalf("link alias to loser: %v", err)
	}

	merged, err := st.MergePeople(ctx, ownerID, loser.ID, survivor.ID)
	if err != nil {
		t.Fatalf("merge people: %v", err)
	}
	if merged.ID != survivor.ID {
		t.Fatalf("expected surviving person %q, got %+v", survivor.ID, merged)
	}
	if _, err := st.GetPerson(ctx, ownerID, loser.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("loser should be removed, got %v", err)
	}

	aliases, err := st.ListSpeakerAliases(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d: %+v", len(aliases), aliases)
	}
	if aliases[0].PersonID == nil || *aliases[0].PersonID != survivor.ID {
		t.Fatalf("expected alias repointed to survivor, got %+v", aliases[0])
	}

	if _, err := st.GetNote(ctx, ownerID, note.ID); err != nil {
		t.Fatalf("note should remain after merge: %v", err)
	}

	otherOwner, err := st.CreateUser(ctx, "other-merge@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	otherPerson, err := st.UpsertPerson(ctx, otherOwner.ID, "other@example.com", "Other", nil)
	if err != nil {
		t.Fatalf("seed other person's person: %v", err)
	}
	if _, err := st.MergePeople(ctx, ownerID, loser.ID, otherPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner merge target: want ErrNotFound, got %v", err)
	}
	if _, err := st.MergePeople(ctx, otherOwner.ID, otherPerson.ID, survivor.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner merge source: want ErrNotFound, got %v", err)
	}
	if _, err := st.MergePeople(ctx, ownerID, "00000000-0000-0000-0000-000000000000", survivor.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing source person: want ErrNotFound, got %v", err)
	}
	if _, err := st.MergePeople(ctx, ownerID, loser.ID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing target person: want ErrNotFound, got %v", err)
	}
	if _, err := st.MergePeople(ctx, ownerID, survivor.ID, survivor.ID); !errors.Is(err, store.ErrInvalidMerge) {
		t.Fatalf("same-person merge: want ErrInvalidMerge, got %v", err)
	}
}

func TestPeopleStoreDeletePersonUnlinksAliases(t *testing.T) {
	st, ownerID, _ := newPeopleStoreWithOwner(t)
	ctx := context.Background()

	person, err := st.UpsertPerson(ctx, ownerID, "delete@example.com", "Delete Me", nil)
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}
	note, err := st.CreateNote(ctx, ownerID, "Meeting")
	if err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", "Delete Me"); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, ownerID, note.ID, "SPEAKER_00", ptrString(person.ID)); err != nil {
		t.Fatalf("link alias to person: %v", err)
	}

	if err := st.DeletePerson(ctx, ownerID, person.ID); err != nil {
		t.Fatalf("delete person: %v", err)
	}
	if _, err := st.GetPerson(ctx, ownerID, person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted person should be missing, got %v", err)
	}

	aliases, err := st.ListSpeakerAliases(ctx, ownerID, note.ID)
	if err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d: %+v", len(aliases), aliases)
	}
	if aliases[0].PersonID != nil {
		t.Fatalf("person delete should set alias person_id null, got %+v", aliases[0])
	}

	if _, err := st.GetNote(ctx, ownerID, note.ID); err != nil {
		t.Fatalf("note should remain after delete: %v", err)
	}

	otherOwner, err := st.CreateUser(ctx, "other-delete@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	otherPerson, err := st.UpsertPerson(ctx, otherOwner.ID, "other@example.com", "Other", nil)
	if err != nil {
		t.Fatalf("seed other person: %v", err)
	}
	if err := st.DeletePerson(ctx, otherOwner.ID, person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner delete: want ErrNotFound, got %v", err)
	}
	if err := st.DeletePerson(ctx, ownerID, otherPerson.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing owner delete target: want ErrNotFound, got %v", err)
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

	fullPeople, err := st.ListPeople(ctx, owner.ID, "")
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

// This test is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.
func TestNotesForPersonIncludesEventAttendeesAndSpeakerAliases(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "activity-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	otherOwner, err := st.CreateUser(ctx, "activity-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other owner: %v", err)
	}

	person, err := st.UpsertPerson(ctx, owner.ID, "person.activity@example.com", "Person", nil)
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	if _, err := st.UpsertPerson(ctx, otherOwner.ID, "person.activity@example.com", "Person", nil); err != nil {
		t.Fatalf("create other person's person: %v", err)
	}

	source, err := st.CreateSource(ctx, owner.ID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := st.UpsertEvents(ctx, owner.ID, source.ID, []calendar.NormalizedEvent{{
		ExternalID: "evt-activity",
		Title:      "Activity sync",
		StartsAt:   noteEventTestBase,
		EndsAt:     noteEventTestBase.Add(30 * time.Minute),
		Attendees: []model.Attendee{
			{Email: "PERSON.ACTIVITY@EXAMPLE.COM", Name: "Person"},
		},
	}}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	events, err := st.ListEvents(ctx, owner.ID, noteEventTestBase.Add(-time.Hour), noteEventTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	eventNote, err := st.CreateNote(ctx, owner.ID, "Event note")
	if err != nil {
		t.Fatalf("create event note: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, owner.ID, eventNote.ID, "event body"); err != nil {
		t.Fatalf("update event note body: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, owner.ID, eventNote.ID, "work"); err != nil {
		t.Fatalf("add event note tag: %v", err)
	}
	folder, err := st.CreateFolder(ctx, owner.ID, "Projects", nil)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := st.AddNoteFolder(ctx, owner.ID, eventNote.ID, folder.ID); err != nil {
		t.Fatalf("add event note folder: %v", err)
	}
	if err := st.SetNoteEvent(ctx, owner.ID, eventNote.ID, events[0].ID); err != nil {
		t.Fatalf("set event note event: %v", err)
	}

	aliasNote, err := st.CreateNote(ctx, owner.ID, "Alias note")
	if err != nil {
		t.Fatalf("create alias note: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, owner.ID, aliasNote.ID, "alias body"); err != nil {
		t.Fatalf("update alias note body: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, owner.ID, aliasNote.ID, "SPEAKER_00", "Person"); err != nil {
		t.Fatalf("upsert alias: %v", err)
	}
	if err := st.SetSpeakerAliasPerson(ctx, owner.ID, aliasNote.ID, "SPEAKER_00", &person.ID); err != nil {
		t.Fatalf("set alias person: %v", err)
	}

	otherSource, err := st.CreateSource(ctx, otherOwner.ID, "ics", "Other Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create other source: %v", err)
	}
	if err := st.UpsertEvents(ctx, otherOwner.ID, otherSource.ID, []calendar.NormalizedEvent{{
		ExternalID: "evt-other",
		Title:      "Other activity",
		StartsAt:   noteEventTestBase,
		EndsAt:     noteEventTestBase.Add(15 * time.Minute),
		Attendees: []model.Attendee{
			{Email: "PERSON.ACTIVITY@EXAMPLE.COM", Name: "Person"},
		},
	}}); err != nil {
		t.Fatalf("upsert other event: %v", err)
	}
	otherEvents, err := st.ListEvents(ctx, otherOwner.ID, noteEventTestBase.Add(-time.Hour), noteEventTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("list other events: %v", err)
	}
	otherEventNote, err := st.CreateNote(ctx, otherOwner.ID, "Other event note")
	if err != nil {
		t.Fatalf("create other event note: %v", err)
	}
	if err := st.SetNoteEvent(ctx, otherOwner.ID, otherEventNote.ID, otherEvents[0].ID); err != nil {
		t.Fatalf("set other note event: %v", err)
	}

	notes, err := st.NotesForPerson(ctx, owner.ID, person.ID)
	if err != nil {
		t.Fatalf("notes for person: %v", err)
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
		if note.OwnerID != owner.ID {
			t.Fatalf("found note for wrong owner: %+v", note)
		}
		if note.ID == otherEventNote.ID {
			t.Fatalf("cross-owner note leaked into person activity: %+v", note)
		}
		switch note.ID {
		case eventNote.ID:
			seenEvent = true
			if note.Snippet == "" {
				t.Fatal("expected event note snippet to be populated")
			}
			if len(note.Tags) != 1 || note.Tags[0] != "work" {
				t.Fatalf("unexpected event note tags: %+v", note.Tags)
			}
			if len(note.FolderIDs) != 1 || note.FolderIDs[0] != folder.ID {
				t.Fatalf("unexpected event note folders: %+v", note.FolderIDs)
			}
		case aliasNote.ID:
			seenAlias = true
			if note.Snippet == "" {
				t.Fatal("expected alias note snippet to be populated")
			}
			if note.Tags == nil || note.FolderIDs == nil {
				t.Fatalf("expected complete note slices, got tags=%v folders=%v", note.Tags, note.FolderIDs)
			}
		default:
			t.Fatalf("unexpected note returned: %+v", note)
		}
	}
	if !seenEvent || !seenAlias {
		t.Fatalf("missing expected notes: event=%v alias=%v", seenEvent, seenAlias)
	}

	emptyPerson, err := st.UpsertPerson(ctx, owner.ID, "no-activity@example.com", "Idle", nil)
	if err != nil {
		t.Fatalf("create empty person: %v", err)
	}
	emptyNotes, err := st.NotesForPerson(ctx, owner.ID, emptyPerson.ID)
	if err != nil {
		t.Fatalf("notes for empty person: %v", err)
	}
	if emptyNotes == nil {
		t.Fatal("expected empty non-nil slice, got nil")
	}
	if len(emptyNotes) != 0 {
		t.Fatalf("expected no notes for empty person, got %+v", emptyNotes)
	}
}
