package pluginkit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

type recordingTranscriber struct {
	gotPCM []float32
	gotReq TranscribeRequest
}

func (r *recordingTranscriber) Transcribe(_ context.Context, pcm []float32, req TranscribeRequest) (TranscribeResult, error) {
	r.gotPCM = append([]float32(nil), pcm...)
	r.gotReq = req
	return TranscribeResult{
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 750, Text: "alpha", Source: "mic"},
			{StartMS: 750, EndMS: 1500, Text: "beta", Source: "mic"},
		},
		Language:   "en",
		Model:      "fake-transcriber",
		DurationMS: 1500,
	}, nil
}

type recordingAgent struct {
	gotReq GenerateRequest
}

func (r *recordingAgent) Generate(_ context.Context, req GenerateRequest) (GenerateResponse, error) {
	r.gotReq = req
	return GenerateResponse{
		Summary: SummaryPayload{Sections: []model.SummarySection{
			{Heading: "Overview", ContentMarkdown: "Done."},
		}},
		Model: "fake-agent",
	}, nil
}

func TestTranscriberHandler(t *testing.T) {
	eng := &recordingTranscriber{}
	srv := httptest.NewServer(TranscriberHandler(Config{Name: "plug", Version: "1.0", Token: "secret"}, eng))
	defer srv.Close()

	infoReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/info", nil)
	infoReq.Header.Set("Authorization", "Bearer secret")
	infoRec, err := http.DefaultClient.Do(infoReq)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	defer infoRec.Body.Close()
	if infoRec.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d", infoRec.StatusCode)
	}
	var info Info
	if err := json.NewDecoder(infoRec.Body).Decode(&info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.PluginAPI != 1 || info.Kind != "transcriber" || string(info.ConfigSchema) != "{}" {
		t.Fatalf("info = %+v", info)
	}

	healthRec, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	healthRec.Body.Close()
	if healthRec.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", healthRec.StatusCode)
	}

	t.Run("transcribe", func(t *testing.T) {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Skip("ffmpeg not available on PATH")
		}
		body := map[string]any{"audio_url": tinyWAVDataURL(160), "config": map[string]any{}}
		reqBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/transcribe", bytes.NewReader(reqBody))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("transcribe: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("transcribe status = %d", resp.StatusCode)
		}
		var out TranscribeResult
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode transcribe: %v", err)
		}
		if len(out.Segments) != 2 || out.Segments[0].Text != "alpha" || out.Model != "fake-transcriber" {
			t.Fatalf("transcribe response = %+v", out)
		}
		if len(eng.gotPCM) == 0 {
			t.Fatal("expected pcm to be decoded")
		}
		if eng.gotReq.Config == nil {
			t.Fatal("expected config to be forwarded")
		}

		shimReqBody, _ := json.Marshal(map[string]any{"audio_url": tinyWAVDataURL(160)})
		shimReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/audio/transcriptions", bytes.NewReader(shimReqBody))
		shimReq.Header.Set("Authorization", "Bearer secret")
		shimReq.Header.Set("Content-Type", "application/json")
		shimResp, err := http.DefaultClient.Do(shimReq)
		if err != nil {
			t.Fatalf("shim: %v", err)
		}
		defer shimResp.Body.Close()
		if shimResp.StatusCode != http.StatusOK {
			t.Fatalf("shim status = %d", shimResp.StatusCode)
		}
		var shimOut map[string]string
		if err := json.NewDecoder(shimResp.Body).Decode(&shimOut); err != nil {
			t.Fatalf("decode shim: %v", err)
		}
		if shimOut["text"] != "alpha beta" {
			t.Fatalf("shim text = %q", shimOut["text"])
		}
	})

	t.Run("multitrack", func(t *testing.T) {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Skip("ffmpeg not available on PATH")
		}
		eng.gotPCM = nil
		body := map[string]any{"audio_url": stereoWAVDataURL(), "config": map[string]any{"multitrack": true}}
		reqBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/transcribe", bytes.NewReader(reqBody))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("transcribe multitrack: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("transcribe multitrack status = %d", resp.StatusCode)
		}
		var out TranscribeResult
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode transcribe multitrack: %v", err)
		}
		if len(out.Segments) != 2 {
			t.Fatalf("transcribe multitrack response = %+v", out)
		}
		if out.Segments[0].Speaker != "You" || out.Segments[0].Source != "mic" {
			t.Fatalf("segment[0] = %+v", out.Segments[0])
		}
		if out.Segments[1].Speaker != "Them" || out.Segments[1].Source != "system" {
			t.Fatalf("segment[1] = %+v", out.Segments[1])
		}
		if len(eng.gotPCM) != 0 {
			t.Fatalf("expected multitrack path to bypass single-pass pcm recording, got %d samples", len(eng.gotPCM))
		}
	})

	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{"info", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/info", nil)
			return req
		}()},
		{"transcribe", func() *http.Request {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/transcribe", bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")
			return req
		}()},
	} {
		resp, err := http.DefaultClient.Do(tc.req)
		if err != nil {
			t.Fatalf("%s missing auth: %v", tc.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s missing auth status = %d", tc.name, resp.StatusCode)
		}
	}

	wrong, _ := http.NewRequest(http.MethodPost, srv.URL+"/transcribe", bytes.NewReader([]byte(`{}`)))
	wrong.Header.Set("Authorization", "Bearer wrong")
	wrong.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(wrong)
	if err != nil {
		t.Fatalf("wrong token transcribe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", resp.StatusCode)
	}
}

func TestAgentHandler(t *testing.T) {
	eng := &recordingAgent{}
	srv := httptest.NewServer(AgentHandler(Config{Name: "plug", Version: "1.0", Token: "secret"}, eng))
	defer srv.Close()

	reqBody, _ := json.Marshal(map[string]any{
		"transcript":     []map[string]any{{"start_ms": 0, "end_ms": 1, "text": "hi", "source": "mic"}},
		"notes_markdown": "- note",
		"template":       map[string]any{"sections": []map[string]any{{"heading": "Overview", "instruction": "Summarise."}}},
		"config":         map[string]any{},
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/generate", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status = %d", resp.StatusCode)
	}
	var out GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode generate: %v", err)
	}
	if len(out.Summary.Sections) != 1 || out.Summary.Sections[0].Heading != "Overview" || out.Model != "fake-agent" {
		t.Fatalf("generate response = %+v", out)
	}
	if len(eng.gotReq.Transcript) != 1 || eng.gotReq.Template.Sections == nil {
		t.Fatalf("engine request = %+v", eng.gotReq)
	}

	infoReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/info", nil)
	infoReq.Header.Set("Authorization", "Bearer secret")
	infoResp, err := http.DefaultClient.Do(infoReq)
	if err != nil {
		t.Fatalf("agent info: %v", err)
	}
	infoResp.Body.Close()
	if infoResp.StatusCode != http.StatusOK {
		t.Fatalf("agent info status = %d", infoResp.StatusCode)
	}

	missingAuth, _ := http.NewRequest(http.MethodPost, srv.URL+"/generate", bytes.NewReader([]byte(`{}`)))
	missingAuth.Header.Set("Content-Type", "application/json")
	unauthResp, err := http.DefaultClient.Do(missingAuth)
	if err != nil {
		t.Fatalf("missing auth generate: %v", err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d", unauthResp.StatusCode)
	}
}
