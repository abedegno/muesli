package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/gorilla/websocket"
)

func TestStreamSecondsToMS(t *testing.T) {
	tests := []struct {
		name    string
		seconds float64
		want    int
	}{
		{name: "zero", seconds: 0, want: 0},
		{name: "exact", seconds: 1.25, want: 1250},
		{name: "positive half rounds away from zero", seconds: 0.0015, want: 2},
		{name: "negative half rounds away from zero", seconds: -0.0015, want: -2},
		{name: "negative exact", seconds: -1.25, want: -1250},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secondsToMS(tt.seconds); got != tt.want {
				t.Fatalf("secondsToMS(%v) = %d, want %d", tt.seconds, got, tt.want)
			}
		})
	}
}

func TestStreamEnqueueAudioFrameDropsOldest(t *testing.T) {
	ch := make(chan streamAudioFrame, 2)
	oldest := []byte("oldest")
	survivor := []byte("survivor")
	ch <- streamAudioFrame{data: oldest}
	ch <- streamAudioFrame{data: survivor}

	frame := []byte("new")
	dropped, err := enqueueAudioFrame(context.Background(), ch, streamAudioFrame{data: frame})
	if err != nil {
		t.Fatalf("enqueueAudioFrame() error = %v", err)
	}
	if dropped == nil || string(dropped.data) != "oldest" {
		t.Fatalf("dropped frame = %#v, want oldest", dropped)
	}

	if got := string((<-ch).data); got != "survivor" {
		t.Fatalf("first queued frame = %q, want survivor", got)
	}
	queued := <-ch
	if got := string(queued.data); got != "new" {
		t.Fatalf("second queued frame = %q, want new", got)
	}

	// enqueueAudioFrame itself intentionally transfers the supplied slice without
	// copying it. handleNoteStream makes the defensive copy at its call site.
	frame[0] = 'N'
	if got := string(queued.data); got != "New" {
		t.Fatalf("queued frame = %q after caller mutation, want alias New", got)
	}
}

func TestStreamEnqueueAudioFrameReturnsCancelledContextWhenSendCannotProceed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := enqueueAudioFrame(ctx, make(chan streamAudioFrame), streamAudioFrame{data: []byte("frame")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("enqueueAudioFrame() error = %v, want context.Canceled", err)
	}
}

func TestStreamDropTrackerCountsContiguousFramesAndSamples(t *testing.T) {
	var tracker streamDropTracker
	if got := tracker.observe(&streamAudioFrame{data: make([]byte, 640), startSample: 320}); got != nil {
		t.Fatalf("first drop resolved run = %#v, want nil", got)
	}
	if got := tracker.observe(&streamAudioFrame{data: make([]byte, 320), startSample: 640}); got != nil {
		t.Fatalf("second drop resolved run = %#v, want nil", got)
	}
	got := tracker.observe(nil)
	if got == nil || got.startSample != 320 || got.droppedSamples != 480 {
		t.Fatalf("resolved run = %#v, want start=320 dropped=480", got)
	}
	if got := tracker.flush(); got != nil {
		t.Fatalf("second resolve = %#v, want nil", got)
	}
}

func TestStreamGapStateMarksOnlyNextSegment(t *testing.T) {
	state := &streamGapState{}
	state.markGap()
	first := model.Segment{}
	state.applyToNextSegment(&first)
	second := model.Segment{}
	state.applyToNextSegment(&second)
	if first.Boundary != "gap" || second.Boundary != "" {
		t.Fatalf("boundaries = %q, %q; want gap then empty", first.Boundary, second.Boundary)
	}
}

func TestStreamGapControlEmitsJSON(t *testing.T) {
	serverConn, clientConn := streamWebsocketPair(t)
	written := make(chan error, 1)
	go func() {
		written <- writeStreamControl(serverConn, streamSegmentMessage{Type: "gap", StreamID: "stream-1", NoteID: "note-1", DroppedMS: 40, Final: true})
	}()

	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got["type"] != "gap" || got["stream_id"] != "stream-1" || got["note_id"] != "note-1" || got["dropped_duration_ms"] != float64(40) {
		t.Fatalf("gap control message = %#v", got)
	}
	if err := <-written; err != nil {
		t.Fatalf("writeStreamControl() error = %v", err)
	}
}

func TestStreamWriteStreamControlEmitsJSON(t *testing.T) {
	serverConn, clientConn := streamWebsocketPair(t)
	written := make(chan error, 1)
	go func() {
		written <- writeStreamControl(serverConn, map[string]any{"type": "segment", "final": true})
	}()

	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	var got struct {
		Type  string `json:"type"`
		Final bool   `json:"final"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; payload = %q", err, payload)
	}
	if got.Type != "segment" || !got.Final {
		t.Fatalf("control message = %+v, want segment with final=true", got)
	}
	if err := <-written; err != nil {
		t.Fatalf("writeStreamControl() error = %v", err)
	}
}

func TestStreamCloseWebsocketCleanlySendsNormalClose(t *testing.T) {
	serverConn, clientConn := streamWebsocketPair(t)
	closed := make(chan error, 1)
	go func() {
		closed <- closeWebsocketCleanly(serverConn)
	}()

	_, _, err := clientConn.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("ReadMessage() error = %v, want websocket close error", err)
	}
	if closeErr.Code != websocket.CloseNormalClosure {
		t.Fatalf("close code = %d, want %d", closeErr.Code, websocket.CloseNormalClosure)
	}
	if err := <-closed; err != nil {
		t.Fatalf("closeWebsocketCleanly() error = %v", err)
	}
}

func streamWebsocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := streamUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error = %v", err)
			return
		}
		serverConnCh <- conn
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "ws"
	clientConn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	select {
	case serverConn := <-serverConnCh:
		return serverConn, clientConn
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server websocket")
		return nil, nil
	}
}
