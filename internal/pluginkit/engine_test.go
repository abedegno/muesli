package pluginkit

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

type serveTestTranscriber struct{}

func (serveTestTranscriber) Transcribe(_ context.Context, _ []float32, _ TranscribeRequest) (TranscribeResult, error) {
	return TranscribeResult{
		Segments:   []model.Segment{{StartMS: 0, EndMS: 1, Text: "hi", Source: "mic"}},
		Language:   "en",
		Model:      "m",
		DurationMS: 1,
	}, nil
}

type serveTestAgent struct{}

func (serveTestAgent) Generate(_ context.Context, _ GenerateRequest) (GenerateResponse, error) {
	return GenerateResponse{
		Summary: SummaryPayload{Sections: []model.SummarySection{{Heading: "Overview", ContentMarkdown: "done"}}},
		Model:   "m",
	}, nil
}

func waitForHTTP(t *testing.T, url string, hdr map[string]string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", url)
}

func TestServeTranscriber(t *testing.T) {
	addr := freeLoopbackAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- ServeTranscriber(ctx, Config{Name: "plug", Version: "1.0", Token: "secret", Addr: addr}, serveTestTranscriber{})
	}()

	waitForHTTP(t, "http://"+addr+"/health", nil)
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/info", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeTranscriber: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for transcriber shutdown")
	}
}

func TestServeAgent(t *testing.T) {
	addr := freeLoopbackAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- ServeAgent(ctx, Config{Name: "plug", Version: "1.0", Token: "secret", Addr: addr}, serveTestAgent{})
	}()

	waitForHTTP(t, "http://"+addr+"/health", nil)
	payload := map[string]any{
		"transcript":     []map[string]any{},
		"notes_markdown": "",
		"template":       map[string]any{"sections": []map[string]any{}},
		"config":         map[string]any{},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/generate", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeAgent: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for agent shutdown")
	}
}

func TestNewServerNormalizesLoopback(t *testing.T) {
	srv, ln, err := NewServer(Config{Addr: "0.0.0.0:0"}, http.NewServeMux())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer ln.Close()
	if host, _, err := net.SplitHostPort(ln.Addr().String()); err != nil || host != "127.0.0.1" {
		t.Fatalf("listener addr = %s err=%v", ln.Addr(), err)
	}
	_ = srv
}
