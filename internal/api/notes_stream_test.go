package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

func streamTestServer(t *testing.T) (*api.Server, *store.Store, *pgxpool.Pool, map[string]string) {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Crypto: cr})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "streamer@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "streamer@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return srv, st, pool, map[string]string{"Authorization": "Bearer " + login.Token}
}

func wsURLFromHTTP(raw string) string {
	u, _ := url.Parse(raw)
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	return u.String()
}

func TestNoteStreamUnavailableWithoutPlugin(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Live"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", noteRec.Code, noteRec.Body)
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(httpSrv.URL)+"/api/notes/"+note.ID+"/stream", http.Header{
		"Authorization": []string{hdr["Authorization"]},
	})
	if err != nil {
		t.Fatalf("dial: %v resp=%v", err, resp)
	}
	defer conn.Close()

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read unavailable: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode unavailable: %v", err)
	}
	if msg["type"] != "unavailable" {
		t.Fatalf("message = %v", msg)
	}

	got, err := st.GetNoteByID(context.Background(), note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got.Status == model.NoteFailed {
		t.Fatal("note should not fail when streaming is unavailable")
	}
}

func TestNoteStreamOwnerScopeAndMissingNoteReturn404(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	otherUser, err := st.CreateUser(context.Background(), "other@example.com", "hash")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherToken := "other-session-token"
	if err := st.CreateToken(context.Background(), otherUser.ID, "session", auth.HashToken(otherToken), "session"); err != nil {
		t.Fatalf("create other token: %v", err)
	}
	otherHdr := http.Header{"Authorization": []string{"Bearer " + otherToken}}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Owned"}, hdr)
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	cases := []struct {
		name string
		path string
		hdr  http.Header
	}{
		{name: "wrong owner", path: "/api/notes/" + note.ID + "/stream", hdr: otherHdr},
		{name: "missing note", path: "/api/notes/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/stream", hdr: http.Header{"Authorization": []string{hdr["Authorization"]}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(httpSrv.URL)+tc.path, tc.hdr)
			if err == nil {
				resp.Body.Close()
				t.Fatal("expected dial failure")
			}
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %v, want 404", resp)
			}
		})
	}
}

