package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

type streamingE2EFixture struct {
	srv  *api.Server
	st   *store.Store
	pool *pgxpool.Pool
	prov storage.Provider
	cr   *crypto.Crypto
	hdr  map[string]string
}

func newStreamingE2EFixture(t *testing.T) *streamingE2EFixture {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr})

	if err := st.SeedBuiltInTemplates(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "streamer@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "streamer@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)

	return &streamingE2EFixture{
		srv:  srv,
		st:   st,
		pool: pool,
		prov: prov,
		cr:   cr,
		hdr:  map[string]string{"Authorization": "Bearer " + login.Token},
	}
}

func registerPlugin(t *testing.T, srv *api.Server, hdr map[string]string, kind, name, endpointURL, token string) string {
	t.Helper()
	rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]any{
		"kind":         kind,
		"name":         name,
		"endpoint_url": endpointURL,
		"token":        token,
		"enabled":      true,
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plugin: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("plugin id missing")
	}
	return created.ID
}

func claimAndProcessJobs(t *testing.T, proc *worker.Processor, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("claim job: %v", err)
		}
		if !ok {
			return
		}
		proc.Process(ctx, job)
	}
	t.Fatal("job drain did not terminate")
}

func countSegments(t *testing.T, pool *pgxpool.Pool, noteID string, provisional bool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*)
		   FROM transcript_segments ts
		   JOIN transcripts t ON t.id = ts.transcript_id
		  WHERE t.note_id = $1 AND ts.provisional = $2`,
		noteID, provisional,
	).Scan(&count); err != nil {
		t.Fatalf("count segments: %v", err)
	}
	return count
}

func openStream(t *testing.T, httpURL, noteID, token string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(httpURL)+"/api/notes/"+noteID+"/stream", http.Header{
		"Authorization": []string{"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("dial: %v resp=%v", err, resp)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func createStreamingNote(t *testing.T, srv *api.Server, hdr map[string]string, title string) string {
	t.Helper()
	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": title}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", rec.Code, rec.Body)
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatal("note id missing")
	}
	return note.ID
}

func setNoteAudioAndJob(t *testing.T, st *store.Store, noteID string) {
	t.Helper()
	ctx := context.Background()
	note, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	audioKey := "notes/" + noteID + "/audio/live.webm"
	if err := st.SetNoteAudio(ctx, note.OwnerID, noteID, audioKey); err != nil {
		t.Fatalf("set note audio: %v", err)
	}
	if _, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+audioKey+`"}`)); err != nil {
		t.Fatalf("enqueue transcribe job: %v", err)
	}
}

