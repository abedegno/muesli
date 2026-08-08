package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

// Engine adapts the shared whisper.cpp batch engine to pluginkit's disposable
// live-session protocol. It never persists or forwards live text to batch jobs.
type Engine struct {
	whisper  *engine.Engine
	model    string
	language string
	loadMu   sync.Mutex
	loading  bool
}

func New(cfg engine.Config) *Engine {
	return &Engine{whisper: engine.New(cfg), model: cfg.Model, language: cfg.Language}
}

func (e *Engine) Status() (string, string, int) { return e.whisper.Status() }

func (e *Engine) StartStream(_ context.Context, req pluginkit.StreamingStartRequest) (pluginkit.StreamingEngineSession, error) {
	status, model, _ := e.whisper.Status()
	if status != "ready" {
		e.startLoading()
		if status == "error" {
			return nil, fmt.Errorf("model %s is retrying download", model)
		}
		return nil, fmt.Errorf("model %s is downloading; retry shortly", model)
	}
	if req.SampleRate <= 0 || req.Channels <= 0 {
		return nil, errors.New("sample rate and channels must be positive")
	}
	s := &session{parent: e, inputRate: req.SampleRate, channels: req.Channels}
	cfg := pluginkit.DefaultStreamingConfig()
	var err error
	s.stream, err = pluginkit.NewStreamingSession(cfg, nil, s.transcribe, s.emit)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (e *Engine) startLoading() {
	e.loadMu.Lock()
	if e.loading {
		e.loadMu.Unlock()
		return
	}
	e.loading = true
	e.loadMu.Unlock()
	go func() {
		_ = e.whisper.EnsureReady(context.Background())
		e.loadMu.Lock()
		e.loading = false
		e.loadMu.Unlock()
	}()
}

type session struct {
	parent    *Engine
	inputRate int
	channels  int
	stream    *pluginkit.StreamingSession
	mu        sync.Mutex
	events    []pluginkit.StreamingEvent
}

func (s *session) WriteAudio(ctx context.Context, pcm []float32) ([]pluginkit.StreamingEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mono, err := normalize(pcm, s.channels, s.inputRate, 16_000)
	if err != nil {
		return nil, err
	}
	s.stream.Feed(mono)
	s.stream.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	events := append([]pluginkit.StreamingEvent(nil), s.events...)
	s.events = s.events[:0]
	return events, nil
}

func (s *session) Close(context.Context) error {
	s.stream.Wait()
	return nil
}

func (s *session) transcribe(samples []float32) (string, error) {
	raw, _ := json.Marshal(map[string]string{"model": s.parent.model, "language": s.parent.language})
	result, err := s.parent.whisper.Transcribe(context.Background(), samples, pluginkit.TranscribeRequest{Config: raw})
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(result.Segments))
	for _, segment := range result.Segments {
		if text := strings.TrimSpace(segment.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " "), nil
}

func (s *session) emit(segment pluginkit.StreamingSegment, err error) {
	event := pluginkit.StreamingEvent{Type: "segment", Final: segment.Final, Text: segment.Text, T0: float64(segment.StartMS) / 1000, T1: float64(segment.EndMS) / 1000}
	if err != nil {
		event = pluginkit.StreamingEvent{Type: "error", Message: err.Error()}
	}
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func normalize(interleaved []float32, channels, fromRate, toRate int) ([]float32, error) {
	if channels <= 0 || len(interleaved)%channels != 0 {
		return nil, errors.New("audio frame is not aligned to its channel count")
	}
	frames := len(interleaved) / channels
	mono := make([]float32, frames)
	for i := range frames {
		for channel := range channels {
			mono[i] += interleaved[i*channels+channel]
		}
		mono[i] /= float32(channels)
	}
	if fromRate == toRate || len(mono) == 0 {
		return mono, nil
	}
	outLen := len(mono) * toRate / fromRate
	if outLen == 0 {
		return nil, nil
	}
	out := make([]float32, outLen)
	for i := range out {
		source := i * fromRate / toRate
		if source >= len(mono) {
			source = len(mono) - 1
		}
		out[i] = mono[source]
	}
	return out, nil
}
