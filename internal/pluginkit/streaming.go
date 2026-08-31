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

	// SilenceHysteresis tolerates a run of below-threshold audio, within an
	// already-active utterance, that is shorter than this duration: it is
	// treated as a natural dip in continuous speech (a breath, a quieter
	// syllable, a sibilant) rather than genuine silence, so it neither joins
	// the SilenceDuration finalize clock nor breaks the utterance. Only once
	// the low-energy run outlasts SilenceHysteresis does the excess start
	// counting toward SilenceDuration. Zero disables the tolerance and
	// reproduces the original single-frame threshold behavior.
	SilenceHysteresis time.Duration

	// VADFrame is the fixed duration of audio the detector classifies at a
	// time. Feed buffers input and hands the detector whole VADFrame-sized
	// subframes, so segmentation depends on the audio rather than on however
	// the caller happened to chunk it: desktop capture sends 200ms frames
	// while other clients may send anything, and one RMS decision spanning
	// 200ms discards quiet speech wholesale. Zero disables reframing and
	// classifies each Feed call as one indivisible frame, which is the
	// original behavior and leaves framing to the caller.
	VADFrame time.Duration
}

// DefaultStreamingConfig returns the recommended live-transcription defaults.
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		SampleRate:        16_000,
		MaxWindow:         30 * time.Second,
		PartialInterval:   1500 * time.Millisecond,
		SilenceDuration:   700 * time.Millisecond,
		EnergyThreshold:   0.01,
		SilenceHysteresis: 300 * time.Millisecond,
		VADFrame:          20 * time.Millisecond,
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

// maxPendingCommits bounds how many final commits can queue up behind an
// in-flight transcription. A single Feed call (or a continuous talker who
// never pauses) can cross several utterance boundaries while one
// transcription is still running; each of those needs to wait its turn
// rather than silently disappear. The bound is small and deliberate: an
// unbounded queue would let a session that never stops talking accumulate
// unbounded transcription backlog (and memory). Once the queue is full, the
// oldest still-queued commit is dropped to make room for the newest -- losing
// the earliest backlog under sustained overload is preferable to growing
// without bound, and no bound smaller than 1 can carry a Feed call spanning
// even two utterances.
const maxPendingCommits = 2

// pendingCommit is a final-segment snapshot captured while a transcription
// was already in flight, queued to run once that transcription completes.
type pendingCommit struct {
	samples []float32
	start   int64
	end     int64
}

