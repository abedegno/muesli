package api_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/model"
	"github.com/gorilla/websocket"
)

func TestStreamHandlerIgnoresTextFrames(t *testing.T) {
	fixture := newStreamHandlerFixture(t)
	conn := fixture.open(t)

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"not":"audio"}`)); err != nil {
		t.Fatalf("write text frame: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("audio")); err != nil {
		t.Fatalf("write binary frame: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read segment proving stream remained active: %v", err)
	}

	frames := fixture.plugin.Frames()
	if len(frames) != 1 || string(frames[0]) != "audio" {
		t.Fatalf("plugin frames = %q, want only binary audio frame", frames)
	}
	writeClientClose(t, conn, websocket.CloseNormalClosure, nil)
	if stopped := waitForPluginSessionEnd(t, fixture.plugin); !stopped {
		t.Fatal("plugin session ended without graceful stop")
	}
}

func TestStreamHandlerClientClosePaths(t *testing.T) {
	tests := []struct {
		name        string
		closeClient func(*testing.T, *websocket.Conn)
		wantStopped bool
	}{
		{
			name: "normal closure",
			closeClient: func(t *testing.T, conn *websocket.Conn) {
				writeClientClose(t, conn, websocket.CloseNormalClosure, nil)
			},
			wantStopped: true,
		},
		{
			name: "going away",
			closeClient: func(t *testing.T, conn *websocket.Conn) {
				writeClientClose(t, conn, websocket.CloseGoingAway, nil)
			},
			wantStopped: true,
		},
		{
			name: "no status received",
			closeClient: func(t *testing.T, conn *websocket.Conn) {
				writeClientClose(t, conn, websocket.CloseNoStatusReceived, []byte{})
			},
			wantStopped: true,
		},
		{
			name: "unexpected close",
			closeClient: func(t *testing.T, conn *websocket.Conn) {
				writeClientClose(t, conn, 4000, nil)
			},
			wantStopped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newStreamHandlerFixture(t)
			conn := fixture.open(t)
			tt.closeClient(t, conn)
			if stopped := waitForPluginSessionEnd(t, fixture.plugin); stopped != tt.wantStopped {
				t.Fatalf("plugin received stop = %v, want %v", stopped, tt.wantStopped)
			}
		})
	}
}

type streamHandlerFixture struct {
	srvURL string
	srv    *httptest.Server
	api    *api.Server
	hdr    map[string]string
	plugin *fakeStreamingPlugin
}

func newStreamHandlerFixture(t *testing.T) *streamHandlerFixture {
	t.Helper()
	srv, st, _, hdr := streamTestServer(t)
	plugin := newFakeStreamingPlugin(t, "plugin-token", []fakeStreamingSegment{{AfterFrames: 1, Text: "ok", StartMS: 0, EndMS: 1}}, 0)
	pluginID := registerPlugin(t, srv, hdr, model.PluginStreamingTranscriber, "stream-handler-test", plugin.URL(), "plugin-token")
	if err := st.SetDefaultPlugin(context.Background(), pluginID); err != nil {
		t.Fatalf("set default plugin: %v", err)
	}
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return &streamHandlerFixture{srvURL: httpSrv.URL, srv: httpSrv, api: srv, hdr: hdr, plugin: plugin}
}

func (f *streamHandlerFixture) open(t *testing.T) *websocket.Conn {
	t.Helper()
	// Creating a fresh note per connection also prevents session state from one
	// close-code case influencing another.
	noteID := createStreamingNote(t, f.api, f.hdr, "Stream close path")
	token := strings.TrimPrefix(f.hdr["Authorization"], "Bearer ")
	return openStream(t, f.srvURL, noteID, token)
}

func writeClientClose(t *testing.T, conn *websocket.Conn, code int, payload []byte) {
	t.Helper()
	if payload == nil {
		payload = websocket.FormatCloseMessage(code, "")
	}
	if err := conn.WriteControl(websocket.CloseMessage, payload, time.Time{}); err != nil {
		t.Fatalf("write close code %d: %v", code, err)
	}
}

func waitForPluginSessionEnd(t *testing.T, plugin *fakeStreamingPlugin) bool {
	t.Helper()
	select {
	case stopped := <-plugin.SessionEnded():
		return stopped
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for plugin session to end")
		return false
	}
}
