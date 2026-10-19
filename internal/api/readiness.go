package api

import (
	"context"
	"errors"
	"github.com/abedegno/muesli/internal/embedded"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"net/http"
	"sync"
	"time"
)

type serviceReadiness struct {
	ID         string `json:"id"`
	Configured bool   `json:"configured"`
	Healthy    bool   `json:"healthy"`
}
type readinessCache struct {
	mu       sync.Mutex
	services []serviceReadiness
	at       time.Time
	pending  chan struct{}
}

func (s *Server) probeServices(ctx context.Context) []serviceReadiness {
	result := make([]serviceReadiness, 4)
	var group sync.WaitGroup
	for i, kind := range []string{model.PluginTranscriber, model.PluginStreamingTranscriber, model.PluginAgent} {
		group.Add(1)
		go func(i int, kind string) {
			defer group.Done()
			result[i].ID = kind
			p, err := s.deps.Store.DefaultPlugin(ctx, s.deps.Crypto, kind)
			if errors.Is(err, store.ErrNotFound) {
				return
			}
			result[i].Configured = true
			result[i].Healthy = err == nil && s.pluginHealthProber()(ctx, p.EndpointURL, p.Token) == nil
		}(i, kind)
	}
	result[3] = serviceReadiness{ID: "semantic-search", Configured: s.deps.Embedder != nil}
	if s.deps.Embedder != nil {
		group.Add(1)
		go func() {
			defer group.Done()
			vector, err := s.deps.Embedder.Embed(ctx, "Muesli readiness check")
			result[3].Healthy = err == nil && len(vector) > 0 && len(vector) == s.deps.Embedder.Dim()
		}()
	}
	group.Wait()
	return result
}
func (s *Server) readinessServices(ctx context.Context) ([]serviceReadiness, error) {
	cache := &s.readiness
	for {
		cache.mu.Lock()
		if time.Since(cache.at) < 5*time.Second {
			result := append([]serviceReadiness(nil), cache.services...)
			cache.mu.Unlock()
			return result, nil
		}
		if pending := cache.pending; pending != nil {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		cache.pending = pending
		cache.mu.Unlock()
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result := s.probeServices(probeCtx)
		cancel()
		cache.mu.Lock()
		cache.services = result
		cache.at = time.Now()
		cache.pending = nil
		close(pending)
		cache.mu.Unlock()
		return result, nil
	}
}
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	services, err := s.readinessServices(r.Context())
	if err != nil {
		writeError(w, 503, "could not check service readiness")
		return
	}
	models := []embedded.ModelDownload{}
	if s.deps.EmbeddedProgress != nil {
		models = s.deps.EmbeddedProgress.ModelsSnapshot()
	}
	writeJSON(w, 200, map[string]any{"services": services, "models": models, "managed_models": s.deps.RetryAIModels != nil})
}
func (s *Server) handleRetryAIModels(w http.ResponseWriter, r *http.Request) {
	if s.deps.RetryAIModels == nil {
		writeError(w, 409, "AI models are managed outside this server; ask your administrator")
		return
	}
	if err := s.deps.RetryAIModels(); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 202, map[string]string{"status": "downloading"})
}
