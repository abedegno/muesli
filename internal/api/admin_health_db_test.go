package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestAdminHealth_AlwaysOK is the DB-backed, real-Store version of the
// "always 200" contract: with no plugins registered, no embedder configured,
// and an empty job queue, every section should still report cleanly. Uses
// testutil.NewPool per repo convention - CI provides TEST_DATABASE_URL; a
// local run without it skips automatically.
func TestAdminHealth_AlwaysOK(t *testing.T) {
	t.Parallel()
	srv, _, hdr := adminServer(t)

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/health", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var got api.AdminHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Server.Version == "" || got.Server.Commit == "" || got.Server.GoVersion == "" {
		t.Fatalf("server info has blank fields: %+v", got.Server)
	}
	if len(got.Plugins) != 0 {
		t.Fatalf("expected no plugins, got %+v", got.Plugins)
	}
	if got.Jobs.Error != "" {
		t.Fatalf("unexpected jobs error: %s", got.Jobs.Error)
	}
	if got.Jobs.Status != "ok" {
		t.Fatalf("expected jobs status ok with no failed jobs, got %q (full: %+v)", got.Jobs.Status, got.Jobs)
	}
	for _, status := range []string{"pending", "running", "done", "failed", "cancelled"} {
		if _, ok := got.Jobs.Counts[status]; !ok {
			t.Fatalf("expected job status %q to be present even at 0, got %+v", status, got.Jobs.Counts)
		}
	}
	if got.Server.Status != "ok" && got.Server.Status != "warn" {
		t.Fatalf("server status = %q, want ok or warn", got.Server.Status)
	}
	if got.Embedding.Enabled {
		t.Fatalf("expected embeddings disabled by default, got %+v", got.Embedding)
	}
	if got.Storage.Error != "" && got.Storage.TotalBytes == 0 {
		t.Logf("storage disk usage errored (acceptable in some sandboxes): %s", got.Storage.Error)
	}
}

// TestAdminHealth_JobsWarnOnFailedJob is the DB-backed, real-Store proof that
// the jobs section's status flips to "warn" the moment any job is
// terminally failed - the code review's headline finding: the
// backend must actually set this, not just the pure-logic jobQueueStatus
// helper in isolation.
func TestAdminHealth_JobsWarnOnFailedJob(t *testing.T) {
	t.Parallel()
	srv, st, hdr := adminServer(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "warn-test note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	// retryable=false forces an immediate terminal failure regardless of the
	// attempt cap.
	if err := st.FailJob(ctx, jobID, "boom", false); err != nil {
		t.Fatalf("fail: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/health", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var got api.AdminHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Jobs.Status != "warn" {
		t.Fatalf("jobs status = %q, want warn with a failed job present (full: %+v)", got.Jobs.Status, got.Jobs)
	}
	if got.Jobs.Counts["failed"] != 1 {
		t.Fatalf("expected 1 failed job, got %+v", got.Jobs.Counts)
	}
}

// TestAdminHealth_PluginProbesUseInjectedSeam registers one enabled and one
// disabled plugin via the real admin API (so ListPlugins/GetPlugin run
// against a real Store), then wires a fake Deps.PluginHealthProber so the
// probe itself never makes a real HTTP call. Confirms: the disabled plugin
// is reported "disabled" and never probed, and the enabled plugin's
// reported status reflects the fake prober's verdict plus the endpoint/token
// it decrypted from the store.
func TestAdminHealth_PluginProbesUseInjectedSeam(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))

	probedURLs := map[string]bool{}
	prober := func(_ context.Context, endpointURL, token string) error {
		probedURLs[endpointURL] = true
		if endpointURL == "http://transcriber:9000" {
			return nil // healthy
		}
		return errUnreachable
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, PluginHealthProber: prober})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind": "transcriber", "name": "healthy-transcriber",
		"endpoint_url": "http://transcriber:9000", "token": "tok-1",
		"config": map[string]string{}, "enabled": true,
	}, hdr)
	doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind": "agent", "name": "disabled-agent",
		"endpoint_url": "http://agent:9001", "token": "tok-2",
		"config": map[string]string{}, "enabled": false,
	}, hdr)

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/health", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var got api.AdminHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Plugins) != 2 {
		t.Fatalf("expected 2 plugin entries, got %+v", got.Plugins)
	}
	byName := map[string]api.PluginHealthEntry{}
	for _, p := range got.Plugins {
		byName[p.Name] = p
	}
	if byName["healthy-transcriber"].Status != "ok" {
		t.Fatalf("expected healthy-transcriber ok, got %+v", byName["healthy-transcriber"])
	}
	if byName["disabled-agent"].Status != "disabled" {
		t.Fatalf("expected disabled-agent disabled, got %+v", byName["disabled-agent"])
	}
	if probedURLs["http://agent:9001"] {
		t.Fatal("disabled plugin must never be probed")
	}
	if !probedURLs["http://transcriber:9000"] {
		t.Fatal("expected the enabled plugin to be probed")
	}
}

var errUnreachable = &probeErr{"connection refused"}

type probeErr struct{ msg string }

func (e *probeErr) Error() string { return e.msg }
