package store_test

import (
	"context"
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
