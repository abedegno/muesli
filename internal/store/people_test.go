package store_test

import (
	"context"
	"errors"
	"testing"

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
