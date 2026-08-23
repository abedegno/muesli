package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected websocket close after unavailable message")
	} else if closeErr, ok := err.(*websocket.CloseError); !ok || closeErr.Code != websocket.CloseNormalClosure {
		t.Fatalf("close error = %v, want normal websocket closure", err)
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

	pluginSrv := newFakeStreamingPlugin(t, "plugin-token", []fakeStreamingSegment{
		{
			AfterFrames: 1,
			Text:        "hello world",
			StartMS:     1250,
			EndMS:       2500,
		},
		{
			AfterFrames: 1,
			Text:        "and again",
			StartMS:     2500,
			EndMS:       4000,
		},
	}, 0)
	pluginSrv.emitLoading = true
	pluginID := registerPlugin(t, srv, hdr, model.PluginStreamingTranscriber, "streaming-test", pluginSrv.URL(), "plugin-token")
	if err := st.SetDefaultPlugin(context.Background(), pluginID); err != nil {
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
	_, loadingPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read loading event: %v", err)
	}
	var loading map[string]any
	if err := json.Unmarshal(loadingPayload, &loading); err != nil || loading["type"] != "loading" {
		t.Fatalf("loading payload = %s, err = %v", loadingPayload, err)
	}
	_, readyPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ready event: %v", err)
	}
	var ready map[string]any
	if err := json.Unmarshal(readyPayload, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready payload = %s, err = %v", readyPayload, err)
	}

	frames := pcmFixtureAllFrames(t)
	if len(frames) != 2 {
		t.Fatalf("pcm fixture frames = %d, want 2", len(frames))
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frames[0]); err != nil {
		t.Fatalf("write audio frame 0: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first segment: %v", err)
	}
	var firstSegment map[string]any
	if err := json.Unmarshal(payload, &firstSegment); err != nil {
		t.Fatalf("decode first segment: %v", err)
	}
	if firstSegment["type"] != "segment" {
		t.Fatalf("first payload = %v", firstSegment)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frames[1]); err != nil {
		t.Fatalf("write audio frame 1: %v", err)
	}
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read second segment: %v", err)
	}
	var secondSegment map[string]any
	if err := json.Unmarshal(payload, &secondSegment); err != nil {
		t.Fatalf("decode second segment: %v", err)
	}
	segments := []map[string]any{firstSegment, secondSegment}
	if got := pluginSrv.Frames(); len(got) != len(frames) {
		t.Fatalf("plugin saw %d frames, want %d", len(got), len(frames))
	} else {
		for i := range frames {
			if !bytes.Equal(got[i], frames[i]) {
				t.Fatalf("frame %d payload mismatch", i)
			}
		}
	}
	if segments[0]["text"] != "hello world" || segments[1]["text"] != "and again" {
		t.Fatalf("segments = %v", segments)
	}
	if segments[0]["provisional"] != true || segments[1]["provisional"] != true {
		t.Fatalf("segments must be provisional: %v", segments)
	}
	if segments[0]["final"] != true || segments[1]["final"] != true {
		t.Fatalf("segments must be final on the wire: %v", segments)
	}
	if segments[0]["start_ms"] != float64(1250) || segments[0]["end_ms"] != float64(2500) {
		t.Fatalf("segment[0] timings = %v", segments[0])
	}
	if segments[1]["start_ms"] != float64(2500) || segments[1]["end_ms"] != float64(4000) {
		t.Fatalf("segment[1] timings = %v", segments[1])
	}

	// The client closes only after reading the expected segment stream.
	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatalf("write close: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected clean websocket close after client disconnect")
	}
	_ = conn.Close()

	if got := countSegments(t, pool, note.ID, true); got != 2 {
		t.Fatalf("provisional segments = %d, want 2", got)
	}
	got, err := st.GetNoteByID(context.Background(), note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got.Status == model.NoteFailed {
		t.Fatal("streaming should not mark note failed")
	}
}

