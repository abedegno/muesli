package pluginkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestEnsureModelCachesAndReportsProgress(t *testing.T) {
	var mu sync.Mutex
	var hits int
	payload := []byte("model-bytes-here")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Length", "16")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	var progress []int64
	onProgress := func(done, total int64) {
		_ = total
		progress = append(progress, done)
	}

	dir := t.TempDir()
	path1, err := EnsureModel(context.Background(), dir, srv.URL+"/model.bin", onProgress)
	if err != nil {
		t.Fatalf("ensure model: %v", err)
	}
	if path1 == "" {
		t.Fatal("empty model path")
	}
	path2, err := EnsureModel(context.Background(), dir, srv.URL+"/model.bin", onProgress)
	if err != nil {
		t.Fatalf("ensure cached model: %v", err)
	}
	if path2 != path1 {
		t.Fatalf("cached path changed: %q vs %q", path1, path2)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("download hits = %d, want 1", hits)
	}
	if len(progress) == 0 {
		t.Fatal("expected progress callbacks")
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("progress not monotonic: %v", progress)
		}
	}
}
