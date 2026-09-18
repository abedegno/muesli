package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.
//
// The hosted-shaped end-to-end scenario the accepted plan for issue #764
// asks for: two API processes (two api.Server instances sharing one
// PostgreSQL) each serving one authenticated live-prompts viewer, a meeting
// driven through the real streaming websocket by the fake streaming
// transcriber, the real worker running the generated live_generate jobs
// against the stub agent, and both viewers reading the result over SSE.
// Interim text does not run; a final segment produces a prompt; growth after
// the target is fixed creates exactly one cadence-limited follow-up; both
// viewers update; batch replacement after the stream closes removes the
// prompts without affecting post-meeting work. The embedded desktop shape's
// transport is covered by e2e/specs/live-prompts.spec.ts.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/worker"
	"github.com/gorilla/websocket"
)

type liveE2EItem struct {
	TemplateID       string `json:"template_id"`
	Status           string `json:"status"`
	RenderedRevision int    `json:"rendered_revision"`
	DesiredRevision  int    `json:"desired_revision"`
	Sections         []struct {
		Heading         string `json:"heading"`
		ContentMarkdown string `json:"content_markdown"`
	} `json:"sections"`
}

// awaitLiveUpdate reads frames from one viewer until an update satisfies
// want, failing after a bounded number of frames or a 5s silence.
func awaitLiveUpdate(t *testing.T, viewer string, sse *liveSSEClient, what string, want func(liveE2EItem) bool) liveE2EItem {
	t.Helper()
	for attempt := 0; attempt < 12; attempt++ {
		f, ok := sse.next(t, 5*time.Second)
		if !ok {
			t.Fatalf("%s: stream closed or silent while waiting for %s", viewer, what)
		}
		if f.event != "update" {
			continue
		}
		var up struct {
			Item liveE2EItem `json:"item"`
		}
		if err := json.Unmarshal([]byte(f.data), &up); err != nil {
			t.Fatalf("%s: decode update: %v", viewer, err)
		}
		if want(up.Item) {
			return up.Item
		}
	}
	t.Fatalf("%s: no update matching %s within 12 frames", viewer, what)
	return liveE2EItem{}
}

func awaitLiveEnded(t *testing.T, viewer string, sse *liveSSEClient, templateID string) {
	t.Helper()
	for attempt := 0; attempt < 12; attempt++ {
		f, ok := sse.next(t, 5*time.Second)
		if !ok {
			t.Fatalf("%s: stream closed or silent while waiting for ended", viewer)
		}
		if f.event != "ended" {
			continue
		}
		var ended struct {
			TemplateID string `json:"template_id"`
		}
		if err := json.Unmarshal([]byte(f.data), &ended); err != nil {
			t.Fatalf("%s: decode ended: %v", viewer, err)
		}
		if ended.TemplateID == templateID {
			return
		}
	}
	t.Fatalf("%s: no ended event for the template within 12 frames", viewer)
}

func readWSSegment(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read stream message: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode stream message: %v", err)
	}
	if msg["type"] != "segment" {
		t.Fatalf("stream message = %v, want a segment", msg)
	}
	return msg
}

