package embedded

import (
	"encoding/json"
	"testing"
)

func TestReporterProgressSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		steps []func(*Reporter)
		want  []Progress
	}{
		{
			name: "normal startup",
			steps: []func(*Reporter){
				func(r *Reporter) { r.Advance(PhaseDBInit, "starting embedded postgres") },
				func(r *Reporter) { r.Advance(PhasePgvector, "installing pgvector") },
				func(r *Reporter) { r.Advance(PhaseMigrate, "running migrations") },
				func(r *Reporter) { r.Advance(PhaseOllamaCheck, "Ollama detected") },
				func(r *Reporter) { r.SetDegraded(false) },
				func(r *Reporter) { r.Advance(PhaseModelPull, "pulling Ollama models") },
				func(r *Reporter) { r.SetPercent(42) },
				func(r *Reporter) { r.Advance(PhaseReady, "ready") },
			},
			want: []Progress{
				{Phase: PhaseDBInit, Detail: "starting embedded postgres", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhasePgvector, Detail: "installing pgvector", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseMigrate, Detail: "running migrations", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseOllamaCheck, Detail: "Ollama detected", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseOllamaCheck, Detail: "Ollama detected", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseModelPull, Detail: "pulling Ollama models", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseModelPull, Detail: "pulling Ollama models", Percent: 42, Ready: false, Degraded: false},
				{Phase: PhaseReady, Detail: "ready", Percent: 100, Ready: true, Degraded: false},
			},
		},
		{
			name: "degraded startup",
			steps: []func(*Reporter){
				func(r *Reporter) { r.Advance(PhaseDBInit, "starting embedded postgres") },
				func(r *Reporter) { r.Advance(PhaseMigrate, "running migrations") },
				func(r *Reporter) { r.Advance(PhaseOllamaCheck, "Ollama unavailable") },
				func(r *Reporter) { r.SetDegraded(true) },
				func(r *Reporter) { r.Advance(PhaseReady, "ready") },
			},
			want: []Progress{
				{Phase: PhaseDBInit, Detail: "starting embedded postgres", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseMigrate, Detail: "running migrations", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseOllamaCheck, Detail: "Ollama unavailable", Percent: 0, Ready: false, Degraded: false},
				{Phase: PhaseOllamaCheck, Detail: "Ollama unavailable", Percent: 0, Ready: false, Degraded: true},
				{Phase: PhaseReady, Detail: "ready", Percent: 100, Ready: true, Degraded: true},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := NewReporter()
			if got := r.Snapshot(); got != (Progress{}) {
				t.Fatalf("initial snapshot = %#v, want zero value", got)
			}

			if len(tc.steps) != len(tc.want) {
				t.Fatalf("test setup mismatch: %d steps vs %d wants", len(tc.steps), len(tc.want))
			}

			for i, step := range tc.steps {
				step(r)
				got := r.Snapshot()
				want := tc.want[i]
				if got != want {
					t.Fatalf("step %d snapshot = %#v, want %#v", i, got, want)
				}
				gotJSON, err := json.Marshal(got)
				if err != nil {
					t.Fatalf("marshal snapshot: %v", err)
				}
				wantJSON, err := json.Marshal(want)
				if err != nil {
					t.Fatalf("marshal want: %v", err)
				}
				if string(gotJSON) != string(wantJSON) {
					t.Fatalf("step %d json = %s, want %s", i, gotJSON, wantJSON)
				}
			}
		})
	}
}

func TestSetSourcePercent_AveragesAcrossSources(t *testing.T) {
	t.Parallel()

	r := NewReporter()
	r.SetSourcePercent("embedding", 100)
	r.SetSourcePercent("agent", 0)

	if got, want := r.Snapshot().Percent, 50; got != want {
		t.Fatalf("percent after embedding=100 and agent=0 = %d, want %d", got, want)
	}

	r.SetSourcePercent("agent", 40)
	if got, want := r.Snapshot().Percent, 70; got != want {
		t.Fatalf("percent after agent=40 = %d, want %d", got, want)
	}

	clamped := NewReporter()
	clamped.SetSourcePercent("embedding", 120)
	if got, want := clamped.Snapshot().Percent, 100; got != want {
		t.Fatalf("percent after embedding=120 = %d, want %d", got, want)
	}
	clamped.SetSourcePercent("agent", -10)
	if got, want := clamped.Snapshot().Percent, 50; got != want {
		t.Fatalf("percent after agent=-10 = %d, want %d", got, want)
	}

	var nilReporter *Reporter
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil reporter panicked: %v", r)
		}
	}()
	nilReporter.SetSourcePercent("embedding", 50)
}
