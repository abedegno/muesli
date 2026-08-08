//go:build !whisper_cgo

package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/pluginkit"
)

func TestTranscribeReturnsStubResult(t *testing.T) {
	t.Parallel()

	eng := New(Config{
		Model:    "stub-model",
		Language: "en",
	})

	pcm := make([]float32, 32000)
	got, err := eng.Transcribe(context.Background(), pcm, pluginkit.TranscribeRequest{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got.DurationMS != 2000 {
		t.Fatalf("duration_ms = %d, want 2000", got.DurationMS)
	}
	if got.Model != "stub-model" || got.Language != "en" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(got.Segments))
	}
	seg := got.Segments[0]
	if seg.StartMS != 0 || seg.EndMS != 2000 || seg.Source != "stub" || seg.Text != "whisper.cpp stub transcription" {
		t.Fatalf("segment = %+v", seg)
	}
}

func TestTranscribeEnsuresModelOnce(t *testing.T) {
	t.Parallel()

	var hits int32
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("stub-model"))
	}))
	defer modelSrv.Close()

	dir := t.TempDir()
	eng := New(Config{
		ModelDir: dir,
		ModelURL: modelSrv.URL + "/model.bin",
		Model:    "stub-model",
		Language: "en",
	})

	pcm := make([]float32, 16000)
	if _, err := eng.Transcribe(context.Background(), pcm, pluginkit.TranscribeRequest{Config: json.RawMessage(`{"model":"override-model","language":"de"}`)}); err != nil {
		t.Fatalf("first Transcribe: %v", err)
	}
	if _, err := eng.Transcribe(context.Background(), pcm, pluginkit.TranscribeRequest{Config: json.RawMessage(`{"model":"override-model","language":"de"}`)}); err != nil {
		t.Fatalf("second Transcribe: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("model hits = %d, want 1", got)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("glob model dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("cached files = %d, want 1", len(files))
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat cached model: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("cached model is empty")
	}
}

func TestEnsureModelRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	var hits int32
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&hits, 1) {
		case 1:
			http.Error(w, "temporary failure", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("retryable-model"))
		}
	}))
	defer modelSrv.Close()

	dir := t.TempDir()
	eng := New(Config{
		ModelDir: dir,
		ModelURL: modelSrv.URL + "/model.bin",
		Model:    "retryable-model",
		Language: "en",
	})

	if err := eng.ensureModel(context.Background()); err == nil {
		t.Fatal("first ensureModel succeeded, want error")
	}
	status, model, percent := eng.Status()
	if status != "error" || model != "retryable-model" || percent != 0 {
		t.Fatalf("status after failure = %q %q %d", status, model, percent)
	}

	if err := eng.ensureModel(context.Background()); err != nil {
		t.Fatalf("second ensureModel: %v", err)
	}
	status, model, percent = eng.Status()
	if status != "ready" || model != "retryable-model" || percent != 100 {
		t.Fatalf("status after success = %q %q %d", status, model, percent)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("model hits = %d, want 2", got)
	}
}

func TestStatusReturnsWhileModelLoads(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("slow-model"))
	}))
	defer modelSrv.Close()

	dir := t.TempDir()
	eng := New(Config{
		ModelDir: dir,
		ModelURL: modelSrv.URL + "/model.bin",
		Model:    "slow-model",
		Language: "en",
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- eng.ensureModel(context.Background())
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model request did not start")
	}

	statusCh := make(chan struct {
		status  string
		model   string
		percent int
	}, 1)
	go func() {
		status, model, percent := eng.Status()
		statusCh <- struct {
			status  string
			model   string
			percent int
		}{status: status, model: model, percent: percent}
	}()

	select {
	case got := <-statusCh:
		if got.status != "downloading" || got.model != "slow-model" || got.percent != 0 {
			t.Fatalf("status while loading = %+v", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Status blocked while model was loading")
	}

	close(release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ensureModel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ensureModel did not finish")
	}
}

func TestEffectiveConfigMergesRequestConfig(t *testing.T) {
	t.Parallel()

	eng := New(Config{
		Model:    "default-model",
		Language: "en",
	})
	cfg := eng.effectiveConfig(json.RawMessage(`{"model":"override-model","language":"it"}`))
	if cfg.Model != "override-model" || cfg.Language != "it" {
		t.Fatalf("effective config = %+v", cfg)
	}
}
