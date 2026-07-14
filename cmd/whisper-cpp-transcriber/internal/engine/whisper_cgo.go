//go:build whisper_cgo

// Package engine (whisper_cgo build) implements the real whisper.cpp cgo
// transcription backend. It is only compiled when built with
// `-tags whisper_cgo` and requires the whisper.cpp static libraries and
// headers to be present on the build machine (see
// cmd/whisper-cpp-transcriber/README or the build-whisper-lib CI workflow).
// The default (untagged) build uses the pure-Go stub in engine.go instead.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
)

const (
	DefaultName     = "muesli-whisper-cpp-transcriber"
	DefaultVersion  = "0.1.0"
	DefaultModel    = "ggml-tiny.en"
	DefaultLanguage = "auto"
)

var ConfigSchema = json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","title":"Model","description":"Model name reported by the whisper.cpp transcriber","default":"ggml-tiny.en"},"language":{"type":"string","title":"Language","description":"Language code to force, or \"auto\" to let whisper.cpp detect it","default":"auto"}},"additionalProperties":false}`)

// Config configures the whisper.cpp cgo engine. Fields mirror the stub
// engine's Config so that main.go and its tests work unchanged under either
// build tag.
type Config struct {
	ModelDir string
	ModelURL string
	Model    string
	Language string
}

// Engine runs transcription through the real whisper.cpp cgo bindings.
type Engine struct {
	cfg Config

	modelOnce sync.Once
	modelErr  error
	model     whisper.Model
	modelPath string

	// mu serializes calls into the whisper.cpp C context. whisper.cpp's
	// whisper_full() is not safe to call concurrently against the same
	// loaded model, so we guard it with a plain mutex rather than trying to
	// keep one whisper.Context per in-flight request.
	mu sync.Mutex
}

func New(cfg Config) *Engine {
	cfg.ModelDir = strings.TrimSpace(cfg.ModelDir)
	cfg.ModelURL = strings.TrimSpace(cfg.ModelURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Language = strings.TrimSpace(cfg.Language)
	return &Engine{cfg: cfg}
}

type pluginConfig struct {
	Model    string `json:"model"`
	Language string `json:"language"`
}

func (e *Engine) Transcribe(ctx context.Context, pcm []float32, opts pluginkit.TranscribeRequest) (pluginkit.TranscribeResult, error) {
	cfg := e.effectiveConfig(opts.Config)
	if err := e.ensureModel(ctx); err != nil {
		return pluginkit.TranscribeResult{}, err
	}

	// whisper.cpp has no context cancellation hook; the caller's ctx is only
	// used for the (one-time) model download above.
	e.mu.Lock()
	defer e.mu.Unlock()

	wctx, err := e.model.NewContext()
	if err != nil {
		return pluginkit.TranscribeResult{}, fmt.Errorf("whisper_cgo: new context: %w", err)
	}

	if wctx.IsMultilingual() {
		lang := cfg.Language
		if lang == "" {
			lang = "auto"
		}
		if err := wctx.SetLanguage(lang); err != nil {
			return pluginkit.TranscribeResult{}, fmt.Errorf("whisper_cgo: set language %q: %w", lang, err)
		}
	}

	if err := wctx.Process(pcm, nil, nil, nil); err != nil {
		return pluginkit.TranscribeResult{}, fmt.Errorf("whisper_cgo: process: %w", err)
	}

	var segments []model.Segment
	for {
		seg, err := wctx.NextSegment()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return pluginkit.TranscribeResult{}, fmt.Errorf("whisper_cgo: next segment: %w", err)
		}
		// seg.Start / seg.End are already whisper's t0/t1 (in centiseconds)
		// scaled to a time.Duration (t*10ms) by the bindings.
		segments = append(segments, model.Segment{
			StartMS: int(seg.Start.Milliseconds()),
			EndMS:   int(seg.End.Milliseconds()),
			Text:    strings.TrimSpace(seg.Text),
			Source:  "whisper-cpp",
		})
	}

	durationMS := len(pcm) * 1000 / 16000

	return pluginkit.TranscribeResult{
		Segments:   segments,
		Language:   wctx.DetectedLanguage(),
		Model:      cfg.Model,
		DurationMS: durationMS,
	}, nil
}

func (e *Engine) ensureModel(ctx context.Context) error {
	e.modelOnce.Do(func() {
		if e.cfg.ModelDir == "" || e.cfg.ModelURL == "" {
			e.modelErr = errors.New("whisper_cgo: model-dir and model-url are required")
			return
		}
		path, err := pluginkit.EnsureModel(ctx, e.cfg.ModelDir, e.cfg.ModelURL, nil)
		if err != nil {
			e.modelErr = fmt.Errorf("whisper_cgo: ensure model: %w", err)
			return
		}
		m, err := whisper.New(path)
		if err != nil {
			e.modelErr = fmt.Errorf("whisper_cgo: load model %s: %w", path, err)
			return
		}
		e.modelPath = path
		e.model = m
	})
	return e.modelErr
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
