package plugintest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/plugintest"
)

func TestStubTranscriber(t *testing.T) {
	stub := plugintest.NewTranscriber()
	defer stub.Close()

	c := plugin.New(stub.URL(), "any-token")
	resp, err := c.Transcribe(context.Background(), plugin.TranscribeRequest{
		AudioURL: "http://x", Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if len(resp.Segments) == 0 || resp.Segments[0].Text == "" {
		t.Fatalf("stub returned no segments: %+v", resp)
	}
}

func TestStubAgent(t *testing.T) {
	stub := plugintest.NewAgent()
	defer stub.Close()

	c := plugin.New(stub.URL(), "any-token")
	resp, err := c.Generate(context.Background(), plugin.GenerateRequest{
		Transcript: []model.Segment{{Text: "x", Source: "mic"}},
		Template:   plugin.TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Sum."}}},
		Config:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(resp.Summary.Sections) != 1 || resp.Summary.Sections[0].Heading != "Overview" {
		t.Fatalf("stub summary wrong: %+v", resp)
	}
}

func TestStubFailureInjection(t *testing.T) {
	stub := plugintest.NewTranscriber()
	defer stub.Close()
	stub.FailNext(2) // next 2 calls return 500, then succeed

	c := plugin.New(stub.URL(), "tok")
	for i := 0; i < 2; i++ {
		if _, err := c.Transcribe(context.Background(), plugin.TranscribeRequest{AudioURL: "u", Config: json.RawMessage(`{}`)}); err == nil {
			t.Fatalf("call %d: expected failure", i)
		}
	}
	if _, err := c.Transcribe(context.Background(), plugin.TranscribeRequest{AudioURL: "u", Config: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("third call should succeed, got %v", err)
	}
}
