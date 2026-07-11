package api

import (
	"context"
	"net/http"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

// PluginHealthProber probes a single plugin's /health endpoint given its
// decrypted endpoint URL and bearer token, returning nil when healthy. Mirrors
// the Deps.Prober seam in health.go: a nil Deps.PluginHealthProber falls back
// to defaultPluginHealthProber (a real plugin.New(...).Health call), so Go
// tests can mock plugin probes without a real HTTP server.
type PluginHealthProber func(ctx context.Context, endpointURL, token string) error

// pluginHealthProbeTimeout bounds each per-plugin probe so one unreachable
// plugin can never stall the whole /api/admin/health response.
const pluginHealthProbeTimeout = 4 * time.Second

func defaultPluginHealthProber(ctx context.Context, endpointURL, token string) error {
	ctx, cancel := context.WithTimeout(ctx, pluginHealthProbeTimeout)
	defer cancel()
	return plugin.New(endpointURL, token).Health(ctx)
}

// pluginHealthProber returns the injected prober, defaulting to the real
// HTTP-probing implementation when none was wired (production). Tests wire
// s.deps.PluginHealthProber to a stub so no test ever makes a real HTTP call.
func (s *Server) pluginHealthProber() PluginHealthProber {
	if s.deps.PluginHealthProber != nil {
		return s.deps.PluginHealthProber
	}
	return defaultPluginHealthProber
}

// ServerInfoHealth reports build/version info for the admin health panel.
type ServerInfoHealth struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	// Status is "ok" when full build info (version/commit/Go version) was
	// available, or "warn" when one or more fields fell back to their
	// "dev"/"unknown" default (e.g. `go run`, or a binary built without VCS
	// stamping) - never "error": a missing build stamp never indicates the
	// server itself is unhealthy.
	Status string `json:"status"`
}

// buildServerInfoHealth reads runtime/debug.ReadBuildInfo with sane fallbacks
// ("dev"/"unknown") when build info is unavailable or incomplete (e.g. `go
// run`, or a binary built without VCS stamping).
func buildServerInfoHealth() ServerInfoHealth {
	out := ServerInfoHealth{Version: "dev", Commit: "unknown", GoVersion: "unknown"}
	bi, ok := debug.ReadBuildInfo()
	if ok && bi != nil {
		if bi.GoVersion != "" {
			out.GoVersion = bi.GoVersion
		}
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			out.Version = bi.Main.Version
		}
		for _, setting := range bi.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				out.Commit = setting.Value
			}
		}
	}
	out.Status = "ok"
	if out.Version == "dev" || out.Commit == "unknown" || out.GoVersion == "unknown" {
		out.Status = "warn"
	}
	return out
}

// PluginHealthEntry is one plugin's status in the admin health panel.
type PluginHealthEntry struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "error" | "disabled"
	Error  string `json:"error,omitempty"`
}

// pluginProbeInput bundles what buildPluginHealthEntries needs to decide one
// plugin's status: either it's disabled (never probed - the admin browser
// cannot reach plugins directly), already known to be unloadable (LoadError,
// e.g. a decrypt failure), or enabled and ready to probe via endpoint+token.
type pluginProbeInput struct {
	ID          string
	Kind        string
	Name        string
	Enabled     bool
	EndpointURL string
	Token       string
	LoadError   error
}

// buildPluginHealthEntries is the pure, mockable core of the plugin-health
// section: given already-resolved inputs (no store/DB access) and a prober,
// it produces one PluginHealthEntry per input, preserving order. Enabled
// entries are probed concurrently, each bounded by pluginHealthProbeTimeout
// via the prober itself, so N plugins don't serialize to N*timeout.
func buildPluginHealthEntries(ctx context.Context, inputs []pluginProbeInput, prober PluginHealthProber) []PluginHealthEntry {
	if prober == nil {
		prober = defaultPluginHealthProber
	}
	out := make([]PluginHealthEntry, len(inputs))
	var wg sync.WaitGroup
	for i, in := range inputs {
		out[i] = PluginHealthEntry{ID: in.ID, Kind: in.Kind, Name: in.Name}
		switch {
		case !in.Enabled:
			out[i].Status = "disabled"
		case in.LoadError != nil:
			out[i].Status = "error"
			out[i].Error = in.LoadError.Error()
		default:
			wg.Add(1)
			go func(i int, in pluginProbeInput) {
				defer wg.Done()
				if err := prober(ctx, in.EndpointURL, in.Token); err != nil {
					out[i].Status = "error"
					out[i].Error = pluginHealthErrorMessage(err)
				} else {
					out[i].Status = "ok"
				}
			}(i, in)
		}
	}
	wg.Wait()
	return out
}

// buildAdminPluginHealth loads every registered plugin, decrypts enabled
// ones' endpoint+token via Store.GetPlugin, and delegates the actual
// probing to buildPluginHealthEntries (the pure/mockable part). A
// ListPlugins/GetPlugin failure is captured per-entry, never failing the
// whole response.
func (s *Server) buildAdminPluginHealth(ctx context.Context) []PluginHealthEntry {
	summaries, err := s.deps.Store.ListPlugins(ctx, s.deps.Crypto)
	if err != nil {
		return []PluginHealthEntry{{Status: "error", Error: "list plugins: " + err.Error()}}
	}
	inputs := make([]pluginProbeInput, len(summaries))
	for i, p := range summaries {
		in := pluginProbeInput{ID: p.ID, Kind: p.Kind, Name: p.Name, Enabled: p.Enabled}
		if p.Enabled {
			decrypted, gerr := s.deps.Store.GetPlugin(ctx, s.deps.Crypto, p.ID)
			if gerr != nil {
				in.LoadError = gerr
			} else {
				in.EndpointURL = decrypted.EndpointURL
				in.Token = decrypted.Token
			}
		}
		inputs[i] = in
	}
	return buildPluginHealthEntries(ctx, inputs, s.pluginHealthProber())
}

