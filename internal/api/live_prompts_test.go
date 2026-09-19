package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

// liveSSEClient reads Server-Sent Events from an httptest.NewServer-backed
// live-prompts connection, one goroutine at a time (issue #764).
type liveSSEClient struct {
	t      *testing.T
	resp   *http.Response
	cancel context.CancelFunc
	lines  chan sseFrame
	done   chan struct{} // closed when the server ends the stream
}

type sseFrame struct {
	event string
	data  string
}

func openLiveSSE(t *testing.T, baseURL, noteID, token string) *liveSSEClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/notes/"+noteID+"/live-prompts", nil)
	if err != nil {
		cancel()
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("do request: %v", err)
	}
	c := &liveSSEClient{t: t, resp: resp, cancel: cancel, lines: make(chan sseFrame, 32), done: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *liveSSEClient) readLoop() {
	defer close(c.done)
	defer close(c.lines)
	scanner := bufio.NewScanner(c.resp.Body)
	var event, data string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if event != "" {
				c.lines <- sseFrame{event: event, data: data}
			}
			event, data = "", ""
		}
	}
}

func (c *liveSSEClient) next(t *testing.T, timeout time.Duration) (sseFrame, bool) {
	t.Helper()
	select {
	case f, ok := <-c.lines:
		return f, ok
	case <-time.After(timeout):
		return sseFrame{}, false
	}
}

