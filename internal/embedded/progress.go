package embedded

import "sync"

// Phase identifies the current embedded startup phase.
type Phase string

const (
	PhaseDBInit      Phase = "db-init"
	PhasePgvector    Phase = "pgvector"
	PhaseMigrate     Phase = "migrate"
	PhaseOllamaCheck Phase = "ollama-check"
	PhaseModelPull   Phase = "model-pull"
	PhaseReady       Phase = "ready"
)

// Progress is the public embedded startup snapshot.
type Progress struct {
	Phase    Phase  `json:"phase"`
	Detail   string `json:"detail"`
	Percent  int    `json:"percent"`
	Ready    bool   `json:"ready"`
	Degraded bool   `json:"degraded"`
}

// Reporter tracks startup progress across concurrent embedded startup steps.
type Reporter struct {
	mu      sync.Mutex
	p       Progress
	sources map[string]int
}

// NewReporter returns a new startup progress reporter.
func NewReporter() *Reporter {
	return &Reporter{}
}

// Advance updates the current phase and detail. Ready is cleared unless the
// phase is PhaseReady, in which case percent is forced to 100.
func (r *Reporter) Advance(phase Phase, detail string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.p.Phase = phase
	r.p.Detail = detail
	if phase == PhaseReady {
		r.p.Ready = true
		r.p.Percent = 100
		return
	}

	r.p.Ready = false
	r.p.Percent = 0
}

// SetPercent updates the current progress percentage.
func (r *Reporter) SetPercent(percent int) {
	if r == nil {
		return
	}

	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	r.mu.Lock()
	r.p.Percent = percent
	r.mu.Unlock()
}

// SetSourcePercent updates a named source's progress and recomputes the
// aggregate percent as the floored average across all known sources.
func (r *Reporter) SetSourcePercent(source string, percent int) {
	if r == nil {
		return
	}

	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	r.mu.Lock()
	if r.sources == nil {
		r.sources = make(map[string]int)
	}
	r.sources[source] = percent

	total := 0
	for _, sourcePercent := range r.sources {
		total += sourcePercent
	}
	r.p.Percent = total / len(r.sources)
	r.mu.Unlock()
}

// SetDegraded updates the degraded flag.
func (r *Reporter) SetDegraded(degraded bool) {
	if r == nil {
		return
	}

	r.mu.Lock()
	r.p.Degraded = degraded
	r.mu.Unlock()
}

// Snapshot returns the current progress snapshot.
func (r *Reporter) Snapshot() Progress {
	if r == nil {
		return Progress{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.p
}
