package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// StreamingStartRequest is the start control frame sent to /stream.
type StreamingStartRequest struct {
	Type         string          `json:"type"`
	LanguageHint string          `json:"language_hint,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
	SampleRate   int             `json:"sample_rate"`
	Channels     int             `json:"channels"`
}

// StreamingEvent is one control frame emitted by the plugin stream.
type StreamingEvent struct {
	Type    string  `json:"type"`
	Final   bool    `json:"final,omitempty"`
	Text    string  `json:"text,omitempty"`
	T0      float64 `json:"t0,omitempty"`
	T1      float64 `json:"t1,omitempty"`
	Speaker *string `json:"speaker,omitempty"`
	Message string  `json:"message,omitempty"`
}

// StreamingSession manages a live websocket transcription session.
type StreamingSession struct {
	conn *websocket.Conn
}

// StreamingClient dials the plugin's websocket endpoint using the same bearer
// envelope as the HTTP client.
type StreamingClient struct {
	baseURL string
	token   string
	dialer  websocket.Dialer
}

// NewStreaming builds a websocket client for the plugin's /stream endpoint.
func NewStreaming(baseURL, token string) *StreamingClient {
	return &StreamingClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		dialer: websocket.Dialer{
			HandshakeTimeout: 30 * time.Second,
		},
	}
}

// Open establishes the websocket connection, sends the start frame, and waits
// for the plugin's ready acknowledgement.
func (c *StreamingClient) Open(ctx context.Context, req StreamingStartRequest) (*StreamingSession, error) {
	if req.Type == "" {
		req.Type = "start"
	}
	if len(req.Options) == 0 {
		req.Options = json.RawMessage(`{}`)
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage(`{}`)
	}

	wsURL, err := wsURL(c.baseURL, "/stream")
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.token)
	headers.Set("X-Muesli-Plugin-API", PluginAPIVersion)

	conn, _, err := c.dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, err
	}
	sess := &StreamingSession{conn: conn}
	if err := sess.writeJSON(req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	ev, err := sess.Recv()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ev.Type != "ready" {
		_ = conn.Close()
		if ev.Type == "error" {
			return nil, fmt.Errorf("streaming plugin error: %s", ev.Message)
		}
		return nil, fmt.Errorf("unexpected streaming plugin event %q", ev.Type)
	}
	return sess, nil
}

// WriteAudio sends one raw PCM frame to the plugin.
func (s *StreamingSession) WriteAudio(frame []byte) error {
	if len(frame) == 0 {
		return nil
	}
	return s.writeMessage(websocket.BinaryMessage, frame)
}

// Stop sends the final stop control message.
func (s *StreamingSession) Stop() error {
	return s.writeJSON(map[string]string{"type": "stop"})
}

// Recv reads one control frame from the plugin.
func (s *StreamingSession) Recv() (StreamingEvent, error) {
	_, payload, err := s.conn.ReadMessage()
	if err != nil {
		return StreamingEvent{}, err
	}
	var ev StreamingEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return StreamingEvent{}, err
	}
	switch ev.Type {
	case "ready", "segment", "error":
		return ev, nil
	default:
		return StreamingEvent{}, fmt.Errorf("unexpected streaming event type %q", ev.Type)
	}
}

// Close terminates the websocket session.
func (s *StreamingSession) Close() error {
	return s.conn.Close()
}

func (s *StreamingSession) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.writeMessage(websocket.TextMessage, b)
}

func (s *StreamingSession) writeMessage(mt int, payload []byte) error {
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.conn.WriteMessage(mt, payload)
}

func wsURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}
