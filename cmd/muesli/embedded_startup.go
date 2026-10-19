package main

import (
	"context"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embedded"
)

const modelPullTimeout = 30 * time.Minute

type embeddedStartupHooks struct {
	migrate       func(string) error
	detect        func(context.Context, string) bool
	configure     func(*config.Config, string, bool)
	pullEmbedding func(context.Context, string, string, embedded.PullProgressFunc) error
	pullModel     func(context.Context, string, string, embedded.PullProgressFunc) error
	onReady       func()
}

func runEmbeddedStartupPhases(ctx context.Context, cfg *config.Config, reporter *embedded.Reporter, ollamaURL string, hooks embeddedStartupHooks) (complete func(), err error) {
	if reporter == nil {
		return func() {}, nil
	}

	reporter.Advance(embedded.PhaseMigrate, "running migrations")
	if err := hooks.migrate(cfg.DatabaseURL); err != nil {
		return nil, err
	}

	reporter.Advance(embedded.PhaseOllamaCheck, "checking Ollama")
	detected := hooks.detect(ctx, ollamaURL)
	if detected {
		reporter.Advance(embedded.PhaseOllamaCheck, "Ollama detected")
	} else {
		reporter.Advance(embedded.PhaseOllamaCheck, "Ollama unavailable")
	}
	hooks.configure(cfg, ollamaURL, detected)
	reporter.SetDegraded(cfg.EmbeddedDegraded)

	if !detected {
		return func() {
			reporter.Advance(embedded.PhaseReady, "ready")
			if hooks.onReady != nil {
				hooks.onReady()
			}
		}, nil
	}

	reporter.Advance(embedded.PhaseModelPull, "pulling Ollama models")
	reporter.SetModel(cfg.EmbeddingsModel, "downloading", 0, "")
	reporter.SetModel(embedded.DefaultOllamaAgentModel, "downloading", 0, "")

	go func() {
		pullCtx, cancel := context.WithTimeout(ctx, modelPullTimeout)
		defer cancel()
		if err := hooks.pullEmbedding(pullCtx, ollamaURL, cfg.EmbeddingsModel, func(percent int) {
			reporter.SetSourcePercent("embedding", percent)
			reporter.SetModel(cfg.EmbeddingsModel, "downloading", percent, "")
		}); err != nil {
			reporter.SetModel(cfg.EmbeddingsModel, "failed", 0, "Download failed. Check the connection and retry.")
		} else {
			reporter.SetModel(cfg.EmbeddingsModel, "ready", 100, "")
		}
	}()
	go func() {
		pullCtx, cancel := context.WithTimeout(ctx, modelPullTimeout)
		defer cancel()
		if err := hooks.pullModel(pullCtx, ollamaURL, embedded.DefaultOllamaAgentModel, func(percent int) {
			reporter.SetSourcePercent("agent", percent)
			reporter.SetModel(embedded.DefaultOllamaAgentModel, "downloading", percent, "")
		}); err != nil {
			reporter.SetModel(embedded.DefaultOllamaAgentModel, "failed", 0, "Download failed. Check the connection and retry.")
		} else {
			reporter.SetModel(embedded.DefaultOllamaAgentModel, "ready", 100, "")
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			reporter.Advance(embedded.PhaseReady, "ready; optional AI models may still be downloading")
			if hooks.onReady != nil {
				hooks.onReady()
			}
		})
	}, nil
}

// applyEmbeddedAgentDefault points cfg.DefaultAgentURL at the embedded Ollama
// agent's endpoint once it has been registered as the DB default plugin (see
// store.EnsureDefaultPlugin in main.go), so /readyz's "agent" dep check
// reflects the real registered default instead of remaining unconfigured
// because MUESLI_DEFAULT_AGENT_URL is unset in embedded mode. No-op if
// endpointURL is empty.
func applyEmbeddedAgentDefault(cfg *config.Config, endpointURL string) {
	if endpointURL == "" {
		return
	}
	cfg.DefaultAgentURL = endpointURL
}
