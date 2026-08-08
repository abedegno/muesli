//go:build !whisper_cgo

package live

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

func TestSessionProducesPartialAndFinal(t *testing.T) {
	eng := New(engine.Config{Model: "tiny.en", Language: "en"})
	req := pluginkit.StreamingStartRequest{Type: "start", SampleRate: 16_000, Channels: 1}
	var session pluginkit.StreamingEngineSession
	for range 1000 {
		var err error
		session, err = eng.StartStream(context.Background(), req)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if session == nil {
		t.Fatal("engine did not become ready")
	}
	speech := make([]float32, 24_000)
	for i := range speech {
		speech[i] = .25
	}
	events, err := session.WriteAudio(context.Background(), speech)
	if err != nil || len(events) != 1 || events[0].Final {
		t.Fatalf("partial events = %#v, err = %v", events, err)
	}
	events, err = session.WriteAudio(context.Background(), make([]float32, 12_000))
	if err != nil || len(events) != 1 || !events[0].Final || events[0].Text == "" {
		t.Fatalf("final events = %#v, err = %v", events, err)
	}
}
