package model

import (
	"testing"
	"time"
)

func TestCompanyJSONRoundTrip(t *testing.T) {
	t.Parallel()

	value := Company{
		ID:        "company_1",
		OwnerID:   "owner_1",
		Domain:    "example.com",
		Name:      "Example Inc",
		CreatedAt: time.Date(2026, time.July, 11, 8, 15, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC),
	}

	assertJSONRoundTrip(t, value, []string{"id", "owner_id", "domain", "name", "created_at", "updated_at"})
}

func TestPersonJSONRoundTripWithCompanyID(t *testing.T) {
	t.Parallel()

	companyID := "company_1"
	value := Person{
		ID:           "person_1",
		OwnerID:      "owner_1",
		PrimaryEmail: "jane@example.com",
		DisplayName:  "Jane Example",
		CompanyID:    &companyID,
		FirstSeenAt:  time.Date(2026, time.July, 11, 9, 30, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC),
	}

	got := assertJSONRoundTrip(t, value, []string{"id", "owner_id", "primary_email", "display_name", "company_id", "first_seen_at", "updated_at"})

	if got.CompanyID == nil {
		t.Fatal("CompanyID = nil, want non-nil")
	}
	if *got.CompanyID != companyID {
		t.Fatalf("CompanyID = %q, want %q", *got.CompanyID, companyID)
	}
}

func TestPersonJSONRoundTripWithoutCompanyID(t *testing.T) {
	t.Parallel()

	value := Person{
		ID:           "person_2",
		OwnerID:      "owner_1",
		PrimaryEmail: "jane@example.com",
		DisplayName:  "Jane Example",
		CompanyID:    nil,
		FirstSeenAt:  time.Date(2026, time.July, 11, 9, 30, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC),
	}

	got := assertJSONRoundTrip(t, value, []string{"id", "owner_id", "primary_email", "display_name", "first_seen_at", "updated_at"})

	if got.CompanyID != nil {
		t.Fatalf("CompanyID = %#v, want nil", got.CompanyID)
	}
}
