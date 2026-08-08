//go:build !whisper_cgo

package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
)

const (
	DefaultName     = "muesli-whisper-cpp-transcriber"
	DefaultVersion  = "0.1.0"
	DefaultModel    = "whisper-cpp-stub"
	DefaultLanguage = "en"
)

var ConfigSchema = json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","title":"Model","description":"Model name reported by the stub transcriber","default":"whisper-cpp-stub"},"language":{"type":"string","title":"Language","description":"Language code reported by the stub transcriber","default":"en"},"multitrack":{"type":"boolean","title":"Multitrack","description":"Split stereo input into per-channel transcription passes","default":false}},"additionalProperties":false}`)

type Config struct {
	ModelDir string
	ModelURL string
	Model    string
	Language string
}

type modelState int

const (
	modelUnloaded modelState = iota
	modelLoading
	modelReady
	modelError
)

type Engine struct {
	cfg      Config
	mu       sync.Mutex
	state    modelState
	loadErr  error
	loadedAt time.Time
	loadDone chan struct{}
}

func New(cfg Config) *Engine {
	cfg.ModelDir = strings.TrimSpace(cfg.ModelDir)
	cfg.ModelURL = strings.TrimSpace(cfg.ModelURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Language = strings.TrimSpace(cfg.Language)
	return &Engine{cfg: cfg}
}

// EnsureReady lazily prepares the configured model without running inference.
func (e *Engine) EnsureReady(ctx context.Context) error { return e.ensureModel(ctx) }

type pluginConfig struct {
	Model    string `json:"model"`
	Language string `json:"language"`
}

func (e *Engine) Transcribe(ctx context.Context, pcm []float32, opts pluginkit.TranscribeRequest) (pluginkit.TranscribeResult, error) {
	cfg := e.effectiveConfig(opts.Config)
	if err := e.ensureModel(ctx); err != nil {
		return pluginkit.TranscribeResult{}, err
	}

	durationMS := len(pcm) * 1000 / 16000

	return pluginkit.TranscribeResult{
		Segments: []model.Segment{
			{
				StartMS: 0,
				EndMS:   durationMS,
				Text:    "whisper.cpp stub transcription",
				Source:  "stub",
			},
		},
		Language:   cfg.Language,
		Model:      cfg.Model,
		DurationMS: durationMS,
	}, nil
}

func (e *Engine) ensureModel(ctx context.Context) error {
	e.mu.Lock()
	for e.state == modelLoading {
		done := e.loadDone
		e.mu.Unlock()
		if done == nil {
			return nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
		e.mu.Lock()
	}
	if e.state == modelReady {
		e.mu.Unlock()
		return nil
	}
	done := make(chan struct{})
	e.state = modelLoading
	e.loadDone = done
	e.mu.Unlock()

	var err error
	if e.cfg.ModelDir == "" || e.cfg.ModelURL == "" {
		e.mu.Lock()
		e.state = modelReady
		e.loadErr = nil
		e.loadedAt = time.Now()
		close(done)
		e.loadDone = nil
		e.mu.Unlock()
		return nil
	}
	_, err = pluginkit.EnsureModel(ctx, e.cfg.ModelDir, e.cfg.ModelURL, nil)
	e.mu.Lock()
	if err != nil {
		e.state = modelError
		e.loadErr = err
		close(done)
		e.loadDone = nil
		e.mu.Unlock()
		return err
	}
	e.state = modelReady
	e.loadErr = nil
	e.loadedAt = time.Now()
	close(done)
	e.loadDone = nil
	e.mu.Unlock()
	return nil
}

func (e *Engine) Status() (status, model string, percent int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch e.state {
	case modelReady:
		return "ready", e.cfg.Model, 100
	case modelLoading:
		return "downloading", e.cfg.Model, 0
	case modelError:
		return "error", e.cfg.Model, 0
	default:
		return "unknown", e.cfg.Model, 0
	}
}

func (e *Engine) effectiveConfig(raw json.RawMessage) pluginConfig {
	cfg := pluginConfig{
		Model:    e.cfg.Model,
		Language: e.cfg.Language,
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Language == "" {
		cfg.Language = DefaultLanguage
	}
	if len(raw) == 0 || string(raw) == "null" {
		return cfg
	}
	var override pluginConfig
	if err := json.Unmarshal(raw, &override); err != nil {
		return cfg
	}
	if strings.TrimSpace(override.Model) != "" {
		cfg.Model = strings.TrimSpace(override.Model)
	}
	if strings.TrimSpace(override.Language) != "" {
		cfg.Language = strings.TrimSpace(override.Language)
	}
	return cfg
}
