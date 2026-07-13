package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func newDigestTestServer(t *testing.T) *api.Server {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	return api.NewServer(api.Deps{Store: st, Crypto: cr})
}

func digestAuthHeader(t *testing.T, srv *api.Server, email string) map[string]string {
	t.Helper()
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return map[string]string{"Authorization": "Bearer " + login.Token}
}

func TestDigestConfigGetAndUpdate(t *testing.T) {
	t.Parallel()

	srv := newDigestTestServer(t)
	hdr := digestAuthHeader(t, srv, "digest-owner@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/digest/config", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		OwnerID string `json:"owner_id"`
		Cadence string `json:"cadence"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Cadence != "off" || got.OwnerID == "" {
		t.Fatalf("GET body = %+v, want default off config", got)
	}

	rec = doRaw(t, srv, http.MethodPut, "/api/digest/config", "{", hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed PUT status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPut, "/api/digest/config", map[string]string{"cadence": "hourly"}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cadence status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPut, "/api/digest/config", map[string]string{"cadence": "daily"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status %d body %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if got.Cadence != "daily" {
		t.Fatalf("updated cadence = %q, want daily", got.Cadence)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/digest/config", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET after update status %d body %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode updated get: %v", err)
	}
	if got.Cadence != "daily" {
		t.Fatalf("GET after update cadence = %q, want daily", got.Cadence)
	}
}

func TestDigestConfigRequiresAuth(t *testing.T) {
	t.Parallel()

	srv := newDigestTestServer(t)
	rec := doJSON(t, srv, http.MethodGet, "/api/digest/config", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
