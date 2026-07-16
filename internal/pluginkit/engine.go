package pluginkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// Transcriber implements the shared plugin contract for transcription plugins.
type Transcriber interface {
	Transcribe(ctx context.Context, pcm []float32, opts TranscribeRequest) (TranscribeResult, error)
}

// Agent implements the shared plugin contract for summarization plugins.
type Agent interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

// Config is the runtime config the caller supplies to the pluginkit server.
type Config struct {
	Name         string
	Version      string
	Kind         string
	Token        string
	Addr         string
	ModelDir     string
	ConfigSchema json.RawMessage
}

// ServeTranscriber serves the transcriber contract on a loopback listener and
// blocks until ctx is cancelled.
func ServeTranscriber(ctx context.Context, cfg Config, eng Transcriber) error {
	srv, ln, err := NewServer(cfg, TranscriberHandler(cfg, eng))
	if err != nil {
		return err
	}
	return serve(ctx, srv, ln)
}

// ServeAgent serves the agent contract on a loopback listener and blocks until
// ctx is cancelled.
func ServeAgent(ctx context.Context, cfg Config, eng Agent) error {
	srv, ln, err := NewServer(cfg, AgentHandler(cfg, eng))
	if err != nil {
		return err
	}
	return serve(ctx, srv, ln)
}

func TranscriberHandler(cfg Config, eng Transcriber) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/status", handleStatus)
	mux.Handle("/info", requireBearer(cfg.Token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, Info{
			Name:         cfg.Name,
			Version:      cfg.Version,
			PluginAPI:    1,
			Kind:         "transcriber",
			ConfigSchema: infoConfigSchema(cfg.ConfigSchema),
		})
	})))
	mux.Handle("/transcribe", requireBearer(cfg.Token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var req TranscribeRequest
		if err := decodeJSON(r, &req); err != nil {
			badRequest(w, err)
			return
		}
		if err := validateTranscribeRequest(req); err != nil {
			badRequest(w, err)
			return
		}
		var mt struct {
			Multitrack bool `json:"multitrack"`
		}
		_ = json.Unmarshal(req.Config, &mt)
		if mt.Multitrack {
			res, err := runMultitrack(r.Context(), req.AudioURL, eng, req)
			if err != nil {
				internalServerError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
			return
		}
		pcm, err := DecodePCM(r.Context(), req.AudioURL)
		if err != nil {
			badRequest(w, err)
			return
		}
		res, err := eng.Transcribe(r.Context(), pcm, req)
		if err != nil {
			internalServerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})))
	mux.Handle("/v1/audio/transcriptions", requireBearer(cfg.Token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		// Minimal JSON-compatible shim. The OpenAI multipart form upload is not
		// implemented here; the conformance and fakeplugin paths only need a JSON
		// body that carries the same audio URL through to /transcribe.
		var payload struct {
			AudioURL      string          `json:"audio_url"`
			InputAudioURL string          `json:"input_audio_url"`
			URL           string          `json:"url"`
			Config        json.RawMessage `json:"config"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			badRequest(w, err)
			return
		}
		audioURL := payload.AudioURL
		if audioURL == "" {
			audioURL = payload.InputAudioURL
		}
		if audioURL == "" {
			audioURL = payload.URL
		}
		if audioURL == "" {
			badRequest(w, errors.New("audio_url is required"))
			return
		}
		if len(payload.Config) == 0 {
			payload.Config = json.RawMessage(`{}`)
		}
		res, err := handleTranscribeLike(r.Context(), eng, TranscribeRequest{
			AudioURL: audioURL,
			Config:   payload.Config,
		})
		if err != nil {
			if httpErr, ok := err.(httpStatusError); ok {
				writeError(w, httpErr.status, httpErr.err)
				return
			}
			internalServerError(w, err)
			return
		}
		text := segmentsText(res.Segments)
		writeJSON(w, http.StatusOK, map[string]string{"text": text})
	})))
	return mux
}

func AgentHandler(cfg Config, eng Agent) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/status", handleStatus)
	mux.Handle("/info", requireBearer(cfg.Token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, Info{
			Name:         cfg.Name,
			Version:      cfg.Version,
			PluginAPI:    1,
			Kind:         "agent",
			ConfigSchema: infoConfigSchema(cfg.ConfigSchema),
		})
	})))
	mux.Handle("/generate", requireBearer(cfg.Token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var req GenerateRequest
		if err := decodeJSON(r, &req); err != nil {
			badRequest(w, err)
			return
		}
		if err := validateGenerateRequest(req); err != nil {
			badRequest(w, err)
			return
		}
		res, err := eng.Generate(r.Context(), req)
		if err != nil {
			internalServerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})))
	return mux
}

type httpStatusError struct {
	status int
	err    error
}

func (e httpStatusError) Error() string { return e.err.Error() }

func handleTranscribeLike(ctx context.Context, eng Transcriber, req TranscribeRequest) (TranscribeResult, error) {
	if err := validateTranscribeRequest(req); err != nil {
		return TranscribeResult{}, httpStatusError{status: http.StatusBadRequest, err: err}
	}
	var mt struct {
		Multitrack bool `json:"multitrack"`
	}
	_ = json.Unmarshal(req.Config, &mt)
	if mt.Multitrack {
		res, err := runMultitrack(ctx, req.AudioURL, eng, req)
		if err != nil {
			return TranscribeResult{}, httpStatusError{status: http.StatusBadRequest, err: err}
		}
		return res, nil
	}
	pcm, err := DecodePCM(ctx, req.AudioURL)
	if err != nil {
		return TranscribeResult{}, httpStatusError{status: http.StatusBadRequest, err: err}
	}
	return eng.Transcribe(ctx, pcm, req)
}

func validateTranscribeRequest(req TranscribeRequest) error {
	if strings.TrimSpace(req.AudioURL) == "" {
		return errors.New("audio_url is required")
	}
	if len(req.Config) == 0 || string(req.Config) == "null" {
		return errors.New("config is required")
	}
	return nil
}

func validateGenerateRequest(req GenerateRequest) error {
	if req.Transcript == nil {
		return errors.New("transcript is required")
	}
	if len(req.Config) == 0 || string(req.Config) == "null" {
		return errors.New("config is required")
	}
	if req.Template.Sections == nil {
		return errors.New("template is required")
	}
	return nil
}

func segmentsText(segs []model.Segment) string {
	var b strings.Builder
	for i, seg := range segs {
		if i > 0 && b.Len() > 0 && seg.Text != "" {
			b.WriteByte(' ')
		}
		b.WriteString(seg.Text)
	}
	return strings.TrimSpace(b.String())
}

func infoConfigSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	http.Error(w, err.Error(), status)
}

func badRequest(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, err)
}

func internalServerError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, err)
}

func methodNotAllowed(w http.ResponseWriter, want string) {
	w.Header().Set("Allow", want)
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
