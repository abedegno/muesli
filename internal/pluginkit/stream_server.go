package pluginkit

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const defaultMaxStreamingSessions = 2

// ErrStreamingModelLoading tells the streaming server that the engine has
// started warming up and the session should remain open until it is ready.
var ErrStreamingModelLoading = errors.New("streaming model is loading")

// StreamingStartRequest describes the audio negotiated by a streaming client.
type StreamingStartRequest struct {
	Type         string          `json:"type"`
	LanguageHint string          `json:"language_hint,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
	SampleRate   int             `json:"sample_rate"`
	Channels     int             `json:"channels"`
}

// StreamingEvent is an event sent to a streaming client.
type StreamingEvent struct {
	Type    string  `json:"type"`
	Final   bool    `json:"final"`
	Text    string  `json:"text,omitempty"`
	T0      float64 `json:"t0,omitempty"`
	T1      float64 `json:"t1,omitempty"`
	Speaker *string `json:"speaker,omitempty"`
	Message string  `json:"message,omitempty"`
}

// StreamingTranscriber creates an independent engine session for each client.
type StreamingTranscriber interface {
	StartStream(ctx context.Context, req StreamingStartRequest) (StreamingEngineSession, error)
}

// StreamingEngineSession consumes normalized, interleaved PCM and returns any
// transcript events made available by that frame. Close releases session state.
type StreamingEngineSession interface {
	WriteAudio(ctx context.Context, pcm []float32) ([]StreamingEvent, error)
	Close(ctx context.Context) error
}

type streamingHandler struct {
	engine    StreamingTranscriber
	slots     chan struct{}
	serverCtx context.Context
	upgrader  websocket.Upgrader
}

func newStreamingHandler(cfg Config, engine StreamingTranscriber) http.Handler {
	limit := cfg.MaxStreamingSessions
	if limit <= 0 {
		limit = defaultMaxStreamingSessions
	}
	serverCtx := cfg.serveContext
	if serverCtx == nil {
		serverCtx = context.Background()
	}
	return &streamingHandler{
		engine:    engine,
		slots:     make(chan struct{}, limit),
		serverCtx: serverCtx,
		upgrader:  websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
}

func (h *streamingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(r.Context())
	handlerDone := make(chan struct{})
	shutdownWatchDone := make(chan struct{})
	defer func() { <-shutdownWatchDone }()
	defer close(handlerDone)
	defer cancel()
	go func() {
		defer close(shutdownWatchDone)
		select {
		case <-h.serverCtx.Done():
			cancel()
			_ = conn.Close()
		case <-handlerDone:
		}
	}()

	var start StreamingStartRequest
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return
	}
	if messageType != websocket.TextMessage || json.Unmarshal(payload, &start) != nil || start.Type != "start" || start.SampleRate <= 0 || start.Channels <= 0 {
		_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: "invalid streaming start request"})
		return
	}

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: "streaming session limit reached"})
		return
	}

	session, err := h.startSession(ctx, conn, start)
	if err != nil {
		_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: err.Error()})
		return
	}
	defer session.Close(context.WithoutCancel(ctx))
	if err := writeStreamingEvent(conn, StreamingEvent{Type: "ready"}); err != nil {
		return
	}

	for {
		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			pcm, err := decodePCM16LE(payload)
			if err != nil {
				_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: err.Error()})
				return
			}
			events, err := session.WriteAudio(ctx, pcm)
			if err != nil {
				_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: err.Error()})
				return
			}
			for _, event := range events {
				if err := validateStreamingEvent(event); err != nil {
					_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: err.Error()})
					return
				}
				if err := writeStreamingEvent(conn, event); err != nil {
					return
				}
			}
		case websocket.TextMessage:
			var control struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(payload, &control) == nil && control.Type == "stop" {
				return
			}
			_ = writeStreamingEvent(conn, StreamingEvent{Type: "error", Message: "invalid streaming control frame"})
			return
		}
	}
}

func (h *streamingHandler) startSession(ctx context.Context, conn *websocket.Conn, start StreamingStartRequest) (StreamingEngineSession, error) {
	loadingSent := false
	for {
		session, err := h.engine.StartStream(ctx, start)
		if !errors.Is(err, ErrStreamingModelLoading) {
			return session, err
		}
		if !loadingSent {
			if err := writeStreamingEvent(conn, StreamingEvent{Type: "loading"}); err != nil {
				return nil, err
			}
			loadingSent = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func decodePCM16LE(payload []byte) ([]float32, error) {
	if len(payload)%2 != 0 {
		return nil, errors.New("PCM16LE frame has an odd byte length")
	}
	pcm := make([]float32, len(payload)/2)
	for i := range pcm {
		pcm[i] = float32(int16(binary.LittleEndian.Uint16(payload[i*2:]))) / 32768
	}
	return pcm, nil
}

func validateStreamingEvent(event StreamingEvent) error {
	if event.Type != "segment" && event.Type != "error" {
		return fmt.Errorf("invalid streaming event type %q", event.Type)
	}
	return nil
}

func writeStreamingEvent(conn *websocket.Conn, event StreamingEvent) error {
	if math.IsNaN(event.T0) || math.IsNaN(event.T1) || math.IsInf(event.T0, 0) || math.IsInf(event.T1, 0) {
		return errors.New("streaming event contains invalid timestamp")
	}
	return conn.WriteJSON(event)
}