func TestNoteStreamRelaysPartialsAndDropsLateDuplicates(t *testing.T) {
	srv, st, pool, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	pluginSrv := newFakeStreamingPlugin(t, "plugin-token", []fakeStreamingSegment{
		{
			AfterFrames: 1,
			Text:        "hel",
			StartMS:     1200,
			EndMS:       1500,
			Final:       boolPtr(false),
		},
		{
			AfterFrames: 1,
			Text:        "hello",
			StartMS:     1200,
			EndMS:       2100,
			Final:       boolPtr(false),
		},
		{
			AfterFrames: 1,
			Text:        "hello world",
			StartMS:     1200,
			EndMS:       2600,
			Final:       boolPtr(true),
		},
		{
			AfterFrames: 1,
			Text:        "late duplicate",
			StartMS:     1200,
			EndMS:       2700,
			Final:       boolPtr(false),
		},
	}, 0)
	pluginID := registerPlugin(t, srv, hdr, model.PluginStreamingTranscriber, "streaming-partials", pluginSrv.URL(), "plugin-token")
	if err := st.SetDefaultPlugin(context.Background(), pluginID); err != nil {
		t.Fatalf("set default plugin: %v", err)
	}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Streaming Partials"}, hdr)
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

	frames := pcmFixtureAllFrames(t)
	if len(frames) != 2 {
		t.Fatalf("pcm fixture frames = %d, want 2", len(frames))
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frames[0]); err != nil {
		t.Fatalf("write audio frame 0: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read partial 0: %v", err)
	}
	var partial0 map[string]any
	if err := json.Unmarshal(payload, &partial0); err != nil {
		t.Fatalf("decode partial 0: %v", err)
	}
	if partial0["text"] != "hel" || partial0["final"] != false {
		t.Fatalf("partial 0 payload = %v", partial0)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, frames[1]); err != nil {
		t.Fatalf("write audio frame 1: %v", err)
	}
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read partial 1: %v", err)
	}
	var partial1 map[string]any
	if err := json.Unmarshal(payload, &partial1); err != nil {
		t.Fatalf("decode partial 1: %v", err)
	}
	if partial1["text"] != "hello" || partial1["final"] != false {
		t.Fatalf("partial 1 payload = %v", partial1)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, frames[0]); err != nil {
		t.Fatalf("write audio frame 2: %v", err)
	}
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	var finalMsg map[string]any
	if err := json.Unmarshal(payload, &finalMsg); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if finalMsg["text"] != "hello world" || finalMsg["final"] != true {
		t.Fatalf("final payload = %v", finalMsg)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, frames[1]); err != nil {
		t.Fatalf("write audio frame 3: %v", err)
	}
	readErr := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage()
		readErr <- err
	}()
	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("expected late duplicate partial to be dropped")
		}
	case <-time.After(250 * time.Millisecond):
	}

	if got := countSegments(t, pool, note.ID, true); got != 1 {
		t.Fatalf("provisional segments = %d, want 1", got)
	}
	if got := countSegments(t, pool, note.ID, false); got != 0 {
		t.Fatalf("final segments = %d, want 0", got)
	}
}

func TestNoteStreamHandlesPluginDropWithoutFailingNote(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	pluginSrv := newFakeStreamingPlugin(t, "plugin-token", nil, 1)
	pluginID := registerPlugin(t, srv, hdr, model.PluginStreamingTranscriber, "streaming-drop", pluginSrv.URL(), "plugin-token")
	if err := st.SetDefaultPlugin(context.Background(), pluginID); err != nil {
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

	frames := pcmFixtureAllFrames(t)
	if len(frames) == 0 {
		t.Fatal("pcm fixture must provide at least one frame")
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frames[0]); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected stream to end after plugin drop")
	}

	got, err := st.GetNoteByID(context.Background(), note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got.Status == model.NoteFailed {
		t.Fatal("plugin drop should not fail the note")
	}
}

// TestNoteStreamWithoutPluginPreservesExistingBatchTranscript is the regression
// test for the start handshake destroying the note's existing transcript before
// the server knew whether it could transcribe anything at all.
//
// The stream endpoint used to create (and therefore seal, delete and CASCADE
// the segments of) the note's transcript before upgrading the socket and before
// looking for a streaming plugin, so this exact request — no streaming plugin
// registered — replied "unavailable" to a note it had just emptied. The loss
// could be permanent: retranscribe refuses a note whose retention_state is
// "discarded", for which the transcript is the only surviving record.
func TestNoteStreamWithoutPluginPreservesExistingBatchTranscript(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	ctx := context.Background()

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Batch"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", noteRec.Code, noteRec.Body)
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	saved, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Model:             "tiny",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 500, Text: "batch one", Source: "mic"},
			{StartMS: 500, EndMS: 900, Text: "batch two", Source: "mic"},
		},
	}, 0)
	if err != nil {
		t.Fatalf("seed batch transcript: %v", err)
	}

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
		t.Fatalf("decode control message: %v", err)
	}
	if msg["type"] != "unavailable" {
		t.Fatalf("message = %v, want type=unavailable", msg)
	}

	got, err := st.GetTranscript(ctx, note.ID)
	if err != nil {
		t.Fatalf("get transcript after stream attempt: %v", err)
	}
	if got.ID != saved.ID {
		t.Fatalf("transcript id = %q, want the original %q — the stream attempt replaced it", got.ID, saved.ID)
	}
	if got.Generation != saved.Generation {
		t.Fatalf("generation = %d, want %d (untouched)", got.Generation, saved.Generation)
	}
	if got.StreamID != nil {
		t.Fatalf("stream_id = %v, want nil (still batch-owned)", *got.StreamID)
	}
	if len(got.Segments) != 2 {
		t.Fatalf("segments = %d (%+v), want the 2 seeded batch segments", len(got.Segments), got.Segments)
	}
}