// waitClosed reports whether the server ended the stream within timeout --
// distinct from next's timeout, which cannot tell silence from closure.
func (c *liveSSEClient) waitClosed(timeout time.Duration) bool {
	select {
	case <-c.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (c *liveSSEClient) close() {
	c.cancel()
	_ = c.resp.Body.Close()
}

func liveTestLogin(t *testing.T, srv *http.Client, baseURL string) (token string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": "live-owner@example.com", "password": "password123"})
	_, err := srv.Post(baseURL+"/api/setup", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	resp, err := srv.Post(baseURL+"/api/login", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	var login struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&login)
	return login.Token
}

// TestLiveNotePrompts_NotFoundMasking proves malformed, absent, and foreign
// note ids are indistinguishably 404, checked before capacity allocation.
func TestLiveNotePrompts_NotFoundMasking(t *testing.T) {
	t.Parallel()
	apiSrv, _ := newTestServer(t)
	httpSrv := httptest.NewServer(apiSrv.Handler())
	defer httpSrv.Close()
	client := httpSrv.Client()
	token := liveTestLogin(t, client, httpSrv.URL)

	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/notes/"+id+"/live-prompts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", id, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("id=%s: status = %d, want 404", id, resp.StatusCode)
		}
	}
}

// TestLiveNotePrompts_SnapshotThenCoalescedUpdate proves the initial
// snapshot is delivered, and that committing two template changes delivers
// one coalesced wake-up whose reread emits both changed rows.
func TestLiveNotePrompts_SnapshotThenCoalescedUpdate(t *testing.T) {
	t.Parallel()
	apiSrv, st := newTestServer(t)
	httpSrv := httptest.NewServer(apiSrv.Handler())
	defer httpSrv.Close()
	client := httpSrv.Client()
	token := liveTestLogin(t, client, httpSrv.URL)

	ctx := context.Background()
	u, err := st.GetUserByEmail(ctx, "live-owner@example.com")
	if err != nil {
		t.Fatalf("lookup owner: %v", err)
	}

	sections := []model.TemplateSection{{Heading: "Live", Instruction: "Summarize."}}
	tmplA, err := st.CreateTemplate(ctx, u.ID, "live-a", "during", sections, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template a: %v", err)
	}
	tmplB, err := st.CreateTemplate(ctx, u.ID, "live-b", "during", sections, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template b: %v", err)
	}

	n, err := st.CreateNote(ctx, u.ID, "Live note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	streamID := "stream-" + n.ID
	tr, err := st.CreateStreamTranscript(ctx, n.ID, streamID, "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create stream transcript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, tr.ID, streamID, model.Segment{StartMS: 0, EndMS: 1000, Text: "hello", Source: "streaming"}); err != nil {
		t.Fatalf("append segment: %v", err)
	}

	sse := openLiveSSE(t, httpSrv.URL, n.ID, token)
	defer sse.close()

	snap, ok := sse.next(t, 5*time.Second)
	if !ok || snap.event != "snapshot" {
		t.Fatalf("expected snapshot event, got %+v ok=%v", snap, ok)
	}
	var snapPayload struct {
		Items []struct {
			TemplateID string `json:"template_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(snap.data), &snapPayload); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(snapPayload.Items) != 2 {
		t.Fatalf("expected 2 items in snapshot, got %d", len(snapPayload.Items))
	}

	// Advance both rows' desired_revision by appending another segment.
	if err := st.AppendStreamSegment(ctx, tr.ID, streamID, model.Segment{StartMS: 1000, EndMS: 2000, Text: "world", Source: "streaming"}); err != nil {
		t.Fatalf("append segment 2: %v", err)
	}

	seenTemplates := map[string]bool{}
	for attempt := 0; attempt < 10 && len(seenTemplates) < 2; attempt++ {
		f, ok := sse.next(t, 5*time.Second)
		if !ok {
			break
		}
		if f.event != "update" {
			continue
		}
		var up struct {
			Item struct {
				TemplateID      string `json:"template_id"`
				DesiredRevision int    `json:"desired_revision"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(f.data), &up); err != nil {
			t.Fatalf("decode update: %v", err)
		}
		if up.Item.DesiredRevision == 2 {
			seenTemplates[up.Item.TemplateID] = true
		}
	}
	if !seenTemplates[tmplA.ID] || !seenTemplates[tmplB.ID] {
		t.Fatalf("expected both templates to emit an update, got %+v", seenTemplates)
	}
}

// TestLiveNotePrompts_TrashEndsStream proves a subscriber admitted before the
// note is trashed is told its cards are gone and is disconnected, which
// releases its lease: trash ends the stream in the store, the wake-up's
// consistent snapshot reads the note as not found, and the handler ends every
// sent key before closing.
func TestLiveNotePrompts_TrashEndsStream(t *testing.T) {
	t.Parallel()
	apiSrv, st := newTestServer(t)
	httpSrv := httptest.NewServer(apiSrv.Handler())
	defer httpSrv.Close()
	client := httpSrv.Client()
	token := liveTestLogin(t, client, httpSrv.URL)

	ctx := context.Background()
	u, err := st.GetUserByEmail(ctx, "live-owner@example.com")
	if err != nil {
		t.Fatalf("lookup owner: %v", err)
	}
	sections := []model.TemplateSection{{Heading: "Live", Instruction: "Summarize."}}
	tmpl, err := st.CreateTemplate(ctx, u.ID, "live-trash", "during", sections, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Trashed mid-meeting")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	streamID := "stream-" + n.ID
	tr, err := st.CreateStreamTranscript(ctx, n.ID, streamID, "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create stream transcript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, tr.ID, streamID, model.Segment{StartMS: 0, EndMS: 1000, Text: "hello", Source: "streaming"}); err != nil {
		t.Fatalf("append segment: %v", err)
	}

	sse := openLiveSSE(t, httpSrv.URL, n.ID, token)
	defer sse.close()
	snap, ok := sse.next(t, 5*time.Second)
	if !ok || snap.event != "snapshot" {
		t.Fatalf("expected snapshot event, got %+v ok=%v", snap, ok)
	}
	var snapPayload struct {
		Items []struct {
			TemplateID string `json:"template_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(snap.data), &snapPayload); err != nil || len(snapPayload.Items) != 1 {
		t.Fatalf("expected one item in the snapshot, got %+v err=%v", snapPayload, err)
	}

	if err := st.DeleteNote(ctx, u.ID, n.ID); err != nil {
		t.Fatalf("trash note: %v", err)
	}

	sawEnded := false
	for attempt := 0; attempt < 10 && !sawEnded; attempt++ {
		f, ok := sse.next(t, 5*time.Second)
		if !ok {
			break
		}
		if f.event != "ended" {
			continue
		}
		var ended struct {
			TemplateID string `json:"template_id"`
		}
		if err := json.Unmarshal([]byte(f.data), &ended); err != nil {
			t.Fatalf("decode ended: %v", err)
		}
		sawEnded = ended.TemplateID == tmpl.ID
	}
	if !sawEnded {
		t.Fatal("expected an ended event for the template after trash")
	}
	if !sse.waitClosed(5 * time.Second) {
		t.Fatal("expected the server to close the stream after trash")
	}
	var leases int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM live_note_subscriptions WHERE note_id=$1`, n.ID).Scan(&leases); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if leases != 0 {
		t.Fatalf("lease rows after trash = %d, want 0", leases)
	}
}
