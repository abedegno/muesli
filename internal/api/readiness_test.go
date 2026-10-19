package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadinessCoalescesTwentyConcurrentSessionsAndIsolatesServers(t *testing.T) {
	t.Parallel()
	seed, st, hdr := adminServer(t)
	for _, kind := range []string{model.PluginTranscriber, model.PluginStreamingTranscriber, model.PluginAgent} {
		rec := doJSON(t, seed, http.MethodPost, "/api/admin/plugins", map[string]any{"kind": kind, "name": kind, "endpoint_url": "http://private-internal:9000", "token": "private-test-token", "enabled": true}, hdr)
		if rec.Code != 201 {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
		var plugin struct{ ID string }
		if err := json.Unmarshal(rec.Body.Bytes(), &plugin); err != nil {
			t.Fatal(err)
		}
		rec = doJSON(t, seed, http.MethodPatch, "/api/admin/plugins/"+plugin.ID, map[string]any{"is_default": true}, hdr)
		if rec.Code != 200 {
			t.Fatalf("default: %d %s", rec.Code, rec.Body)
		}
	}
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	var probes atomic.Int32
	gate := make(chan struct{})
	srv := api.NewServer(api.Deps{Store: st, Crypto: cr, PluginHealthProber: func(ctx context.Context, _, _ string) error {
		probes.Add(1)
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	var group sync.WaitGroup
	responses := make(chan string, 20)
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			rec := doJSON(t, srv, http.MethodGet, "/api/readiness", nil, hdr)
			responses <- fmt.Sprintf("%d %s", rec.Code, rec.Body.String())
		}()
	}
	deadline := time.Now().Add(time.Second * 5)
	for probes.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(gate)
	group.Wait()
	close(responses)
	if probes.Load() != 3 {
		t.Fatalf("expected three shared probes for twenty sessions, got %d", probes.Load())
	}
	for body := range responses {
		if !strings.HasPrefix(body, "200 ") || strings.Contains(body, "private-") || !strings.Contains(body, `"healthy":true`) {
			t.Errorf("unsafe or failed readiness: %s", body)
		}
	}
	other := api.NewServer(api.Deps{Store: st, Crypto: cr, PluginHealthProber: func(context.Context, string, string) error { return errors.New("private secret error") }})
	rec := doJSON(t, other, http.MethodGet, "/api/readiness", nil, hdr)
	if strings.Contains(rec.Body.String(), `"healthy":true`) || strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("cross-server readiness cache or leaked error: %s", rec.Body)
	}
}

func TestExpiredCaptureIsFlaggedForReviewWithoutEndingAnotherDevicesRecording(t *testing.T) {
	t.Parallel()
	srv, st, hdr := adminServer(t)
	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Interrupted"}, hdr)
	var note struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/start-capture", map[string]string{"capture_id": "aa48a4c5-f036-4255-b5f4-c2e08cc028f9"}, hdr)
	if rec.Code != 200 {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if _, err := st.Pool().Exec(context.Background(), `UPDATE notes SET capture_expires_at=now()-interval '1 second' WHERE id=$1`, note.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/notes/" + note.ID, "/api/notes"} {
		rec = doJSON(t, srv, http.MethodGet, path, nil, hdr)
		if !strings.Contains(rec.Body.String(), `"capture_needs_review":true`) {
			t.Fatalf("expired capture presented as active: %s", rec.Body)
		}
	}
}