// JobQueueHealth is the job-queue-depth section of the admin health panel.
type JobQueueHealth struct {
	Counts map[string]int `json:"counts"`
	// Status is "error" when Store.JobStatusCounts itself failed, "warn"
	// when it succeeded but at least one job is terminally failed, or "ok"
	// otherwise.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// jobQueueStatus derives the job-queue section's status badge from its
// counts: "warn" when any job is terminally failed, "ok" otherwise. Pure
// function of the counts map, so it's testable without a DB.
func jobQueueStatus(counts map[string]int) string {
	if counts[model.JobFailed] > 0 {
		return "warn"
	}
	return "ok"
}

// buildJobQueueHealth reports job counts per status (Store.JobStatusCounts).
// A store error is captured on this section only, not the whole response.
func (s *Server) buildJobQueueHealth(ctx context.Context) JobQueueHealth {
	counts, err := s.deps.Store.JobStatusCounts(ctx)
	if err != nil {
		return JobQueueHealth{Counts: map[string]int{}, Status: "error", Error: err.Error()}
	}
	return JobQueueHealth{Counts: counts, Status: jobQueueStatus(counts)}
}

// EmbeddingHealth combines the static EmbeddingsStatus config (EMB01 shape)
// with live done/total coverage counts (EMB02's Store.EmbeddingStatus).
type EmbeddingHealth struct {
	EmbeddingsStatus
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Error string `json:"error,omitempty"`
}

// buildEmbeddingHealth reports embedding config always (even when disabled,
// matching handleAdminEmbeddingsStatus's convention), plus live done/total
// coverage when enabled. Matches the Deps.Embedder == nil disabled-state
// convention used by handleAdminReembedStatus: no live store lookup is
// attempted while disabled, so done/total stay 0.
func (s *Server) buildEmbeddingHealth(ctx context.Context) EmbeddingHealth {
	enabled := s.deps.Embedder != nil
	dim := s.deps.Config.EmbeddingsDim
	if dim <= 0 {
		dim = 768 // match the fallback in embed.New / handleAdminEmbeddingsStatus
	}
	out := EmbeddingHealth{
		EmbeddingsStatus: EmbeddingsStatus{
			Enabled:     enabled,
			Model:       s.deps.Config.EmbeddingsModel,
			Dim:         dim,
			MinScore:    s.deps.Config.EmbeddingsMinScore,
			DocPrefix:   s.deps.Config.EmbeddingsDocPrefix,
			QueryPrefix: s.deps.Config.EmbeddingsQueryPrefix,
		},
	}
	if !enabled {
		return out
	}
	// Use the embedder's actual dim for the live lookup (may differ from the
	// static config default above), mirroring handleAdminReembedStatus.
	liveDim := s.deps.Embedder.Dim()
	out.Dim = liveDim
	done, total, err := s.deps.Store.EmbeddingStatus(ctx, s.deps.Config.EmbeddingsModel, liveDim)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Done = done
	out.Total = total
	return out
}

// StorageDiskUsage is the storage-dir disk-usage section of the admin health
// panel: total/free bytes for Config.StorageDir.
type StorageDiskUsage struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	Error      string `json:"error,omitempty"`
}

// buildStorageDiskUsage stats the storage dir's filesystem via syscall.Statfs
// (this repo only ever runs in Linux containers, so no cross-platform
// fallback is needed). A stat error is captured on this section only.
func buildStorageDiskUsage(path string) StorageDiskUsage {
	out := StorageDiskUsage{Path: path}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		out.Error = err.Error()
		return out
	}
	bsize := uint64(stat.Bsize) //nolint:unconvert // Bsize's width varies by arch
	out.TotalBytes = stat.Blocks * bsize
	out.FreeBytes = stat.Bavail * bsize
	return out
}

// AdminHealthResponse is the full body of GET /api/admin/health.
type AdminHealthResponse struct {
	Server    ServerInfoHealth    `json:"server"`
	Plugins   []PluginHealthEntry `json:"plugins"`
	Jobs      JobQueueHealth      `json:"jobs"`
	Embedding EmbeddingHealth     `json:"embedding"`
	Storage   StorageDiskUsage    `json:"storage"`
}

// handleAdminHealth aggregates a full admin health snapshot: server build
// info, per-plugin probes, job-queue depth, embedding coverage, and
// storage-dir disk usage. ALWAYS returns 200 - every section captures its
// own failure as an error string (plus a status badge for plugins) instead
// of failing the whole response, so one broken dependency never takes down
// the whole panel.
func (s *Server) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, http.StatusOK, AdminHealthResponse{
		Server:    buildServerInfoHealth(),
		Plugins:   s.buildAdminPluginHealth(ctx),
		Jobs:      s.buildJobQueueHealth(ctx),
		Embedding: s.buildEmbeddingHealth(ctx),
		Storage:   buildStorageDiskUsage(s.deps.Config.StorageDir),
	})
}