func TestStreamingE2E_LiveSegmentsAndBatchFinalize(t *testing.T) {
	fixture := newStreamingE2EFixture(t)
	httpSrv := httptest.NewServer(fixture.srv.Handler())
	t.Cleanup(httpSrv.Close)

	streamPlugin := newFakeStreamingPlugin(t, "stream-token", []fakeStreamingSegment{
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
	pluginID := registerPlugin(t, fixture.srv, fixture.hdr, model.PluginStreamingTranscriber, "streaming-fake", streamPlugin.URL(), "stream-token")
	if err := fixture.st.SetDefaultPlugin(context.Background(), pluginID); err != nil {
		t.Fatalf("set default streaming plugin: %v", err)
	}

	noteID := createStreamingNote(t, fixture.srv, fixture.hdr, "Live")
	setNoteAudioAndJob(t, fixture.st, noteID)

	conn := openStream(t, httpSrv.URL, noteID, strings.TrimPrefix(fixture.hdr["Authorization"], "Bearer "))
	frames := pcmFixtureAllFrames(t)
	if len(frames) != 2 {
		t.Fatalf("pcm fixture frames = %d, want 2", len(frames))
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frames[0]); err != nil {
		t.Fatalf("write pcm frame 0: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first stream message: %v", err)
	}
	var firstSegment map[string]any
	if err := json.Unmarshal(payload, &firstSegment); err != nil {
		t.Fatalf("decode first segment: %v", err)
	}
	if firstSegment["type"] != "segment" {
		t.Fatalf("first payload = %v", firstSegment)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frames[1]); err != nil {
		t.Fatalf("write pcm frame 1: %v", err)
	}
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read second stream message: %v", err)
	}
	var secondSegment map[string]any
	if err := json.Unmarshal(payload, &secondSegment); err != nil {
		t.Fatalf("decode second segment: %v", err)
	}
	segments := []map[string]any{firstSegment, secondSegment}
	if segments[0]["provisional"] != true || segments[1]["provisional"] != true {
		t.Fatalf("segments must be provisional: %v", segments)
	}
	if segments[0]["text"] != "hello world" || segments[1]["text"] != "and again" {
		t.Fatalf("segment texts = %v", segments)
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

	if got := streamPlugin.Frames(); len(got) != len(frames) {
		t.Fatalf("plugin saw %d frames, want %d", len(got), len(frames))
	} else {
		for i := range frames {
			if !bytes.Equal(got[i], frames[i]) {
				t.Fatalf("frame %d payload mismatch", i)
			}
		}
	}

	if got := countSegments(t, fixture.pool, noteID, true); got != 2 {
		t.Fatalf("provisional segments = %d, want 2", got)
	}
	if got := countSegments(t, fixture.pool, noteID, false); got != 0 {
		t.Fatalf("final segments before batch = %d, want 0", got)
	}
	note, err := fixture.st.GetNoteByID(context.Background(), noteID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if !note.PartialTranscript {
		t.Fatal("partial_transcript should be true after live provisional segments")
	}

	batchTranscriber := plugintest.NewTranscriber()
	t.Cleanup(batchTranscriber.Close)
	batchPluginID := registerPlugin(t, fixture.srv, fixture.hdr, model.PluginTranscriber, "batch-fake", batchTranscriber.URL(), "batch-token")
	if err := fixture.st.SetDefaultPlugin(context.Background(), batchPluginID); err != nil {
		t.Fatalf("set default batch plugin: %v", err)
	}
	agent := plugintest.NewAgent()
	t.Cleanup(agent.Close)
	agentPluginID := registerPlugin(t, fixture.srv, fixture.hdr, model.PluginAgent, "agent-fake", agent.URL(), "agent-token")
	if err := fixture.st.SetDefaultPlugin(context.Background(), agentPluginID); err != nil {
		t.Fatalf("set default agent plugin: %v", err)
	}

	proc := worker.NewProcessor(fixture.st, fixture.cr, fixture.prov, config.Config{}, nil)
	claimAndProcessJobs(t, proc, fixture.st)

	if got := countSegments(t, fixture.pool, noteID, true); got != 0 {
		t.Fatalf("provisional segments after batch = %d, want 0", got)
	}
	if got := countSegments(t, fixture.pool, noteID, false); got != 2 {
		t.Fatalf("final segments after batch = %d, want 2", got)
	}
	finalTranscript, err := fixture.st.GetTranscript(context.Background(), noteID)
	if err != nil {
		t.Fatalf("get transcript: %v", err)
	}
	if len(finalTranscript.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(finalTranscript.Segments))
	}
	if got, want := finalTranscript.Segments[0].Text, "Welcome everyone."; got != want {
		t.Fatalf("segment[0] = %q, want %q", got, want)
	}
	if got, want := finalTranscript.Segments[1].Text, "Let's begin."; got != want {
		t.Fatalf("segment[1] = %q, want %q", got, want)
	}
	note, err = fixture.st.GetNoteByID(context.Background(), noteID)
	if err != nil {
		t.Fatalf("get note after batch: %v", err)
	}
	if note.PartialTranscript {
		t.Fatal("partial_transcript should be false after batch success")
	}
}

func TestStreamingE2E_BatchOnlyDegradesWhenStreamingUnavailable(t *testing.T) {
	fixture := newStreamingE2EFixture(t)
	httpSrv := httptest.NewServer(fixture.srv.Handler())
	t.Cleanup(httpSrv.Close)

	streamOnlyNoteID := createStreamingNote(t, fixture.srv, fixture.hdr, "Live unavailable")
	conn := openStream(t, httpSrv.URL, streamOnlyNoteID, strings.TrimPrefix(fixture.hdr["Authorization"], "Bearer "))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read unavailable: %v", err)
	}
	var unavailable map[string]any
	if err := json.Unmarshal(payload, &unavailable); err != nil {
		t.Fatalf("decode unavailable: %v", err)
	}
	if unavailable["type"] != "unavailable" {
		t.Fatalf("payload = %v, want unavailable", unavailable)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected clean websocket close after unavailable")
	}

	batchOnlyNoteID := createStreamingNote(t, fixture.srv, fixture.hdr, "Batch only")
	setNoteAudioAndJob(t, fixture.st, batchOnlyNoteID)

	batchTranscriber := plugintest.NewTranscriber()
	t.Cleanup(batchTranscriber.Close)
	batchPluginID := registerPlugin(t, fixture.srv, fixture.hdr, model.PluginTranscriber, "batch-fake", batchTranscriber.URL(), "batch-token")
	if err := fixture.st.SetDefaultPlugin(context.Background(), batchPluginID); err != nil {
		t.Fatalf("set default batch plugin: %v", err)
	}
	agent := plugintest.NewAgent()
	t.Cleanup(agent.Close)
	agentPluginID := registerPlugin(t, fixture.srv, fixture.hdr, model.PluginAgent, "agent-fake", agent.URL(), "agent-token")
	if err := fixture.st.SetDefaultPlugin(context.Background(), agentPluginID); err != nil {
		t.Fatalf("set default agent plugin: %v", err)
	}

	proc := worker.NewProcessor(fixture.st, fixture.cr, fixture.prov, config.Config{}, nil)
	claimAndProcessJobs(t, proc, fixture.st)

	if got := countSegments(t, fixture.pool, batchOnlyNoteID, true); got != 0 {
		t.Fatalf("batch-only provisional segments = %d, want 0", got)
	}
	if got := countSegments(t, fixture.pool, batchOnlyNoteID, false); got != 2 {
		t.Fatalf("batch-only final segments = %d, want 2", got)
	}
	note, err := fixture.st.GetNoteByID(context.Background(), batchOnlyNoteID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if note.PartialTranscript {
		t.Fatal("partial_transcript should be false after batch-only success")
	}
	tr, err := fixture.st.GetTranscript(context.Background(), batchOnlyNoteID)
	if err != nil {
		t.Fatalf("get transcript: %v", err)
	}
	if len(tr.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(tr.Segments))
	}
	if got, want := tr.Segments[0].Text, "Welcome everyone."; got != want {
		t.Fatalf("segment[0] = %q, want %q", got, want)
	}
	if got, want := tr.Segments[1].Text, "Let's begin."; got != want {
		t.Fatalf("segment[1] = %q, want %q", got, want)
	}
	if got := countSegments(t, fixture.pool, streamOnlyNoteID, true); got != 0 {
		t.Fatalf("stream-only provisional segments = %d, want 0", got)
	}
}
