package api

import (
	"context"
	"net/http"
	"time"
)

// DepProber probes whether a URL is reachable.
// A nil prober (or a nil entry in the map) should use the default HTTP prober.
type DepProber func(ctx context.Context, url string) error

// probeClient is used by defaultProber. It never follows redirects so that any
// HTTP response (including 3xx) is treated as "the service is up". The timeout
// is left at zero because defaultProber supplies a context deadline instead.
var probeClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse // treat any 3xx as reachable, stop there
	},
}

// defaultProber performs an HTTP HEAD with a 3-second context timeout.
// Any HTTP response (even 3xx/4xx/5xx) counts as reachable; only network
// errors or timeouts are treated as not reachable.
func defaultProber(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return err
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type depStatus struct {
	Configured bool   `json:"configured"`
	Ok         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

type readyzResponse struct {
	Status string               `json:"status"`
	Deps   map[string]depStatus `json:"deps"`
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config
	prober := s.deps.Prober
	if prober == nil {
		prober = defaultProber
	}

	type depSpec struct {
		name string
		url  string
	}
	specs := []depSpec{
		{"transcriber", cfg.DefaultTranscriberURL},
		{"agent", cfg.DefaultAgentURL},
		{"embeddings", cfg.EmbeddingsURL},
	}

	deps := make(map[string]depStatus, len(specs))
	allOK := true
	for _, spec := range specs {
		if spec.url == "" {
			deps[spec.name] = depStatus{Configured: false, Ok: true}
			continue
		}
		if err := prober(r.Context(), spec.url); err != nil {
			deps[spec.name] = depStatus{Configured: true, Ok: false, Error: err.Error()}
			allOK = false
		} else {
			deps[spec.name] = depStatus{Configured: true, Ok: true}
		}
	}

	status := "ready"
	code := http.StatusOK
	if !allOK {
		status = "starting"
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, readyzResponse{Status: status, Deps: deps})
}