func TestNoteStreamRelaysAndPersistsProvisionalSegments(t *testing.T) {
	srv, st, pool, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	pluginDone := make(chan struct{})
	pluginSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":       "streaming-test",
				"version":    "1.0.0",
				"plugin_api": 1,
				"kind":       model.PluginStreamingTranscriber,
			})
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/stream":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			defer conn.Close()

			if _, payload, err := conn.ReadMessage(); err != nil {
				t.Fatalf("read start: %v", err)
			} else {
				var start map[string]any
				if err := json.Unmarshal(payload, &start); err != nil {
					t.Fatalf("decode start: %v", err)
				}
				if start["type"] != "start" || start["sample_rate"].(float64) != 16000 || start["channels"].(float64) != 1 {
					t.Fatalf("unexpected start %v", start)
				}
			}
			if err := conn.WriteJSON(map[string]string{"type": "ready"}); err != nil {
				t.Fatalf("write ready: %v", err)
			}
			if mt, payload, err := conn.ReadMessage(); err != nil {
				t.Fatalf("read audio: %v", err)
			} else if mt != websocket.BinaryMessage || !bytes.Equal(payload, []byte{1, 2, 3, 4}) {
				t.Fatalf("audio payload = %v", payload)
			}
			if err := conn.WriteJSON(map[string]any{
				"type":    "segment",
				"final":   true,
				"text":    "hello world",
				"t0":      1.25,
				"t1":      2.5,
				"speaker": nil,
			}); err != nil {
				t.Fatalf("write segment: %v", err)
			}
			if _, payload, err := conn.ReadMessage(); err != nil {
				t.Fatalf("read stop: %v", err)
			} else if string(payload) != `{"type":"stop"}` {
				t.Fatalf("stop payload = %s", payload)
			}
			close(pluginDone)
		default:
			http.NotFound(w, r)
		}
	}))
	defer pluginSrv.Close()

	pluginRec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         model.PluginStreamingTranscriber,
		"name":         "streaming-test",
		"endpoint_url": pluginSrv.URL,
		"token":        "plugin-token",
		"enabled":      true,
	}, hdr)
	if pluginRec.Code != http.StatusCreated {
		t.Fatalf("create plugin: %d %s", pluginRec.Code, pluginRec.Body)
	}
	var pluginCreated struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(pluginRec.Body.Bytes(), &pluginCreated)
	if err := st.SetDefaultPlugin(context.Background(), pluginCreated.ID); err != nil {
		t.Fatalf("set default plugin: %v", err)
	}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Streaming"}, hdr)
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(httpSrv.URL)+"/api/notes/"+note.ID+"/stream", http.Header{
		"Authorization": []string{hdr["Authorization"]},
	})
	if err != nil {
		t.Fatalf("dial: %v resp=%v", err, resp)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	var segMsg map[string]any
	if err := json.Unmarshal(payload, &segMsg); err != nil {
		t.Fatalf("decode segment: %v", err)
	}
	if segMsg["type"] != "segment" || segMsg["text"] != "hello world" {
		t.Fatalf("segment = %v", segMsg)
	}
	if segMsg["provisional"] != true {
		t.Fatalf("segment must be provisional: %v", segMsg)
	}
	if segMsg["start_ms"] != float64(1250) || segMsg["end_ms"] != float64(2500) {
		t.Fatalf("segment timings = %v", segMsg)
	}

	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = conn.Close()
	select {
	case <-pluginDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for plugin stop")
	}

	var transcriptID string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM transcripts WHERE note_id=$1`, note.ID).Scan(&transcriptID); err != nil {
		t.Fatalf("query transcript: %v", err)
	}
	var provisional bool
	if err := pool.QueryRow(context.Background(), `SELECT provisional FROM transcript_segments WHERE transcript_id=$1`, transcriptID).Scan(&provisional); err != nil {
		t.Fatalf("query provisional: %v", err)
	}
	if !provisional {
		t.Fatal("expected provisional transcript segment")
	}
	got, err := st.GetNoteByID(context.Background(), note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got.Status == model.NoteFailed {
		t.Fatal("streaming should not mark note failed")
	}
}

func TestNoteStreamHandlesPluginDropWithoutFailingNote(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	pluginSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":       "streaming-drop",
				"version":    "1.0.0",
				"plugin_api": 1,
				"kind":       model.PluginStreamingTranscriber,
			})
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/stream":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			defer conn.Close()

			if _, payload, err := conn.ReadMessage(); err != nil {
				t.Fatalf("read start: %v", err)
			} else {
				var start map[string]any
				if err := json.Unmarshal(payload, &start); err != nil {
					t.Fatalf("decode start: %v", err)
				}
				if start["type"] != "start" {
					t.Fatalf("unexpected start %v", start)
				}
			}
			if err := conn.WriteJSON(map[string]string{"type": "ready"}); err != nil {
				t.Fatalf("write ready: %v", err)
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			_ = conn.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	defer pluginSrv.Close()

	pluginRec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         model.PluginStreamingTranscriber,
		"name":         "streaming-drop",
		"endpoint_url": pluginSrv.URL,
		"token":        "plugin-token",
		"enabled":      true,
	}, hdr)
	if pluginRec.Code != http.StatusCreated {
		t.Fatalf("create plugin: %d %s", pluginRec.Code, pluginRec.Body)
	}
	var pluginCreated struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(pluginRec.Body.Bytes(), &pluginCreated)
	if err := st.SetDefaultPlugin(context.Background(), pluginCreated.ID); err != nil {
		t.Fatalf("set default plugin: %v", err)
	}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Drop"}, hdr)
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(httpSrv.URL)+"/api/notes/"+note.ID+"/stream", http.Header{
		"Authorization": []string{hdr["Authorization"]},
	})
	if err != nil {
		t.Fatalf("dial: %v resp=%v", err, resp)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{9, 8, 7, 6}); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected stream to end after plugin drop")
	}

	got, err := st.GetNote(context.Background(), "streamer-user", note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got.Status == model.NoteFailed {
		t.Fatal("plugin drop should not fail the note")
	}
}
