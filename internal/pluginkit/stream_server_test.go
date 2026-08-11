package pluginkit

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/plugin"
	"go.uber.org/goleak"
	"net/http/httptest"
)

type wireTestEngine struct {
	mu       sync.Mutex
	sessions []*wireTestSession
	started  chan *wireTestSession
}

type loadingWireEngine struct {
	attempts int
	ready    *wireTestEngine
}

func (e *loadingWireEngine) Transcribe(context.Context, []float32, TranscribeRequest) (TranscribeResult, error) {
	return TranscribeResult{}, nil
}

func (e *loadingWireEngine) StartStream(ctx context.Context, req StreamingStartRequest) (StreamingEngineSession, error) {
	e.attempts++
	if e.attempts < 3 {
		return nil, ErrStreamingModelLoading
	}
	return e.ready.StartStream(ctx, req)
}

func TestStreamingLoadingKeepsSessionOpenUntilReady(t *testing.T) {
	engine := &loadingWireEngine{ready: newWireTestEngine()}
	server := httptest.NewServer(TranscriberHandler(Config{Token: "secret"}, engine))
	defer server.Close()

	var events []plugin.StreamingEvent
	session, err := plugin.NewStreaming(server.URL, "secret").Open(context.Background(), wireStart("warm"), func(event plugin.StreamingEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if len(events) != 1 || events[0].Type != "loading" {
		t.Fatalf("loading events = %#v, want one loading event", events)
	}
	if engine.attempts != 3 {
		t.Fatalf("StartStream attempts = %d, want 3", engine.attempts)
	}
}

func newWireTestEngine() *wireTestEngine {
	return &wireTestEngine{started: make(chan *wireTestSession, 8)}
}

func (e *wireTestEngine) Transcribe(context.Context, []float32, TranscribeRequest) (TranscribeResult, error) {
	return TranscribeResult{}, nil
}

func (e *wireTestEngine) StartStream(_ context.Context, req StreamingStartRequest) (StreamingEngineSession, error) {
	var options struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(req.Options, &options)
	s := &wireTestSession{id: options.ID, audio: make(chan []float32, 1), closed: make(chan struct{})}
	e.mu.Lock()
	e.sessions = append(e.sessions, s)
	e.mu.Unlock()
	e.started <- s
	return s, nil
}

type wireTestSession struct {
	id        string
	audio     chan []float32
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *wireTestSession) WriteAudio(_ context.Context, pcm []float32) ([]StreamingEvent, error) {
	s.audio <- append([]float32(nil), pcm...)
	return []StreamingEvent{
		{Type: "segment", Text: s.id + "-partial", Final: false, T0: 1.25, T1: 2.5},
		{Type: "segment", Text: s.id + "-final", Final: true, T0: 1.25, T1: 3.5},
	}, nil
}

func (s *wireTestSession) Close(context.Context) error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func TestStreamingWireProtocolWithRealClient(t *testing.T) {
	engine := newWireTestEngine()
	server := httptest.NewServer(TranscriberHandler(Config{Token: "secret"}, engine))
	defer server.Close()

	session := openWireSession(t, server.URL, "secret", "one")
	defer session.Close()
	engineSession := <-engine.started
	frame := make([]byte, 6)
	negativeFullScale := int16(-32768)
	binary.LittleEndian.PutUint16(frame[0:], uint16(negativeFullScale))
	binary.LittleEndian.PutUint16(frame[2:], 0)
	binary.LittleEndian.PutUint16(frame[4:], uint16(int16(16384)))
	if err := session.WriteAudio(frame); err != nil {
		t.Fatal(err)
	}
	pcm := <-engineSession.audio
	if len(pcm) != 3 || pcm[0] != -1 || pcm[1] != 0 || pcm[2] != .5 {
		t.Fatalf("unexpected decoded PCM: %#v", pcm)
	}
	partial := recvWireEvent(t, session)
	final := recvWireEvent(t, session)
	if partial.Final || partial.Text != "one-partial" || partial.T0 != 1.25 || partial.T1 != 2.5 {
		t.Fatalf("partial event did not round trip: %#v", partial)
	}
	if !final.Final || final.Text != "one-final" || final.T1 != 3.5 {
		t.Fatalf("final event did not round trip: %#v", final)
	}
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engineSession.closed:
	case <-time.After(time.Second):
		t.Fatal("engine session was not closed after stop")
	}
}

func TestStreamingDisconnectDoesNotLeakGoroutines(t *testing.T) {
	// Snapshot the goroutines already running (the test binary's own, plus
	// anything a previous test left parked) so this only reports goroutines
	// this test's server created.
	ignoreExisting := goleak.IgnoreCurrent()
	engine := newWireTestEngine()
	server := httptest.NewServer(TranscriberHandler(Config{Token: "secret"}, engine))
	session := openWireSession(t, server.URL, "secret", "disconnect")
	engineSession := <-engine.started
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engineSession.closed:
	case <-time.After(time.Second):
		t.Fatal("engine session was not closed after disconnect")
	}
	// Blocks until every handler goroutine has returned.
	server.Close()
	// Names the leaked stack rather than reporting a count that drifted.
	goleak.VerifyNone(t, ignoreExisting)
}

func TestStreamingSessionCapRejectsOnlyExcessSession(t *testing.T) {
	engine := newWireTestEngine()
	server := httptest.NewServer(TranscriberHandler(Config{Token: "secret", MaxStreamingSessions: 1}, engine))
	defer server.Close()

	first := openWireSession(t, server.URL, "secret", "first")
	defer first.Close()
	<-engine.started
	_, err := plugin.NewStreaming(server.URL, "secret").Open(context.Background(), wireStart("excess"))
	if err == nil || !strings.Contains(err.Error(), "session limit reached") {
		t.Fatalf("expected usable cap error, got %v", err)
	}
	if err := first.WriteAudio([]byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	if event := recvWireEvent(t, first); event.Text != "first-partial" {
		t.Fatalf("accepted session was affected: %#v", event)
	}
}

func TestStreamingConcurrentSessionsDoNotCrossTalk(t *testing.T) {
	engine := newWireTestEngine()
	server := httptest.NewServer(TranscriberHandler(Config{Token: "secret", MaxStreamingSessions: 2}, engine))
	defer server.Close()

	var sessions [2]*plugin.StreamingSession
	var wg sync.WaitGroup
	for i, id := range []string{"alpha", "beta"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sessions[i] = openWireSession(t, server.URL, "secret", id)
		}(i, id)
	}
	wg.Wait()
	defer sessions[0].Close()
	defer sessions[1].Close()
	for _, session := range sessions {
		if err := session.WriteAudio([]byte{0, 0}); err != nil {
			t.Fatal(err)
		}
	}
	if got := recvWireEvent(t, sessions[0]).Text; got != "alpha-partial" {
		t.Fatalf("alpha received %q", got)
	}
	if got := recvWireEvent(t, sessions[1]).Text; got != "beta-partial" {
		t.Fatalf("beta received %q", got)
	}
}

func openWireSession(t *testing.T, url, token, id string) *plugin.StreamingSession {
	t.Helper()
	session, err := plugin.NewStreaming(url, token).Open(context.Background(), wireStart(id))
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func wireStart(id string) plugin.StreamingStartRequest {
	return plugin.StreamingStartRequest{Type: "start", Options: json.RawMessage(`{"id":"` + id + `"}`), SampleRate: 16000, Channels: 1}
}

func recvWireEvent(t *testing.T, session *plugin.StreamingSession) plugin.StreamingEvent {
	t.Helper()
	event, err := session.Recv()
	if err != nil {
		t.Fatal(err)
	}
	return event
}