// StreamingSession turns framed PCM input into partial and final transcript segments.
// Feed may be called concurrently; each session owns all of its mutable state.
type StreamingSession struct {
	cfg        StreamingConfig
	vad        VAD
	transcribe TranscribeFunc
	emit       SegmentHandler

	mu                    sync.Mutex
	pending               []float32
	window                []float32
	totalSamples          int64
	windowStartSample     int64
	lastSpeechEndSample   int64
	speechSincePartial    int64
	continuousSilence     int64
	lowEnergySamples      int64
	active                bool
	transcriptionInFlight bool
	pendingCommits        []pendingCommit
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
	if cfg.SilenceHysteresis < 0 {
		return nil, errors.New("streaming silence hysteresis must not be negative")
	}
	if cfg.VADFrame < 0 {
		return nil, errors.New("streaming vad frame must not be negative")
	}
	if cfg.VADFrame > 0 && durationSamples(cfg.VADFrame, cfg.SampleRate) == 0 {
		return nil, errors.New("streaming vad frame must span at least one sample")
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

// Feed accepts PCM from the caller in whatever chunk size it arrives. With
// cfg.VADFrame set, the audio is buffered and classified in fixed-duration
// subframes so that identical audio segments identically however it was
// chunked; any remainder shorter than one subframe is held until the next
// call completes it. With cfg.VADFrame zero, each call is classified whole.
func (s *StreamingSession) Feed(frame []float32) {
	if len(frame) == 0 {
		return
	}
	frameCopy := append([]float32(nil), frame...)

	s.mu.Lock()
	defer s.mu.Unlock()

	subframe := int(s.durationSamples(s.cfg.VADFrame))
	if subframe <= 0 {
		s.classifyLocked(frameCopy)
		return
	}
	s.pending = append(s.pending, frameCopy...)
	consumed := 0
	for len(s.pending)-consumed >= subframe {
		s.classifyLocked(s.pending[consumed : consumed+subframe])
		consumed += subframe
	}
	if consumed > 0 {
		s.pending = s.pending[:copy(s.pending, s.pending[consumed:])]
		// Compacting in place reuses the backing array, which would otherwise
		// stay as large as the biggest chunk the caller ever sent for the rest
		// of the session -- a single large upload would pin that allocation
		// across every meeting on a hosted server. Release it once it is far
		// larger than the remainder can ever need.
		if cap(s.pending) > 4*subframe {
			s.pending = append(make([]float32, 0, 2*subframe), s.pending...)
		}
	}
}

// classifyLocked runs the detector over exactly one frame and advances the
// utterance state machine. Callers must hold s.mu.
func (s *StreamingSession) classifyLocked(frameCopy []float32) {
	isSpeech := s.vad.IsSpeech(frameCopy)
	frameStart := s.totalSamples
	s.totalSamples += int64(len(frameCopy))

	if isSpeech {
		if !s.active {
			s.active = true
			s.windowStartSample = frameStart
		}
		s.continuousSilence = 0
		s.lowEnergySamples = 0
		s.appendSpeechLocked(frameCopy)
		return
	}

	if !s.active {
		return
	}

	// A run of below-threshold frames shorter than SilenceHysteresis is
	// tolerated as a natural dip within continuous speech rather than
	// genuine silence: the audio still joins the transcription window and
	// the utterance stays active, so a brief dip alone cannot trigger a
	// false final. Only the excess beyond the hysteresis window counts
	// toward the SilenceDuration finalize clock. With SilenceHysteresis
	// zero this reduces to the original single-frame threshold behavior.
	s.lowEnergySamples += int64(len(frameCopy))
	hysteresisSamples := s.durationSamples(s.cfg.SilenceHysteresis)
	if s.lowEnergySamples <= hysteresisSamples {
		s.appendSpeechLocked(frameCopy)
		return
	}

	s.continuousSilence = s.lowEnergySamples - hysteresisSamples
	if s.continuousSilence >= s.durationSamples(s.cfg.SilenceDuration) {
		s.startTranscriptionLocked(true)
		s.resetUtteranceLocked()
	}
}

// appendSpeechLocked folds one frame of speech (or hysteresis-tolerated,
// speech-adjacent) audio into the active utterance's transcription window
// and advances the partial-result cadence. Callers must hold s.mu.
func (s *StreamingSession) appendSpeechLocked(frame []float32) {
	s.speechSincePartial += int64(len(frame))
	s.lastSpeechEndSample = s.totalSamples
	s.window = append(s.window, frame...)
	s.boundWindow()
	if s.speechSincePartial >= s.durationSamples(s.cfg.PartialInterval) {
		s.speechSincePartial = 0
		s.startTranscriptionLocked(false)
	}
}

// Wait blocks until all transcription callbacks currently running (or queued
// behind one that is running) for the session finish. It does not itself
// start a final transcription for a still-open utterance -- callers that need
// the trailing utterance flushed before treating the session as done must
// call Finish instead. Callers must not call Feed concurrently with Wait.
func (s *StreamingSession) Wait() {
	s.wg.Wait()
}

// Finish forces whatever utterance is still active to a final transcription
// and blocks until it -- along with anything already in flight or queued
// ahead of it -- has been produced and handed to emit. Session shutdown (a
// client's stop frame, a disconnect, an explicit close) must call Finish
// rather than Wait: Wait only waits for work that has already started, so on
// its own it lets a still-open utterance's audio simply vanish. Feed must not
// be called concurrently with Finish.
func (s *StreamingSession) Finish() {
	s.mu.Lock()
	if s.active && len(s.window) > 0 {
		s.startTranscriptionLocked(true)
		s.resetUtteranceLocked()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *StreamingSession) boundWindow() {
	maxSamples := int(s.durationSamples(s.cfg.MaxWindow))
	if len(s.window) <= maxSamples {
		return
	}
	// The utterance has run long enough to fill the window without a silence
	// gap ever finalizing it (a continuous talker). Silently evicting the
	// oldest audio here used to mean only the trailing MaxWindow of a long
	// utterance was ever transcribed. Commit what has accumulated as a final
	// segment instead, then start a fresh window for the rest of the same
	// (still active) utterance.
	s.startTranscriptionLocked(true)
	s.beginNextWindowLocked()
}

// startTranscriptionLocked either launches a transcription of the current
// window immediately, or -- if one is already running -- decides what to do
// with this trigger. Callers must hold s.mu.
func (s *StreamingSession) startTranscriptionLocked(final bool) {
	if len(s.window) == 0 {
		return
	}
	if s.transcriptionInFlight {
		// A transcription cannot be re-entered concurrently; that invariant
		// is unconditional. What happens to this trigger while it waits
		// depends on what it represents. A partial (cadence) trigger is just
		// a preview refresh: the audio is still sitting in the window and
		// will be included in the next partial or the eventual final, so
		// dropping it here loses nothing permanent. A final (commit) trigger
		// is different -- it is about to hand off a snapshot of the window
		// and the window is about to be cleared for what comes next, so
		// dropping it would lose that utterance's audio outright. Queue it
		// (bounded by maxPendingCommits) instead, to run as soon as the
		// in-flight transcription -- and anything already ahead of it in the
		// queue -- finishes.
		if final {
			s.queueCommitLocked()
		}
		return
	}
	s.launchTranscriptionLocked(final, append([]float32(nil), s.window...), s.windowStartSample, s.lastSpeechEndSample)
}

// queueCommitLocked snapshots the current window as a final commit to run
// once the in-flight transcription completes. Callers must hold s.mu.
func (s *StreamingSession) queueCommitLocked() {
	if len(s.pendingCommits) >= maxPendingCommits {
		s.pendingCommits = s.pendingCommits[1:]
	}
	s.pendingCommits = append(s.pendingCommits, pendingCommit{
		samples: append([]float32(nil), s.window...),
		start:   s.windowStartSample,
		end:     s.lastSpeechEndSample,
	})
}

// dequeueCommitLocked pops the oldest queued commit, if any. Callers must
// hold s.mu.
func (s *StreamingSession) dequeueCommitLocked() (pendingCommit, bool) {
	if len(s.pendingCommits) == 0 {
		return pendingCommit{}, false
	}
	next := s.pendingCommits[0]
	s.pendingCommits = s.pendingCommits[1:]
	return next, true
}

// launchTranscriptionLocked starts one transcription goroutine for the given
// snapshot. On completion it re-acquires s.mu to hand off to the next queued
// commit (if any) before clearing transcriptionInFlight, so queued commits
// run one at a time in order without ever overlapping. Callers must hold
// s.mu.
func (s *StreamingSession) launchTranscriptionLocked(final bool, samples []float32, start, end int64) {
	s.transcriptionInFlight = true
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
		if next, ok := s.dequeueCommitLocked(); ok {
			s.launchTranscriptionLocked(true, next.samples, next.start, next.end)
		} else {
			s.transcriptionInFlight = false
		}
		s.mu.Unlock()
		if err != nil || segment.Text != "" {
			s.emit(segment, err)
		}
	}()
}

// beginNextWindowLocked starts accumulating a fresh transcription window for
// an utterance that remains active (e.g. after a max-window commit) -- unlike
// resetUtteranceLocked, which ends the utterance entirely. Callers must hold
// s.mu.
func (s *StreamingSession) beginNextWindowLocked() {
	s.window = nil
	s.speechSincePartial = 0
	s.windowStartSample = s.totalSamples
	s.lastSpeechEndSample = s.totalSamples
}

func (s *StreamingSession) resetUtteranceLocked() {
	s.window = nil
	s.active = false
	s.continuousSilence = 0
	s.lowEnergySamples = 0
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
