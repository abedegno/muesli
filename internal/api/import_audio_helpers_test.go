package api

import (
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
)

func TestAllowedAudioContentType(t *testing.T) {
	t.Parallel()

	if got, ok := allowedAudioContentType("audio/webm", nil); !ok || got != "audio/webm" {
		t.Fatalf("audio/webm => %q %v, want audio/webm true", got, ok)
	}
	if _, ok := allowedAudioContentType("audio/webm; codecs=opus", storage.DefaultAllowedContentTypes); !ok {
		t.Fatal("content-type with parameters should be accepted")
	}
	if _, ok := allowedAudioContentType("text/plain", storage.DefaultAllowedContentTypes); ok {
		t.Fatal("text/plain should be rejected")
	}
	if _, ok := allowedAudioContentType("", storage.DefaultAllowedContentTypes); ok {
		t.Fatal("missing content type should be rejected")
	}
}

func TestUploadWithinLimit(t *testing.T) {
	t.Parallel()

	if !uploadWithinLimit(100, 100) {
		t.Fatal("equal size should be allowed")
	}
	if uploadWithinLimit(101, 100) {
		t.Fatal("oversize should be rejected")
	}
}

func TestFirstAudioMatch(t *testing.T) {
	t.Parallel()

	notes := []model.Note{
		{ID: "one", Title: "First", Status: model.NoteUploaded, CreatedAt: time.Unix(10, 0)},
		{ID: "two", Title: "Second", Status: model.NoteReady, CreatedAt: time.Unix(20, 0)},
	}
	match, ok := firstAudioMatch(notes)
	if !ok {
		t.Fatal("expected a match")
	}
	if match.NoteID != "one" || match.Title != "First" || match.Status != model.NoteUploaded || !match.CreatedAt.Equal(time.Unix(10, 0)) {
		t.Fatalf("unexpected match %+v", match)
	}
	if _, ok := firstAudioMatch(nil); ok {
		t.Fatal("nil slice should not match")
	}
}

func TestTitleFromFilename(t *testing.T) {
	t.Parallel()

	if got := titleFromFilename("team-sync.webm"); got != "team-sync" {
		t.Fatalf("titleFromFilename = %q, want %q", got, "team-sync")
	}
	if got := titleFromFilename(""); got != "Imported audio" {
		t.Fatalf("empty filename => %q, want Imported audio", got)
	}
}
