package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestHandleRetranscribeRejectsMalformedID(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/notes/not-a-uuid/retranscribe", strings.NewReader(`{}`))

	srv.handleRetranscribe(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
}

func TestDecodeOptionalJSONBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr bool
		want    retranscribeRequest
	}{
		{name: "empty body", body: "", want: retranscribeRequest{}},
		{name: "empty object", body: `{}`, want: retranscribeRequest{}},
		{name: "model only", body: `{"model":"large-v3"}`, want: retranscribeRequest{Model: "large-v3"}},
		{name: "language only", body: `{"language":"fr"}`, want: retranscribeRequest{Language: "fr"}},
		{name: "both fields", body: `{"model":"large-v3","language":"en"}`, want: retranscribeRequest{Model: "large-v3", Language: "en"}},
		{name: "invalid json", body: `{`, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got retranscribeRequest
			err := decodeOptionalJSONBody(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)), &got)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeOptionalJSONBody: %v", err)
			}
			if got != tc.want {
				t.Fatalf("decoded body = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRetranscribeConflictReason(t *testing.T) {
	t.Parallel()

	base := model.Note{
		Status:            model.NoteReady,
		AudioObjectKey:    "notes/1/audio/a.webm",
		RetentionState:    "kept",
		PartialTranscript: false,
	}

	tests := []struct {
		name   string
		note   model.Note
		reason string
		want   bool
	}{
		{name: "ready with audio", note: base, want: false},
		{name: "failed with audio", note: func() model.Note { n := base; n.Status = model.NoteFailed; return n }(), want: false},
		{name: "no audio key", note: func() model.Note { n := base; n.AudioObjectKey = ""; return n }(), reason: "no stored audio to retranscribe", want: true},
		{name: "discarded retention", note: func() model.Note { n := base; n.RetentionState = "discarded"; return n }(), reason: "no stored audio to retranscribe", want: true},
		{name: "uploaded state", note: func() model.Note { n := base; n.Status = model.NoteUploaded; return n }(), reason: "note is not ready to retranscribe", want: true},
		{name: "transcribing state", note: func() model.Note { n := base; n.Status = model.NoteTranscribing; return n }(), reason: "note is not ready to retranscribe", want: true},
		{name: "summarizing state", note: func() model.Note { n := base; n.Status = model.NoteSummarizing; return n }(), reason: "note is not ready to retranscribe", want: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reason, got := retranscribeConflictReason(tc.note)
			if got != tc.want {
				t.Fatalf("got conflict=%v, want %v", got, tc.want)
			}
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
		})
	}
}

func TestBuildTranscribeJobPayload(t *testing.T) {
	t.Parallel()

	payload, err := buildTranscribeJobPayload("notes/1/audio/a.webm", "", "", 0)
	if err != nil {
		t.Fatalf("buildTranscribeJobPayload: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(got) != 2 || got["audio_key"] != "notes/1/audio/a.webm" || got["expected_generation"] != float64(0) {
		t.Fatalf("payload = %#v, want only audio_key and expected_generation", got)
	}

	payload, err = buildTranscribeJobPayload("notes/1/audio/a.webm", "large-v3", "fr", 3)
	if err != nil {
		t.Fatalf("buildTranscribeJobPayload: %v", err)
	}
	got = map[string]any{}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["audio_key"] != "notes/1/audio/a.webm" || got["model"] != "large-v3" || got["language"] != "fr" || got["expected_generation"] != float64(3) {
		t.Fatalf("payload = %#v, want audio_key/model/language/expected_generation", got)
	}
}
