package pluginkit

import (
	"errors"
	"math"
	"strings"
	"sync"
	"time"
)

// VAD classifies one PCM frame as speech or silence.
type VAD interface {
	IsSpeech(frame []float32) bool
}

// EnergyVAD is a deliberately simple, dependency-free RMS energy detector.
// More capable voice activity detectors can be supplied through VAD.
type EnergyVAD struct {
	Threshold float64
}

// IsSpeech reports whether the frame's root-mean-square energy reaches the threshold.
func (v EnergyVAD) IsSpeech(frame []float32) bool {
	if len(frame) == 0 {
		return false
	}
	var sum float64
	for _, sample := range frame {
		x := float64(sample)
		sum += x * x
	}
	return math.Sqrt(sum/float64(len(frame))) >= v.Threshold
}

// StreamingConfig controls the audio timing and memory bounds of a StreamingSession.
type StreamingConfig struct {
	SampleRate      int
	MaxWindow       time.Duration
	PartialInterval time.Duration
	SilenceDuration time.Duration
	EnergyThreshold float64
}

// DefaultStreamingConfig returns the recommended live-transcription defaults.
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		SampleRate:      16_000,
		MaxWindow:       30 * time.Second,
		PartialInterval: 1500 * time.Millisecond,
		SilenceDuration: 700 * time.Millisecond,
		EnergyThreshold: 0.01,
	}
}

// StreamingSegment is one partial or final transcription result.
type StreamingSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
	Final   bool
}

// TranscribeFunc converts mono float32 PCM at the session's configured sample rate to text.
type TranscribeFunc func(samples []float32) (string, error)

// SegmentHandler receives completed transcription attempts. A failed attempt has a zero
// segment and a non-nil error. Calls can occur on the session's transcription goroutine.
type SegmentHandler func(segment StreamingSegment, err error)

// StreamingSession turns framed PCM input into partial and final transcript segments.
// Feed may be called concurrently; each session owns all of its mutable state.
type StreamingSession struct {
	cfg        StreamingConfig
	vad        VAD
	transcribe TranscribeFunc
	emit       SegmentHandler

	mu                    sync.Mutex
	window                []float32
	totalSamples          int64
	windowStartSample     int64
	lastSpeechEndSample   int64
	speechSincePartial    int64
	continuousSilence     int64
	active                bool
	transcriptionInFlight bool
	wg                    sync.WaitGroup
}

// NewStreamingSession constructs an independent streaming session.
func NewStreamingSession(cfg StreamingConfig, vad VAD, transcribe TranscribeFunc, emit SegmentHandler) (*StreamingSession, error) {
	if cfg.SampleRate <= 0 {
		return nil, errors.New("streaming sample rate must be positive")
	}
	if cfg.MaxWindow <= 0 {
		return nil, errors.New("streaming max window must be positive")
	}
	if cfg.PartialInterval <= 0 {
		return nil, errors.New("streaming partial interval must be positive")
	}
	if cfg.SilenceDuration <= 0 {
		return nil, errors.New("streaming silence duration must be positive")
	}
	if cfg.EnergyThreshold < 0 {
		return nil, errors.New("streaming energy threshold must not be negative")
	}
	if durationSamples(cfg.MaxWindow, cfg.SampleRate) == 0 ||
		durationSamples(cfg.PartialInterval, cfg.SampleRate) == 0 ||
		durationSamples(cfg.SilenceDuration, cfg.SampleRate) == 0 {
		return nil, errors.New("streaming durations must span at least one sample")
	}
	if transcribe == nil || emit == nil {
		return nil, errors.New("streaming transcriber and segment handler are required")
	}
	if vad == nil {
		vad = EnergyVAD{Threshold: cfg.EnergyThreshold}
	}
	return &StreamingSession{cfg: cfg, vad: vad, transcribe: transcribe, emit: emit}, nil
}

// Feed processes one PCM frame. Frame duration is derived from its sample count.
func (s *StreamingSession) Feed(frame []float32) {
	if len(frame) == 0 {
		return
	}
	frameCopy := append([]float32(nil), frame...)

	s.mu.Lock()
	defer s.mu.Unlock()
	isSpeech := s.vad.IsSpeech(frameCopy)
	frameStart := s.totalSamples
	s.totalSamples += int64(len(frameCopy))

	if isSpeech {
		if !s.active {
			s.active = true
			s.windowStartSample = frameStart
		}
		s.continuousSilence = 0
		s.speechSincePartial += int64(len(frameCopy))
		s.lastSpeechEndSample = s.totalSamples
		s.window = append(s.window, frameCopy...)
		s.boundWindow()
		if s.speechSincePartial >= s.durationSamples(s.cfg.PartialInterval) {
			s.speechSincePartial = 0
			s.startTranscriptionLocked(false)
		}
		return
	}

	if !s.active {
		return
	}
	s.continuousSilence += int64(len(frameCopy))
	if s.continuousSilence >= s.durationSamples(s.cfg.SilenceDuration) {
		s.startTranscriptionLocked(true)
		s.resetUtteranceLocked()
	}
}

// Wait blocks until all transcription callbacks currently running for the session finish.
// Callers must not call Feed concurrently with Wait.
func (s *StreamingSession) Wait() {
	s.wg.Wait()
}

func (s *StreamingSession) boundWindow() {
	maxSamples := int(s.durationSamples(s.cfg.MaxWindow))
	if len(s.window) <= maxSamples {
		return
	}
	dropped := len(s.window) - maxSamples
	copy(s.window, s.window[dropped:])
	s.window = s.window[:maxSamples]
	s.windowStartSample += int64(dropped)
}

func (s *StreamingSession) startTranscriptionLocked(final bool) {
	// This is intentionally a drop, not a wait or queue: cadence and final triggers
	// that occur while the callback is busy disappear here.
	if s.transcriptionInFlight || len(s.window) == 0 {
		return
	}
	s.transcriptionInFlight = true
	samples := append([]float32(nil), s.window...)
	start := s.windowStartSample
	end := s.lastSpeechEndSample
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		text, err := s.transcribe(samples)
		segment := StreamingSegment{}
		if err == nil {
			segment = StreamingSegment{
				StartMS: s.samplesToMS(start),
				EndMS:   s.samplesToMS(end),
				Text:    strings.TrimSpace(text),
				Final:   final,
			}
		}
		s.mu.Lock()
		s.transcriptionInFlight = false
		s.mu.Unlock()
		if err != nil || segment.Text != "" {
			s.emit(segment, err)
		}
	}()
}

func (s *StreamingSession) resetUtteranceLocked() {
	s.window = nil
	s.active = false
	s.continuousSilence = 0
	s.speechSincePartial = 0
	s.windowStartSample = 0
	s.lastSpeechEndSample = 0
}

func (s *StreamingSession) durationSamples(d time.Duration) int64 {
	return durationSamples(d, s.cfg.SampleRate)
}

func (s *StreamingSession) samplesToMS(samples int64) int64 {
	return samples * int64(time.Second/time.Millisecond) / int64(s.cfg.SampleRate)
}

func durationSamples(d time.Duration, sampleRate int) int64 {
	return int64(d) * int64(sampleRate) / int64(time.Second)
}
