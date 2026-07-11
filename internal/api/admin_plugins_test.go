package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func adminServer(t *testing.T) (*api.Server, *store.Store, map[string]string) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return srv, st, map[string]string{"Authorization": "Bearer " + login.Token}
}

func TestAdminPluginsCRUD(t *testing.T) {
	t.Parallel()
	srv, _, hdr := adminServer(t)

	// Create.
	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         "transcriber",
		"name":         "whisper",
		"endpoint_url": "http://transcriber:9000",
		"token":        "plugin-token",
		"config":       map[string]string{"api_key": "sk-secret"},
		"enabled":      true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("expected id")
	}

	// List redacts the secret and never returns the token.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	if bytesContains(rec.Body.Bytes(), "sk-secret") || bytesContains(rec.Body.Bytes(), "plugin-token") {
		t.Fatalf("secret leaked in list: %s", rec.Body)
	}

	// PATCH to set default.
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/plugins/"+created.ID,
		map[string]any{"is_default": true}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status %d body %s", rec.Code, rec.Body)
	}

	// DELETE.
	rec = doJSON(t, srv, http.MethodDelete, "/api/admin/plugins/"+created.ID, nil, hdr)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", rec.Code)
	}

	// Unauthenticated is rejected.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status %d, want 401", rec.Code)
	}
}

func bytesContains(b []byte, sub string) bool {
	return len(sub) == 0 || (len(b) >= len(sub) && func() bool {
		for i := 0; i+len(sub) <= len(b); i++ {
			if string(b[i:i+len(sub)]) == sub {
				return true
			}
		}
		return false
	}())
}

func TestAdminPluginsPasswordFormatRedaction(t *testing.T) {
	// requires TEST_DATABASE_URL — uses adminServer(t) helper
	srv, st, hdr := adminServer(t)

	schema := `{"type":"object","properties":{"api_key":{"type":"string","format":"password"}}}`
	config := `{"api_key":"super-secret"}`

	// Create plugin with format:password schema
	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":          "transcriber",
		"name":          "pw-test-plugin",
		"endpoint_url":  "http://pw-plugin:9000",
		"token":         "tok",
		"config":        json.RawMessage(config),
		"config_schema": json.RawMessage(schema),
		"enabled":       true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// List: value must be redacted, key must be present
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	body := rec.Body.Bytes()
	if bytesContains(body, "super-secret") {
		t.Fatalf("secret leaked in list: %s", body)
	}
	// The field key should be present (partial redaction, not whole-config erasure)
	if !bytesContains(body, "api_key") {
		t.Fatalf("expected api_key key in redacted config, got: %s", body)
	}
	// Redacted placeholder
	if !bytesContains(body, `"*"`) {
		t.Fatalf("expected redacted value '*' in response: %s", body)
	}

	// Raw stored value is intact (decrypt via store directly)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	got, err := st.GetPlugin(context.Background(), cr, created.ID)
	if err != nil {
		t.Fatalf("GetPlugin: %v", err)
	}
	if !bytesContains(got.Config, "super-secret") {
		t.Fatalf("stored value missing after redaction: %s", got.Config)
	}
}

func TestGetPluginStatus(t *testing.T) {
	t.Parallel()

	// Stand-in plugin server that serves GET /status and GET /info.
	fakePlugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"downloading","model":"base","percent":55}`))
		case "/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"test","version":"0.1.0","plugin_api":1,"kind":"transcriber"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fakePlugin.Close)

	srv, _, hdr := adminServer(t)

	// Register a plugin pointing at the fake server.
	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         "transcriber",
		"name":         "status-test",
		"endpoint_url": fakePlugin.URL,
		"token":        "tok",
		"enabled":      true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Call GET /api/admin/plugins/{id}/status.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/plugins/"+created.ID+"/status", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}

	var got struct {
		Status  string `json:"status"`
		Model   string `json:"model"`
		Percent int    `json:"percent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "downloading" {
		t.Errorf("status = %q, want %q", got.Status, "downloading")
	}
	if got.Model != "base" {
		t.Errorf("model = %q, want %q", got.Model, "base")
	}
	if got.Percent != 55 {
		t.Errorf("percent = %d, want 55", got.Percent)
	}
}

func TestGetPluginStatusNotFound(t *testing.T) {
	t.Parallel()
	srv, _, hdr := adminServer(t)

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/plugins/00000000-0000-0000-0000-000000000000/status", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestCheckPluginHealthHealthy(t *testing.T) {
	t.Parallel()

	fakePlugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fakePlugin.Close)

	srv, _, hdr := adminServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         "transcriber",
		"name":         "health-test-ok",
		"endpoint_url": fakePlugin.URL,
		"token":        "tok",
		"enabled":      true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = doJSON(t, srv, http.MethodPost, "/api/admin/plugins/"+created.ID+"/health", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		Healthy bool   `json:"healthy"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Healthy {
		t.Errorf("healthy = false, want true (error=%q)", got.Error)
	}
	if got.Error != "" {
		t.Errorf("error = %q, want empty", got.Error)
	}
}

func TestCheckPluginHealthUnhealthy(t *testing.T) {
	t.Parallel()

	fakePlugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			http.Error(w, "model not loaded", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fakePlugin.Close)

	srv, _, hdr := adminServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         "transcriber",
		"name":         "health-test-bad",
		"endpoint_url": fakePlugin.URL,
		"token":        "tok",
		"enabled":      true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = doJSON(t, srv, http.MethodPost, "/api/admin/plugins/"+created.ID+"/health", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status %d body %s (unhealthy plugin must not be an HTTP error)", rec.Code, rec.Body)
	}
	var got struct {
		Healthy bool   `json:"healthy"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Healthy {
		t.Errorf("healthy = true, want false")
	}
	if got.Error == "" {
		t.Errorf("expected non-empty error message")
	}
}

func TestCheckPluginHealthNotFound(t *testing.T) {
	t.Parallel()
	srv, _, hdr := adminServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins/00000000-0000-0000-0000-000000000000/health", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

// TestCheckPluginHealthUnhealthySanitizesBody guards against relaying a
// misbehaving plugin's raw /health response body back to the admin UI: that
// body is plugin-controlled and could (e.g.) echo back the bearer token this
// server just sent it. Only a safe, status-code-based message should appear.
func TestCheckPluginHealthUnhealthySanitizesBody(t *testing.T) {
	t.Parallel()

	const secretLookingBody = "token=super-secret-plugin-bearer-abc123"
	fakePlugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			http.Error(w, secretLookingBody, http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fakePlugin.Close)

	srv, _, hdr := adminServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         "transcriber",
		"name":         "health-test-leak",
		"endpoint_url": fakePlugin.URL,
		"token":        "tok",
		"enabled":      true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = doJSON(t, srv, http.MethodPost, "/api/admin/plugins/"+created.ID+"/health", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status %d body %s", rec.Code, rec.Body)
	}
	if bytesContains(rec.Body.Bytes(), secretLookingBody) || bytesContains(rec.Body.Bytes(), "super-secret") {
		t.Fatalf("plugin response body leaked into health JSON: %s", rec.Body)
	}
	var got struct {
		Healthy bool   `json:"healthy"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Healthy {
		t.Errorf("healthy = true, want false")
	}
	if got.Error != "plugin returned 500" {
		t.Errorf("error = %q, want a safe status-code-only message", got.Error)
	}
}
