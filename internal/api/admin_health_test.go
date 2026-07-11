package api

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestBuildServerInfoHealth is pure logic - no DB, no HTTP - so it runs fast
// and locally. It only asserts the "never blank" contract: whatever
// runtime/debug.ReadBuildInfo returns (or doesn't), every field is populated,
// and Status is always a valid, non-error badge value.
func TestBuildServerInfoHealth(t *testing.T) {
	t.Parallel()
	info := buildServerInfoHealth()
	if info.Version == "" {
		t.Fatal("Version must never be empty")
	}
	if info.Commit == "" {
		t.Fatal("Commit must never be empty")
	}
	if info.GoVersion == "" {
		t.Fatal("GoVersion must never be empty")
	}
	if info.Status != "ok" && info.Status != "warn" {
		t.Fatalf("Status = %q, want ok or warn (server info is never an error)", info.Status)
	}
	// go test always runs under `go test`, which does stamp module version
	// info, so under normal CI/local conditions we expect a fully-populated
	// "ok" badge; this documents (without hard-failing on unusual toolchains)
	// that "warn" only appears when a field truly fell back.
	if info.Status == "warn" && info.Version != "dev" && info.Commit != "unknown" && info.GoVersion != "unknown" {
		t.Fatalf("Status = warn but no field fell back: %+v", info)
	}
}

// TestJobQueueStatus is pure logic - a plain map in, a string out - and
// directly exercises the "warn when any job is terminally failed" rule
// the code review flagged as missing from the server side.
func TestJobQueueStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		counts map[string]int
		want   string
	}{
		{"empty queue is ok", map[string]int{}, "ok"},
		{"all zero is ok", map[string]int{"pending": 0, "running": 0, "done": 0, "failed": 0, "cancelled": 0}, "ok"},
		{"pending/running/done only is ok", map[string]int{"pending": 2, "running": 1, "done": 5, "failed": 0}, "ok"},
		{"any failed job is warn", map[string]int{"pending": 0, "running": 0, "done": 3, "failed": 1}, "warn"},
		{"many failed jobs is still just warn", map[string]int{"failed": 40}, "warn"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := jobQueueStatus(c.counts); got != c.want {
				t.Fatalf("jobQueueStatus(%+v) = %q, want %q", c.counts, got, c.want)
			}
		})
	}
}

// TestBuildStorageDiskUsage is pure logic (stats a real path but makes no
// network/DB call) so it runs fast and locally.
func TestBuildStorageDiskUsage(t *testing.T) {
	t.Parallel()

	t.Run("existing path reports usage with no error", func(t *testing.T) {
		t.Parallel()
		out := buildStorageDiskUsage(t.TempDir())
		if out.Error != "" {
			t.Fatalf("unexpected error: %s", out.Error)
		}
		if out.TotalBytes == 0 {
			t.Fatal("expected non-zero TotalBytes for an existing path")
		}
	})

	t.Run("missing path captures a per-section error", func(t *testing.T) {
		t.Parallel()
		out := buildStorageDiskUsage("/no/such/path/adm05-health-test")
		if out.Error == "" {
			t.Fatal("expected a captured error for a missing path")
		}
		if out.TotalBytes != 0 || out.FreeBytes != 0 {
			t.Fatalf("expected zeroed bytes on error, got %+v", out)
		}
	})
}

// TestBuildPluginHealthEntries is the pure-logic test for the injectable
// plugin-probe seam (ADM05's mirror of Deps.Prober): it never opens a real
// HTTP server or a DB, only a hand-built input list and a fake prober.
func TestBuildPluginHealthEntries(t *testing.T) {
	t.Parallel()

	inputs := []pluginProbeInput{
		{ID: "1", Kind: "transcriber", Name: "healthy", Enabled: true, EndpointURL: "http://ok", Token: "t1"},
		{ID: "2", Kind: "agent", Name: "unhealthy", Enabled: true, EndpointURL: "http://bad", Token: "t2"},
		{ID: "3", Kind: "agent", Name: "off", Enabled: false},
		{ID: "4", Kind: "transcriber", Name: "unloadable", Enabled: true, LoadError: errors.New("decrypt failed")},
	}

	var mu sync.Mutex
	probed := map[string]bool{}
	prober := func(_ context.Context, endpointURL, token string) error {
		mu.Lock()
		probed[endpointURL+"|"+token] = true
		mu.Unlock()
		if endpointURL == "http://bad" {
			return errors.New("connection refused")
		}
		return nil
	}

	entries := buildPluginHealthEntries(context.Background(), inputs, prober)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}

	if entries[0].Status != "ok" || entries[0].Error != "" {
		t.Fatalf("entry 0 = %+v, want status=ok no error", entries[0])
	}
	if entries[1].Status != "error" || entries[1].Error == "" {
		t.Fatalf("entry 1 = %+v, want status=error with a message", entries[1])
	}
	if entries[2].Status != "disabled" {
		t.Fatalf("entry 2 = %+v, want status=disabled", entries[2])
	}
	if entries[2].Error != "" {
		t.Fatalf("disabled entry must never carry an error, got %+v", entries[2])
	}
	if entries[3].Status != "error" || entries[3].Error != "decrypt failed" {
		t.Fatalf("entry 3 = %+v, want status=error decrypt failed", entries[3])
	}

	// The disabled plugin must never be probed (admin browser cannot reach
	// plugins directly - only the server may probe them).
	mu.Lock()
	defer mu.Unlock()
	if !probed["http://ok|t1"] {
		t.Fatal("expected the healthy plugin to be probed")
	}
	if !probed["http://bad|t2"] {
		t.Fatal("expected the unhealthy plugin to be probed")
	}
	// The disabled and unloadable plugins have nothing valid to probe with.
	if len(probed) != 2 {
		t.Fatalf("expected exactly 2 probes, got %d: %v", len(probed), probed)
	}
}

// TestBuildPluginHealthEntries_NilProberUsesDefault confirms the nil-prober
// fallback wiring (mirrors defaultProber in health.go) without making a real
// HTTP call: an empty, disabled-only input list never reaches the prober, so
// this exercises the "prober == nil -> defaultPluginHealthProber" branch
// safely and stays pure/local.
func TestBuildPluginHealthEntries_NilProberUsesDefault(t *testing.T) {
	t.Parallel()
	inputs := []pluginProbeInput{{ID: "1", Enabled: false}}
	entries := buildPluginHealthEntries(context.Background(), inputs, nil)
	if len(entries) != 1 || entries[0].Status != "disabled" {
		t.Fatalf("got %+v, want a single disabled entry", entries)
	}
}

// TestBuildEmbeddingHealth_Disabled is pure logic: Deps.Embedder == nil means
// no store call is ever attempted, so this needs no DB.
func TestBuildEmbeddingHealth_Disabled(t *testing.T) {
	t.Parallel()
	srv := NewServer(Deps{})
	out := srv.buildEmbeddingHealth(context.Background())
	if out.Enabled {
		t.Fatal("expected Enabled=false with a nil Embedder")
	}
	if out.Done != 0 || out.Total != 0 {
		t.Fatalf("expected zeroed done/total while disabled, got %+v", out)
	}
	if out.Error != "" {
		t.Fatalf("expected no error while disabled, got %q", out.Error)
	}
	if out.Dim != 768 {
		t.Fatalf("expected the default fallback dim 768, got %d", out.Dim)
	}
}
