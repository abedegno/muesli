package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embedded"
)

func TestRunEmbeddedStartupPhases_OrderAndReadyGating(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DatabaseURL:     "postgres://example",
		EmbeddingsModel: "nomic-embed-text",
	}
	reporter := embedded.NewReporter()

	var mu sync.Mutex
	events := make([]string, 0, 8)
	record := func(name string) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	}

	pullsStarted := make(chan struct{}, 2)
	allowPulls := make(chan struct{})
	readyDone := make(chan struct{})

	complete, err := runEmbeddedStartupPhases(context.Background(), &cfg, reporter, "http://127.0.0.1:11434", embeddedStartupHooks{
		migrate: func(databaseURL string) error {
			record("migrate")
			if got := reporter.Snapshot().Phase; got != embedded.PhaseMigrate {
				t.Errorf("phase during migrate = %q, want %q", got, embedded.PhaseMigrate)
			}
			if databaseURL != cfg.DatabaseURL {
				t.Errorf("databaseURL = %q, want %q", databaseURL, cfg.DatabaseURL)
			}
			return nil
		},
		detect: func(_ context.Context, _ string) bool {
			record("detect")
			if got := reporter.Snapshot().Phase; got != embedded.PhaseOllamaCheck {
				t.Errorf("phase during detect = %q, want %q", got, embedded.PhaseOllamaCheck)
			}
			return true
		},
		configure: embedded.ConfigureEmbeddedOllama,
		pullEmbedding: func(_ context.Context, _ string, _ string, _ embedded.PullProgressFunc) error {
			record("pull-embedding")
			if got := reporter.Snapshot().Phase; got != embedded.PhaseModelPull {
				t.Errorf("phase during embedding pull = %q, want %q", got, embedded.PhaseModelPull)
			}
			pullsStarted <- struct{}{}
			<-allowPulls
			return nil
		},
		pullModel: func(_ context.Context, _ string, _ string, onProgress embedded.PullProgressFunc) error {
			record("pull-model")
			if got := reporter.Snapshot().Phase; got != embedded.PhaseModelPull {
				t.Errorf("phase during agent pull = %q, want %q", got, embedded.PhaseModelPull)
			}
			onProgress(50)
			pullsStarted <- struct{}{}
			<-allowPulls
			return nil
		},
		onReady: func() {
			close(readyDone)
		},
	})
	if err != nil {
		t.Fatalf("runEmbeddedStartupPhases() error: %v", err)
	}

	if got := reporter.Snapshot().Phase; got != embedded.PhaseModelPull {
		t.Fatalf("phase before completion = %q, want %q", got, embedded.PhaseModelPull)
	}
	if reporter.Snapshot().Ready {
		t.Fatal("ready flipped before completion")
	}

	<-pullsStarted
	<-pullsStarted

	mu.Lock()
	if len(events) < 4 {
		t.Fatalf("events = %v, want at least 4 entries before completion", events)
	}
	if events[0] != "migrate" {
		t.Fatalf("first event = %q, want migrate", events[0])
	}
	if events[1] != "detect" {
		t.Fatalf("second event = %q, want detect", events[1])
	}
	mu.Unlock()

	complete()
	if reporter.Snapshot().Ready {
		t.Fatal("ready flipped before pulls completed")
	}

	close(allowPulls)

	<-readyDone
	if !reporter.Snapshot().Ready {
		t.Fatal("ready did not flip after pulls completed and startup finished")
	}
	if got := reporter.Snapshot().Phase; got != embedded.PhaseReady {
		t.Fatalf("final phase = %q, want %q", got, embedded.PhaseReady)
	}
}

