package plugin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
)

type streamHandshake struct {
	authorization string
	apiVersion    string
	startType     int
	start         []byte
	tls           bool
	err           error
}

func TestStreamingOpenHandshakeAndStartFrame(t *testing.T) {
	tests := []struct {
		name         string
		tls          bool
		req          StreamingStartRequest
		wantLanguage bool
		wantOptions  string
		wantConfig   string
	}{
		{
			name: "http base dials ws and normalizes empty objects",
			req: StreamingStartRequest{
				SampleRate: 16000,
				Channels:   1,
			},
			wantOptions: `{}`,
			wantConfig:  `{}`,
		},
		{
			name: "https base dials wss and preserves supplied fields",
			tls:  true,
			req: StreamingStartRequest{
				LanguageHint: "es",
				Options:      json.RawMessage(`{"foo":1}`),
				Config:       json.RawMessage(`{"model":"tiny"}`),
				SampleRate:   48000,
				Channels:     2,
			},
			wantLanguage: true,
			wantOptions:  `{"foo":1}`,
			wantConfig:   `{"model":"tiny"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(chan streamHandshake, 1)
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := streamHandshake{
					authorization: r.Header.Get("Authorization"),
					apiVersion:    r.Header.Get("X-Muesli-Plugin-API"),
					tls:           r.TLS != nil,
				}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					result.err = err
					got <- result
					return
				}
				defer conn.Close()
				result.startType, result.start, result.err = conn.ReadMessage()
				got <- result
				if result.err == nil {
					_ = conn.WriteJSON(StreamingEvent{Type: "ready"})
				}
			})

			var srv *httptest.Server
			if tt.tls {
				srv = httptest.NewTLSServer(handler)
			} else {
				srv = httptest.NewServer(handler)
			}
			defer srv.Close()

			client := NewStreaming(srv.URL+"/", "tok-123")
			if tt.tls {
				client.dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test server certificate
			}
			sess, err := client.Open(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer sess.Close()

			handshake := <-got
			if handshake.err != nil {
				t.Fatalf("server handshake: %v", handshake.err)
			}
			if handshake.authorization != "Bearer tok-123" {
				t.Errorf("Authorization = %q, want Bearer token envelope", handshake.authorization)
			}
			if handshake.apiVersion != PluginAPIVersion {
				t.Errorf("X-Muesli-Plugin-API = %q, want %q", handshake.apiVersion, PluginAPIVersion)
			}
			if handshake.tls != tt.tls {
				t.Errorf("TLS handshake = %v, want %v (base scheme %s)", handshake.tls, tt.tls, srv.URL)
			}
			if handshake.startType != websocket.TextMessage {
				t.Errorf("start message type = %d, want text", handshake.startType)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(handshake.start, &raw); err != nil {
				t.Fatalf("decode start frame %q: %v", handshake.start, err)
			}
			assertJSONValue(t, raw, "type", `"start"`)
			assertJSONValue(t, raw, "sample_rate", jsonNumber(tt.req.SampleRate))
			assertJSONValue(t, raw, "channels", jsonNumber(tt.req.Channels))
			assertJSONValue(t, raw, "options", tt.wantOptions)
			assertJSONValue(t, raw, "config", tt.wantConfig)
			language, present := raw["language_hint"]
			if present != tt.wantLanguage {
				t.Fatalf("language_hint present = %v, want %v; frame=%s", present, tt.wantLanguage, handshake.start)
			}
			if tt.wantLanguage && string(language) != `"`+tt.req.LanguageHint+`"` {
				t.Errorf("language_hint = %s, want %q", language, tt.req.LanguageHint)
			}
		})
	}
}

func TestStreamingOpenDialFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "handshake rejected", status: http.StatusUnauthorized},
		{name: "endpoint missing", status: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "no websocket", tt.status)
			}))
			defer srv.Close()

			sess, err := NewStreaming(srv.URL, "tok").Open(context.Background(), StreamingStartRequest{})
			if err == nil || sess != nil {
				t.Fatalf("Open = (%v, %v), want nil session and handshake error", sess, err)
			}
		})
	}
}

func TestStreamingWriteAudioAndStop(t *testing.T) {
	type received struct {
		types    []int
		payloads [][]byte
		err      error
	}
	got := make(chan received, 1)
	srv := newStreamServer(t, func(conn *websocket.Conn) {
		var result received
		for range 2 {
			mt, payload, err := conn.ReadMessage()
			if err != nil {
				result.err = err
				break
			}
			result.types = append(result.types, mt)
			result.payloads = append(result.payloads, payload)
		}
		got <- result
	})
	defer srv.Close()

	sess, err := NewStreaming(srv.URL, "tok").Open(context.Background(), StreamingStartRequest{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sess.Close()
	if err := sess.WriteAudio(nil); err != nil {
		t.Fatalf("WriteAudio(empty): %v", err)
	}
	if err := sess.WriteAudio([]byte{0, 1, 2}); err != nil {
		t.Fatalf("WriteAudio: %v", err)
	}
	if err := sess.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	result := <-got
	if result.err != nil {
		t.Fatalf("server read: %v", result.err)
	}
	if len(result.types) != 2 {
		t.Fatalf("received %d frames, want audio and stop only (zero-length audio sends none)", len(result.types))
	}
	if result.types[0] != websocket.BinaryMessage || string(result.payloads[0]) != string([]byte{0, 1, 2}) {
		t.Errorf("audio frame = type %d payload %v, want binary [0 1 2]", result.types[0], result.payloads[0])
	}
	if result.types[1] != websocket.TextMessage || string(result.payloads[1]) != `{"type":"stop"}` {
		t.Errorf("stop frame = type %d payload %s", result.types[1], result.payloads[1])
	}
}

func TestStreamingRecvEvents(t *testing.T) {
	speaker := "Ada"
	tests := []struct {
		name      string
		payload   string
		want      StreamingEvent
		wantError bool
	}{
		{name: "partial without speaker", payload: `{"type":"segment","final":false,"text":"hel"}`, want: StreamingEvent{Type: "segment", Text: "hel"}},
		{name: "final with speaker and times", payload: `{"type":"segment","final":true,"text":"hello","t0":1.25,"t1":2.5,"speaker":"Ada"}`, want: StreamingEvent{Type: "segment", Final: true, Text: "hello", T0: 1.25, T1: 2.5, Speaker: &speaker}},
		{name: "error event", payload: `{"type":"error","message":"decoder failed"}`, want: StreamingEvent{Type: "error", Message: "decoder failed"}},
		{name: "malformed JSON", payload: `{"type":`, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newStreamServer(t, func(conn *websocket.Conn) {
				_ = conn.WriteMessage(websocket.TextMessage, []byte(tt.payload))
			})
			defer srv.Close()
			sess, err := NewStreaming(srv.URL, "tok").Open(context.Background(), StreamingStartRequest{})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer sess.Close()

			event, err := sess.Recv()
			if tt.wantError {
				if err == nil {
					t.Fatalf("Recv = %+v, want malformed JSON error", event)
				}
				return
			}
			if err != nil {
				t.Fatalf("Recv: %v", err)
			}
			if event.Type != tt.want.Type || event.Final != tt.want.Final || event.Text != tt.want.Text || event.T0 != tt.want.T0 || event.T1 != tt.want.T1 || event.Message != tt.want.Message {
				t.Errorf("event = %+v, want %+v", event, tt.want)
			}
			if tt.want.Speaker == nil {
				if event.Speaker != nil {
					t.Errorf("Speaker = %q, want nil", *event.Speaker)
				}
			} else if event.Speaker == nil || *event.Speaker != *tt.want.Speaker {
				t.Errorf("Speaker = %v, want %q", event.Speaker, *tt.want.Speaker)
			}
		})
	}
}

func TestStreamingLifecycleCloseErrors(t *testing.T) {
	tests := []struct {
		name      string
		serverEnd func(*websocket.Conn)
		wantClean bool
	}{
		{
			name: "clean server close is distinguishable",
			serverEnd: func(conn *websocket.Conn) {
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))
			},
			wantClean: true,
		},
		{
			name:      "mid-stream connection loss",
			serverEnd: func(conn *websocket.Conn) { _ = conn.Close() },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newStreamServer(t, tt.serverEnd)
			defer srv.Close()
			sess, err := NewStreaming(srv.URL, "tok").Open(context.Background(), StreamingStartRequest{})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			_, err = sess.Recv()
			if err == nil {
				t.Fatal("Recv after server close returned nil error")
			}
			clean := websocket.IsCloseError(err, websocket.CloseNormalClosure)
			if clean != tt.wantClean {
				t.Errorf("clean-close classification = %v, want %v; error=%v", clean, tt.wantClean, err)
			}
		})
	}
}

func TestStreamingCloseReleasesConnection(t *testing.T) {
	serverRead := make(chan error, 1)
	srv := newStreamServer(t, func(conn *websocket.Conn) {
		_, _, err := conn.ReadMessage()
		serverRead <- err
	})
	defer srv.Close()

	sess, err := NewStreaming(srv.URL, "tok").Open(context.Background(), StreamingStartRequest{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-serverRead; err == nil {
		t.Fatal("server read succeeded after client Close, want closed connection")
	}
}

func newStreamServer(t *testing.T, afterReady func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read start: %v", err)
			return
		}
		if err := conn.WriteJSON(StreamingEvent{Type: "ready"}); err != nil {
			t.Errorf("write ready: %v", err)
			return
		}
		afterReady(conn)
	}))
}

func assertJSONValue(t *testing.T, raw map[string]json.RawMessage, key, want string) {
	t.Helper()
	got, ok := raw[key]
	if !ok {
		t.Fatalf("%s absent from start frame", key)
	}
	if string(got) != want {
		t.Errorf("%s = %s, want %s", key, got, want)
	}
}

func jsonNumber(value int) string {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(b)
}