// TestNoteStreamOnBatchTranscriptIsRefusedWithReason covers the same note with
// a streaming plugin that IS available: the handshake gets as far as an open
// plugin session and is then refused by the store, because spec §7 scopes
// supersession to a stream-owned transcript. The batch transcript survives and
// the client is told why.
func TestNoteStreamOnBatchTranscriptIsRefusedWithReason(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	ctx := context.Background()

	fake := newFakeStreamingPlugin(t, "plugin-token", []fakeStreamingSegment{{AfterFrames: 1, Text: "ok", StartMS: 0, EndMS: 1}}, 0)
	pluginID := registerPlugin(t, srv, hdr, model.PluginStreamingTranscriber, "batch-refusal-test", fake.URL(), "plugin-token")
	if err := st.SetDefaultPlugin(ctx, pluginID); err != nil {
		t.Fatalf("set default plugin: %v", err)
	}
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	noteID := createStreamingNote(t, srv, hdr, "Batch owned")
	saved, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Model:             "tiny",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 500, Text: "batch one", Source: "mic"}},
	}, 0)
	if err != nil {
		t.Fatalf("seed batch transcript: %v", err)
	}

	conn := openStream(t, httpSrv.URL, noteID, strings.TrimPrefix(hdr["Authorization"], "Bearer "))
	// Bounded: a regression here leaves the stream open and healthy, so an
	// unbounded read would hang until the package test timeout instead of
	// failing.

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read control message: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode control message: %v", err)
	}
	if msg["type"] != "unavailable" || msg["reason"] != "batch_transcript_exists" {
		t.Fatalf("message = %v, want type=unavailable reason=batch_transcript_exists", msg)
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get transcript: %v", err)
	}
	if got.ID != saved.ID || len(got.Segments) != 1 {
		t.Fatalf("transcript = %+v, want the seeded batch transcript intact", got)
	}

	// The session is acquired before ownership is attempted, so refusing here
	// must also release it. Without this, dropping failStreamStart's Close would
	// leak the session and every other assertion above would still pass.
	waitForPluginSessionEnd(t, fake)
}

// TestNoteStreamWithoutPluginPreservesExistingStreamTranscript binds the
// ORDERING half of the fix on its own. The batch-transcript test above cannot:
// with supersession scoped to stream-owned transcripts, a batch transcript
// survives an early create too. Here the note's existing transcript is
// stream-owned, so the store would happily supersede it — the only thing
// standing between a note with no streaming plugin and an emptied transcript is
// that creation now happens after the plugin session opens.
func TestNoteStreamWithoutPluginPreservesExistingStreamTranscript(t *testing.T) {
	srv, st, _, hdr := streamTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	ctx := context.Background()

	noteID := createStreamingNote(t, srv, hdr, "Previous live")
	prior, err := st.CreateStreamTranscript(ctx, noteID, "stream-earlier", "streaming", "", 0)
	if err != nil {
		t.Fatalf("seed stream transcript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, prior.ID, "stream-earlier", model.Segment{
		StartMS: 0, EndMS: 500, Text: "earlier live text", Source: "mic",
	}); err != nil {
		t.Fatalf("seed live segment: %v", err)
	}

	conn := openStream(t, httpSrv.URL, noteID, strings.TrimPrefix(hdr["Authorization"], "Bearer "))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read unavailable: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode control message: %v", err)
	}
	if msg["type"] != "unavailable" {
		t.Fatalf("message = %v, want type=unavailable", msg)
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get transcript after stream attempt: %v", err)
	}
	if got.ID != prior.ID {
		t.Fatalf("transcript id = %q, want the prior stream transcript %q — a failed start superseded it", got.ID, prior.ID)
	}
	if got.Generation != prior.Generation {
		t.Fatalf("generation = %d, want %d (untouched)", got.Generation, prior.Generation)
	}
	if len(got.Segments) != 1 {
		t.Fatalf("segments = %d (%+v), want the 1 seeded live segment", len(got.Segments), got.Segments)
	}
}
