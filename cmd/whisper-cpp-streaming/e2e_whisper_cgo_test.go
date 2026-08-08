//go:build whisper_cgo

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/internal/live"
	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

func TestRealAudioProducesPartialAndFinal(t *testing.T) {
	eng := live.New(engine.Config{ModelDir: filepath.Join(t.TempDir(), "models"), ModelURL: defaultTinyURL, Model: "tiny.en", Language: "en"})
	req := pluginkit.StreamingStartRequest{Type: "start", SampleRate: 16_000, Channels: 1}
	session := waitReady(t, func() (pluginkit.StreamingEngineSession, error) { return eng.StartStream(context.Background(), req) })
	partials, err := session.WriteAudio(context.Background(), testWAVPCM(t))
	if err != nil {
		t.Fatal(err)
	}
	finals, err := session.WriteAudio(context.Background(), make([]float32, 16_000))
	if err != nil {
		t.Fatal(err)
	}
	partialCount, finalCount, finalText := 0, 0, ""
	for _, event := range append(partials, finals...) {
		if event.Final {
			finalCount++
			finalText = event.Text
		} else {
			partialCount++
		}
	}
	if partialCount < 1 || finalCount != 1 || finalText == "" {
		t.Fatalf("partials=%d finals=%d finalText=%q", partialCount, finalCount, finalText)
	}
}
