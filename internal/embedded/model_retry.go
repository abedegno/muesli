package embedded

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrModelsBusy = errors.New("model downloads are already running")

// NewModelRetry returns a per-server coordinator. At most two downloads run;
// retries share the server shutdown context and never start from a health read.
func NewModelRetry(ctx context.Context, reporter *Reporter, baseURL, embeddingModel string, pull func(context.Context, string, string, PullProgressFunc) error) func() error {
	var mu sync.Mutex
	busy := false
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if busy {
			return ErrModelsBusy
		}
		for _, model := range reporter.ModelsSnapshot() {
			if model.Status == "downloading" {
				return ErrModelsBusy
			}
		}
		busy = true
		for _, name := range []string{embeddingModel, DefaultOllamaAgentModel} {
			reporter.SetModel(name, "downloading", 0, "")
		}
		go func() {
			defer func() { mu.Lock(); busy = false; mu.Unlock() }()
			pullCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()
			var group sync.WaitGroup
			for _, name := range []string{embeddingModel, DefaultOllamaAgentModel} {
				group.Add(1)
				go func(name string) {
					defer group.Done()
					err := pull(pullCtx, baseURL, name, func(percent int) { reporter.SetModel(name, "downloading", percent, "") })
					if err != nil {
						reporter.SetModel(name, "failed", 0, "Download failed. Check the connection and retry.")
					} else {
						reporter.SetModel(name, "ready", 100, "")
					}
				}(name)
			}
			group.Wait()
		}()
		return nil
	}
}