func TestRunEmbeddedStartupPhases_BothPullsGetGenerousDeadline(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DatabaseURL:     "postgres://example",
		EmbeddingsModel: "nomic-embed-text",
	}
	reporter := embedded.NewReporter()

	embeddingCtxCh := make(chan context.Context, 1)
	modelCtxCh := make(chan context.Context, 1)
	readyDone := make(chan struct{})

	complete, err := runEmbeddedStartupPhases(context.Background(), &cfg, reporter, "http://127.0.0.1:11434", embeddedStartupHooks{
		migrate: func(string) error { return nil },
		detect: func(context.Context, string) bool {
			return true
		},
		configure: func(*config.Config, string, bool) {},
		pullEmbedding: func(ctx context.Context, _ string, _ string, _ embedded.PullProgressFunc) error {
			embeddingCtxCh <- ctx
			return nil
		},
		pullModel: func(ctx context.Context, _ string, _ string, _ embedded.PullProgressFunc) error {
			modelCtxCh <- ctx
			return nil
		},
		onReady: func() {
			close(readyDone)
		},
	})
	if err != nil {
		t.Fatalf("runEmbeddedStartupPhases() error: %v", err)
	}

	embeddingCtx := <-embeddingCtxCh
	modelCtx := <-modelCtxCh

	embeddingDeadline, ok := embeddingCtx.Deadline()
	if !ok {
		t.Fatal("embedding pull ctx has no deadline")
	}
	if remaining := time.Until(embeddingDeadline); remaining <= 60*time.Second {
		t.Fatalf("embedding pull deadline too short: %s", remaining)
	}

	modelDeadline, ok := modelCtx.Deadline()
	if !ok {
		t.Fatal("agent pull ctx has no deadline")
	}
	if remaining := time.Until(modelDeadline); remaining <= 60*time.Second {
		t.Fatalf("agent pull deadline too short: %s", remaining)
	}

	if delta := embeddingDeadline.Sub(modelDeadline); delta > 2*time.Second || delta < -2*time.Second {
		t.Fatalf("deadlines differ too much: %s", delta)
	}

	complete()

	select {
	case <-readyDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback did not fire")
	}
}

func TestRunEmbeddedStartupPhases_ProgressDoesNotRegressAcrossSources(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DatabaseURL:     "postgres://example",
		EmbeddingsModel: "nomic-embed-text",
	}
	reporter := embedded.NewReporter()

	embeddingProgressed := make(chan struct{})
	allowFinish := make(chan struct{})
	readyDone := make(chan struct{})
	snapshots := make(chan int, 2)

	complete, err := runEmbeddedStartupPhases(context.Background(), &cfg, reporter, "http://127.0.0.1:11434", embeddedStartupHooks{
		migrate: func(string) error { return nil },
		detect: func(context.Context, string) bool {
			return true
		},
		configure: func(*config.Config, string, bool) {},
		pullEmbedding: func(_ context.Context, _ string, _ string, onProgress embedded.PullProgressFunc) error {
			onProgress(100)
			close(embeddingProgressed)
			<-allowFinish
			return nil
		},
		pullModel: func(_ context.Context, _ string, _ string, onProgress embedded.PullProgressFunc) error {
			<-embeddingProgressed
			onProgress(0)
			snapshots <- reporter.Snapshot().Percent
			onProgress(40)
			snapshots <- reporter.Snapshot().Percent
			<-allowFinish
			return nil
		},
		onReady: func() {
			close(readyDone)
		},
	})
	if err != nil {
		t.Fatalf("runEmbeddedStartupPhases() error: %v", err)
	}

	if got := <-snapshots; got != 50 {
		t.Fatalf("percent after embedding=100 and agent=0 = %d, want %d", got, 50)
	}
	if got := <-snapshots; got != 70 {
		t.Fatalf("percent after agent=40 = %d, want %d", got, 70)
	}
	if got := reporter.Snapshot().Percent; got != 70 {
		t.Fatalf("final reporter percent = %d, want %d", got, 70)
	}

	complete()
	close(allowFinish)

	select {
	case <-readyDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback did not fire")
	}
}

func TestApplyEmbeddedAgentDefault_SetsURL(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	applyEmbeddedAgentDefault(&cfg, "http://127.0.0.1:5555")
	if got, want := cfg.DefaultAgentURL, "http://127.0.0.1:5555"; got != want {
		t.Fatalf("DefaultAgentURL = %q, want %q", got, want)
	}
}

func TestApplyEmbeddedAgentDefault_EmptyURLIsNoOp(t *testing.T) {
	t.Parallel()

	cfg := config.Config{DefaultAgentURL: "http://existing"}
	applyEmbeddedAgentDefault(&cfg, "")
	if got, want := cfg.DefaultAgentURL, "http://existing"; got != want {
		t.Fatalf("DefaultAgentURL = %q, want %q", got, want)
	}
}

func TestApplyEmbeddedTranscriberDefault_SetsURL(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	applyEmbeddedTranscriberDefault(&cfg, "http://127.0.0.1:5555")
	if got, want := cfg.DefaultTranscriberURL, "http://127.0.0.1:5555"; got != want {
		t.Fatalf("DefaultTranscriberURL = %q, want %q", got, want)
	}
}

func TestApplyEmbeddedTranscriberDefault_EmptyURLIsNoOp(t *testing.T) {
	t.Parallel()

	cfg := config.Config{DefaultTranscriberURL: "http://existing"}
	applyEmbeddedTranscriberDefault(&cfg, "")
	if got, want := cfg.DefaultTranscriberURL, "http://existing"; got != want {
		t.Fatalf("DefaultTranscriberURL = %q, want %q", got, want)
	}
}
