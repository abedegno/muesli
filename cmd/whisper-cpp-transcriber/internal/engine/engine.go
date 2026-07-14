package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
)

const (
	DefaultName     = "muesli-whisper-cpp-transcriber"
	DefaultVersion  = "0.1.0"
	DefaultModel    = "whisper-cpp-stub"
	DefaultLanguage = "en"
)

var ConfigSchema = json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","title":"Model","description":"Model name reported by the stub transcriber","default":"whisper-cpp-stub"},"language":{"type":"string","title":"Language","description":"Language code reported by the stub transcriber","default":"en"}},"additionalProperties":false}`)

type Config struct {
	ModelDir string
	ModelURL string
	Model    string
	Language string
}

type Engine struct {
	cfg       Config
	modelOnce sync.Once
	modelErr  error
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
	e.modelOnce.Do(func() {
		if e.cfg.ModelDir == "" || e.cfg.ModelURL == "" {
			return
		}
		_, e.modelErr = pluginkit.EnsureModel(ctx, e.cfg.ModelDir, e.cfg.ModelURL, nil)
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
