package embedded

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelRetryBoundsDownloadsAndUsesShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reporter := NewReporter()
	var calls atomic.Int32
	started := make(chan struct{}, 2)
	finished := make(chan struct{}, 2)
	retry := NewModelRetry(ctx, reporter, "http://unused", "embedding", func(ctx context.Context, _, _ string, progress PullProgressFunc) error {
		calls.Add(1)
		progress(43)
		started <- struct{}{}
		<-ctx.Done()
		finished <- struct{}{}
		return ctx.Err()
	})
	if err := retry(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("downloads did not start")
		}
	}
	for range 20 {
		if !errors.Is(retry(), ErrModelsBusy) {
			t.Fatal("concurrent retry accepted")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("unbounded downloads: %d", calls.Load())
	}
	cancel()
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("shutdown ignored")
		}
	}
	deadline := time.Now().Add(time.Second)
	for {
		models := reporter.ModelsSnapshot()
		if len(models) == 2 && models[0].Status == "failed" && models[1].Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing failed states: %+v", models)
		}
		time.Sleep(time.Millisecond)
	}
}
