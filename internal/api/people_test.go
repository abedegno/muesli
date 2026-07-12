package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
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
