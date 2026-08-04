package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/gorilla/websocket"
)

type fakeStreamingSegment struct {
	AfterFrames int
	Text        string
	StartMS     int
	EndMS       int
	Speaker     *string
	Final       *bool
}

type fakeStreamingPlugin struct {
	srv             *httptest.Server
	token           string
	segments        []fakeStreamingSegment
	dropAfterFrames int

	mu              sync.Mutex
	binaryFrames    int
	frames          [][]byte
	emittedSegments []fakeStreamingSegment
	sessionEnded    chan bool
}

func newFakeStreamingPlugin(t *testing.T, token string, segments []fakeStreamingSegment, dropAfterFrames int) *fakeStreamingPlugin {
	t.Helper()
	p := &fakeStreamingPlugin{
		token:           token,
		segments:        append([]fakeStreamingSegment(nil), segments...),
		dropAfterFrames: dropAfterFrames,
		sessionEnded:    make(chan bool, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/info", p.handleInfo)
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/stream", p.handleStream)
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

func (p *fakeStreamingPlugin) URL() string {
	return p.srv.URL
}

func (p *fakeStreamingPlugin) Close() {
	if p.srv != nil {
		p.srv.Close()
	}
}

func (p *fakeStreamingPlugin) BinaryFrames() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.binaryFrames
}

func (p *fakeStreamingPlugin) Frames() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.frames))
	for i, frame := range p.frames {
		out[i] = append([]byte(nil), frame...)
	}
	return out
}

func (p *fakeStreamingPlugin) EmittedSegments() []fakeStreamingSegment {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]fakeStreamingSegment, len(p.emittedSegments))
	copy(out, p.emittedSegments)
	return out
}

func (p *fakeStreamingPlugin) SessionEnded() <-chan bool {
	return p.sessionEnded
}

func (p *fakeStreamingPlugin) handleInfo(w http.ResponseWriter, r *http.Request) {
	if !p.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(plugin.Info{Name: "fake-streaming", Version: "0", PluginAPI: 1, Kind: model.PluginStreamingTranscriber})
}

func (p *fakeStreamingPlugin) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (p *fakeStreamingPlugin) handleStream(w http.ResponseWriter, r *http.Request) {
	if !p.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	stopped := false
	defer func() { p.sessionEnded <- stopped }()

	if _, payload, err := conn.ReadMessage(); err != nil {
		return
	} else {
		var start struct {
			Type       string `json:"type"`
			SampleRate int    `json:"sample_rate"`
			Channels   int    `json:"channels"`
		}
		if err := json.Unmarshal(payload, &start); err != nil {
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": "invalid start message"})
			return
		}
		if start.Type != "start" || start.SampleRate != 16000 || start.Channels != 1 {
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": "invalid start parameters"})
			return
		}
	}

	if err := conn.WriteJSON(map[string]string{"type": "ready"}); err != nil {
		return
	}

	framesSinceEmit := 0
	nextSegment := 0
	for {
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			p.mu.Lock()
			p.binaryFrames++
			p.frames = append(p.frames, append([]byte(nil), payload...))
			binaryFrames := p.binaryFrames
			p.mu.Unlock()
			if p.dropAfterFrames > 0 && binaryFrames >= p.dropAfterFrames {
				return
			}
			framesSinceEmit++
			if nextSegment < len(p.segments) {
				seg := p.segments[nextSegment]
				if seg.AfterFrames > 0 && framesSinceEmit >= seg.AfterFrames {
					if err := p.emitSegment(conn, seg); err != nil {
						return
					}
					p.mu.Lock()
					p.emittedSegments = append(p.emittedSegments, seg)
					p.mu.Unlock()
					nextSegment++
					framesSinceEmit = 0
				}
			}
		case websocket.TextMessage:
			var control struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(payload, &control); err != nil {
				continue
			}
			if control.Type != "stop" {
				continue
			}
			stopped = true
			if nextSegment < len(p.segments) && framesSinceEmit > 0 {
				if err := p.emitSegment(conn, p.segments[nextSegment]); err != nil {
					return
				}
				p.mu.Lock()
				p.emittedSegments = append(p.emittedSegments, p.segments[nextSegment])
				p.mu.Unlock()
				nextSegment++
			}
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Time{})
			return
		}
	}
}

func (p *fakeStreamingPlugin) emitSegment(conn *websocket.Conn, seg fakeStreamingSegment) error {
	final := true
	if seg.Final != nil {
		final = *seg.Final
	}
	return conn.WriteJSON(plugin.StreamingEvent{
		Type:    "segment",
		Final:   final,
		Text:    seg.Text,
		T0:      float64(seg.StartMS) / 1000,
		T1:      float64(seg.EndMS) / 1000,
		Speaker: seg.Speaker,
	})
}

func boolPtr(v bool) *bool {
	return &v
}

func (p *fakeStreamingPlugin) checkAuth(r *http.Request) bool {
	if p.token == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+p.token && r.Header.Get("X-Muesli-Plugin-API") == "1"
}
