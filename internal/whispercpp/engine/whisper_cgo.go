//go:build whisper_cgo

// Package engine (whisper_cgo build) implements the real whisper.cpp cgo
// transcription backend. It is only compiled when built with
// `-tags whisper_cgo` and requires the whisper.cpp static libraries and
// headers to be present on the build machine (see
// .github/workflows/build-whisper-lib.yml, which produces the vendored
// artifact this links against).
//
// Linking happens as part of the `go build`/`go test` invocation itself, so
// a missing library/header is a COMPILE-time failure, not something any
// Go-level code (including a test's t.Skip) can guard against. Use
// `make test-whisper-cgo` (scripts/test-whisper-cgo.sh) rather than a raw
// `go test -tags whisper_cgo ...` in any context where the vendored libs
// might not be present -- it checks for them before invoking the Go
// toolchain at all, so it never reaches the link step when they're absent.
//
// The default (untagged) build uses the pure-Go stub in engine.go instead,
// and is unaffected by any of this.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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

var ConfigSchema = json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","title":"Model","description":"Model name reported by the whisper.cpp transcriber","default":"ggml-tiny.en"},"language":{"type":"string","title":"Language","description":"Language code to force, or \"auto\" to let whisper.cpp detect it","default":"auto"},"multitrack":{"type":"boolean","title":"Multitrack","description":"Split stereo input into per-channel transcription passes","default":false}},"additionalProperties":false}`)

// Config configures the whisper.cpp cgo engine. Fields mirror the stub
// engine's Config so that main.go and its tests work unchanged under either
// build tag.
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

// Engine runs transcription through the real whisper.cpp cgo bindings.
type Engine struct {
	cfg Config

	mu        sync.Mutex
	state     modelState
	loadErr   error
	loadedAt  time.Time
	loadDone  chan struct{}
	model     whisper.Model
	modelPath string

	// mu serializes calls into the whisper.cpp C context. whisper.cpp's
	// whisper_full() is not safe to call concurrently against the same
	// loaded model, so we guard it with a plain mutex rather than trying to
	// keep one whisper.Context per in-flight request.
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

	var (
		path string
		m    whisper.Model
		err  error
	)
	if e.cfg.ModelDir == "" {
		err = errors.New("whisper_cgo: model-dir is required")
	} else {
		// Prefer a pre-bundled model file (the packaged app ships one at
		// ModelDir/<Model>.bin, no MODEL_URL); fall back to downloading from
		// model-url for dev/hosted. See pluginkit.ResolveModel.
		path, err = pluginkit.ResolveModel(ctx, e.cfg.ModelDir, e.cfg.Model, e.cfg.ModelURL, nil)
		if err == nil {
			m, err = whisper.New(path)
		}
	}
	if err != nil {
		e.mu.Lock()
		e.state = modelError
		if e.cfg.ModelDir == "" {
			e.loadErr = err
		} else if path == "" {
			e.loadErr = fmt.Errorf("whisper_cgo: ensure model: %w", err)
		} else {
			e.loadErr = fmt.Errorf("whisper_cgo: load model %s: %w", path, err)
		}
		close(done)
		e.loadDone = nil
		e.mu.Unlock()
		return e.loadErr
	}
	e.mu.Lock()
	e.modelPath = path
	e.model = m
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
