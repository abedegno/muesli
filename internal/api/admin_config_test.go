package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

const testFakeSecret = "fake-super-secret-value-xyz123"

// TestAdminConfigRedactsSecrets hits GET /api/admin/config with a fake
// secret configured (mirroring MUESLI_MASTER_KEY etc.) and asserts the raw
// response body never contains it anywhere — only "(set)"/"(unset)"
// placeholders for secret-shaped fields. Also regression-guards that
// MUESLI_TRASH_RETENTION_DAYS (ADM07) appears like any other field.
// Uses testutil.NewPool per repo convention — CI provides TEST_DATABASE_URL;
// a local run without it skips automatically (this handler itself never
// touches the store, but /api/setup + /api/login do, same as the sibling
// admin_embeddings_test.go).
func TestAdminConfigRedactsSecrets(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))

	cfg := config.Config{
		Addr:                    ":8080",
		MasterKey:               testFakeSecret,
		StorageSigningKey:       testFakeSecret,
		DefaultTranscriberToken: testFakeSecret,
		DefaultAgentToken:       testFakeSecret,
		TrashRetentionDays:      30,
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Config: cfg})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/config", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	if strings.Contains(body, testFakeSecret) {
		t.Fatalf("fake secret leaked into /api/admin/config response: %s", body)
	}

	var entries []struct {
		Name   string `json:"name"`
		EnvVar string `json:"envVar"`
		Value  string `json:"value"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	found := map[string]string{}
	for _, e := range entries {
		found[e.EnvVar] = e.Value
	}
	for _, name := range []string{
		"MUESLI_MASTER_KEY",
		"MUESLI_STORAGE_SIGNING_KEY",
		"MUESLI_DEFAULT_TRANSCRIBER_TOKEN",
		"MUESLI_DEFAULT_AGENT_TOKEN",
	} {
		v, ok := found[name]
		if !ok {
			t.Errorf("expected entry %s in response", name)
			continue
		}
		if v != "(set)" {
			t.Errorf("%s value = %q, want \"(set)\"", name, v)
		}
	}
	if v, ok := found["MUESLI_ADDR"]; !ok || v != ":8080" {
		t.Errorf("MUESLI_ADDR = %q, ok=%v, want \":8080\"", v, ok)
	}
	// ADM07 coordination note regression guard: TrashRetentionDays must
	// appear like every other field, no special-casing.
	if v, ok := found["MUESLI_TRASH_RETENTION_DAYS"]; !ok || v != "30" {
		t.Errorf("MUESLI_TRASH_RETENTION_DAYS = %q, ok=%v, want \"30\"", v, ok)
	}
}

// TestAdminConfigRequiresAuth ensures the route is mounted in the
// authenticated admin route group, not publicly reachable.
func TestAdminConfigRequiresAuth(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Config: config.Config{}})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/config", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without auth; body: %s", rec.Code, rec.Body)
	}
}