func TestLivePromptsE2E_TwoProcessViewers(t *testing.T) {
	fixture := newStreamingE2EFixture(t)
	ctx := context.Background()
	st := fixture.st
	hdr := fixture.hdr
	token := strings.TrimPrefix(hdr["Authorization"], "Bearer ")

	// Two API processes over one database. Each server that serves a stream
	// starts a LISTEN connection; Close releases it before the pool's cleanup.
	srvA := fixture.srv
	t.Cleanup(srvA.Close)
	srvB := api.NewServer(api.Deps{Store: st, Storage: fixture.prov, Crypto: fixture.cr})
	t.Cleanup(srvB.Close)
	httpA := httptest.NewServer(srvA.Handler())
	t.Cleanup(httpA.Close)
	httpB := httptest.NewServer(srvB.Handler())
	t.Cleanup(httpB.Close)

	// The stub agent renders every template section; the fake streaming
	// transcriber emits one segment per audio frame: an interim first, then
	// finals.
	agent := plugintest.NewAgent()
	t.Cleanup(agent.Close)
	if err := st.EnsureDefaultPlugin(ctx, fixture.cr, model.PluginAgent, "agent", agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	streamPlugin := newFakeStreamingPlugin(t, "stream-token", []fakeStreamingSegment{
		{AfterFrames: 1, Text: "interim only", StartMS: 0, EndMS: 1000, Final: boolPtr(false)},
		{AfterFrames: 1, Text: "first final", StartMS: 0, EndMS: 1000},
		{AfterFrames: 1, Text: "second final", StartMS: 1000, EndMS: 2000},
		{AfterFrames: 1, Text: "third final", StartMS: 2000, EndMS: 3000},
	}, 0)
	streamID := registerPlugin(t, srvA, hdr, model.PluginStreamingTranscriber, "streaming-fake", streamPlugin.URL(), "stream-token")
	if err := st.SetDefaultPlugin(ctx, streamID); err != nil {
		t.Fatalf("set default streaming plugin: %v", err)
	}
	batch := newSingleSegmentTranscriber(t, model.Segment{StartMS: 0, EndMS: 3000, Text: "the whole meeting", Source: "batch"})
	batchID := registerPlugin(t, srvA, hdr, model.PluginTranscriber, "batch-fake", batch.URL, "batch-token")
	if err := st.SetDefaultPlugin(ctx, batchID); err != nil {
		t.Fatalf("set default batch transcriber: %v", err)
	}

	owner, err := st.GetUserByEmail(ctx, "streamer@example.com")
	if err != nil {
		t.Fatalf("lookup owner: %v", err)
	}
	sections := []model.TemplateSection{{Heading: "Live", Instruction: "Summarize so far."}}
	tmpl, err := st.CreateTemplate(ctx, owner.ID, "live-e2e", "during", sections, true, "", "", nil)
	if err != nil {
		t.Fatalf("create live template: %v", err)
	}
	noteID := createStreamingNote(t, srvA, hdr, "Live e2e")

	viewerA := openLiveSSE(t, httpA.URL, noteID, token)
	defer viewerA.close()
	viewerB := openLiveSSE(t, httpB.URL, noteID, token)
	defer viewerB.close()
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		snap, ok := v.next(t, 5*time.Second)
		if !ok || snap.event != "snapshot" {
			t.Fatalf("%s: expected snapshot, got %+v ok=%v", name, snap, ok)
		}
		var payload struct {
			Items []liveE2EItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(snap.data), &payload); err != nil || len(payload.Items) != 0 {
			t.Fatalf("%s: expected an empty snapshot before speech, got %s err=%v", name, snap.data, err)
		}
	}

	conn := openStream(t, httpA.URL, noteID, token)
	if _, payload, err := conn.ReadMessage(); err != nil || !strings.Contains(string(payload), `"ready"`) {
		t.Fatalf("expected ready, got %s err=%v", payload, err)
	}
	frames := pcmFixtureAllFrames(t)
	sendFrame := func(i int) {
		t.Helper()
		if err := conn.WriteMessage(websocket.BinaryMessage, frames[i%len(frames)]); err != nil {
			t.Fatalf("write pcm frame %d: %v", i, err)
		}
	}

	// 1. Interim-only text schedules nothing.
	sendFrame(0)
	if seg := readWSSegment(t, conn); seg["final"] != false {
		t.Fatalf("first segment should be interim, got %v", seg)
	}
	var liveJobs int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs WHERE note_id=$1 AND type=$2`, noteID, model.JobLiveGenerate).Scan(&liveJobs); err != nil {
		t.Fatalf("count live jobs: %v", err)
	}
	if rows, err := st.ListVisibleLiveTemplateOutputs(ctx, owner.ID, noteID); err != nil || len(rows) != 0 || liveJobs != 0 {
		t.Fatalf("interim text must not schedule work: rows=%+v jobs=%d err=%v", rows, liveJobs, err)
	}

	// 2. The first final segment produces a queued prompt on both viewers.
	sendFrame(1)
	if seg := readWSSegment(t, conn); seg["final"] != true {
		t.Fatalf("second segment should be final, got %v", seg)
	}
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		awaitLiveUpdate(t, name, v, "queued at revision 1", func(it liveE2EItem) bool {
			return it.TemplateID == tmpl.ID && it.Status == model.LiveOutputPending && it.DesiredRevision == 1
		})
	}

	// 3. A second final before the worker runs raises demand on both viewers.
	sendFrame(2)
	readWSSegment(t, conn)
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		awaitLiveUpdate(t, name, v, "demand at revision 2", func(it liveE2EItem) bool {
			return it.TemplateID == tmpl.ID && it.DesiredRevision == 2
		})
	}

	// 4. The worker's live claim fixes target_revision at 2; growth committed
	// after that (the third final) must not move it, and completion must
	// schedule exactly one cadence-limited follow-up for revision 3.
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok || job.Type != model.JobLiveGenerate {
		t.Fatalf("claim live job: ok=%v type=%q err=%v", ok, job.Type, err)
	}
	claim, err := st.ClaimLiveGenerateJobTx(ctx, job, time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC))
	if err != nil || !claim.Valid || claim.TargetRevision != 2 {
		t.Fatalf("live claim: valid=%v target=%d err=%v, want a valid claim at target 2", claim.Valid, claim.TargetRevision, err)
	}
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		awaitLiveUpdate(t, name, v, "running", func(it liveE2EItem) bool {
			return it.TemplateID == tmpl.ID && it.Status == model.LiveOutputRunning
		})
	}
	sendFrame(3)
	readWSSegment(t, conn)
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		awaitLiveUpdate(t, name, v, "demand at revision 3", func(it liveE2EItem) bool {
			return it.TemplateID == tmpl.ID && it.DesiredRevision == 3
		})
	}
	proc := worker.NewProcessor(st, fixture.cr, fixture.prov, config.Config{}, nil)
	proc.Process(ctx, job)
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		it := awaitLiveUpdate(t, name, v, "rendered at revision 2 with a follow-up queued", func(it liveE2EItem) bool {
			return it.TemplateID == tmpl.ID && it.RenderedRevision == 2 && len(it.Sections) > 0
		})
		if it.DesiredRevision != 3 || it.Status != model.LiveOutputPending {
			t.Fatalf("%s: after completion status=%q desired=%d, want pending (follow-up queued) at desired 3", name, it.Status, it.DesiredRevision)
		}
		if !strings.Contains(it.Sections[0].ContentMarkdown, "Stub summary") {
			t.Fatalf("%s: sections did not come from the agent: %+v", name, it.Sections)
		}
	}
	var followUps int
	var cadenceHeld bool
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*), bool_and(lease_expires_at > now() + interval '10 seconds' AND lease_expires_at <= now() + interval '16 seconds')
		   FROM jobs WHERE note_id=$1 AND type=$2 AND status=$3`,
		noteID, model.JobLiveGenerate, model.JobPending).Scan(&followUps, &cadenceHeld); err != nil {
		t.Fatalf("count follow-ups: %v", err)
	}
	if followUps != 1 || !cadenceHeld {
		t.Fatalf("follow-up jobs = %d (cadence held: %v), want exactly one not-before ~15s out", followUps, cadenceHeld)
	}

	// 5. The stream closes and batch transcription replaces it: prompts end on
	// both viewers, the follow-up is cancelled, and post-meeting work (the
	// batch transcript and its summaries) proceeds untouched.
	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatalf("write close: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected the websocket to close after the client's close")
	}
	_ = conn.Close()
	setNoteAudioAndJob(t, st, noteID)
	claimAndProcessJobs(t, proc, st)
	for name, v := range map[string]*liveSSEClient{"viewer A": viewerA, "viewer B": viewerB} {
		awaitLiveEnded(t, name, v, tmpl.ID)
	}
	if rows, err := st.ListVisibleLiveTemplateOutputs(ctx, owner.ID, noteID); err != nil || len(rows) != 0 {
		t.Fatalf("live rows after batch replacement: %+v err=%v, want none", rows, err)
	}
	var pendingLive int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs WHERE note_id=$1 AND type=$2 AND status=$3`,
		noteID, model.JobLiveGenerate, model.JobPending).Scan(&pendingLive); err != nil {
		t.Fatalf("count pending live jobs: %v", err)
	}
	if pendingLive != 0 {
		t.Fatalf("pending live jobs after the stream ended = %d, want 0", pendingLive)
	}
	summaries, err := st.GetSummaries(ctx, noteID)
	if err != nil || len(summaries) == 0 {
		t.Fatalf("post-meeting summaries: %v %+v, want at least one", err, summaries)
	}
	rec := doJSON(t, srvB, http.MethodGet, "/api/notes/"+noteID+"/full", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("full note after the meeting via the second process: %d %s", rec.Code, rec.Body)
	}
	_ = store.ErrNotFound
}
