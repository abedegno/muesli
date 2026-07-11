package chat_test

import (
	"context"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/chat"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

const testModel = "test-model"

// fakeEmbedder mirrors internal/api/search_test.go's fakeEmbedder: a
// fixed-vector embedder that never errors, used to exercise the semantic
// ranking path deterministically.
type fakeEmbedder struct {
	vec []float32
}

func (f fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return f.vec, nil
}

func (f fakeEmbedder) Dim() int { return len(f.vec) }

// erroringEmbedder always fails Embed, used to prove top-k degrades to
// lexical-only rather than erroring.
type erroringEmbedder struct{}

func (erroringEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, context.DeadlineExceeded
}

func (erroringEmbedder) Dim() int { return embed.Dim }

func fixedVec() []float32 {
	v := make([]float32, embed.Dim)
	for i := range v {
		v[i] = float32((i % 7) + 1)
	}
	return v
}

func newStoreAndOwner(t *testing.T) (*store.Store, string) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	u, err := st.CreateUser(context.Background(), "owner+"+t.Name()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return st, u.ID
}

func createNote(t *testing.T, st *store.Store, ownerID, title string) model.Note {
	t.Helper()
	n, err := st.CreateNote(context.Background(), ownerID, title)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

func addTranscript(t *testing.T, st *store.Store, noteID string, segs []model.Segment) model.Transcript {
	t.Helper()
	tr, err := st.SaveTranscript(context.Background(), model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments:          segs,
	})
	if err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	return tr
}

func setAlias(t *testing.T, st *store.Store, ownerID, noteID, speakerLabel, aliasName string) {
	t.Helper()
	if err := st.UpsertSpeakerAlias(context.Background(), ownerID, noteID, speakerLabel, aliasName); err != nil {
		t.Fatalf("UpsertSpeakerAlias: %v", err)
	}
}

func TestNoteTranscriptAppliesAliasesWithPreface(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	note := createNote(t, st, ownerID, "Standup")
	addTranscript(t, st, note.ID, []model.Segment{
		{StartMS: 0, EndMS: 1000, Text: "Let's get started.", Source: "whisper", Speaker: "SPEAKER_00"},
		{StartMS: 1000, EndMS: 2000, Text: "Sounds good to me.", Source: "whisper", Speaker: "SPEAKER_01"},
	})
	setAlias(t, st, ownerID, note.ID, "SPEAKER_00", "Alice")
	setAlias(t, st, ownerID, note.ID, "SPEAKER_01", "Bob")

	r := chat.NewRetriever(st, nil, config.Config{})
	got, err := r.NoteTranscript(context.Background(), ownerID, note.ID)
	if err != nil {
		t.Fatalf("NoteTranscript: %v", err)
	}

	wantPreface := "Speakers: SPEAKER_00 -> Alice, SPEAKER_01 -> Bob\n" +
		"Use the provided speaker names verbatim; do not infer, abbreviate, or merge names."
	if !strings.HasPrefix(got.Text, wantPreface+"\n\n") {
		t.Fatalf("Text = %q, want prefix %q", got.Text, wantPreface+"\n\n")
	}
	if !strings.Contains(got.Text, "Alice: Let's get started.") {
		t.Fatalf("Text = %q, want alias-substituted Alice line", got.Text)
	}
	if !strings.Contains(got.Text, "Bob: Sounds good to me.") {
		t.Fatalf("Text = %q, want alias-substituted Bob line", got.Text)
	}
	if len(got.Segments) != 2 || got.Segments[0].Speaker != "Alice" || got.Segments[1].Speaker != "Bob" {
		t.Fatalf("Segments = %+v, want alias-substituted speakers", got.Segments)
	}
}

func TestNoteTranscriptNoAliasesUnchanged(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	note := createNote(t, st, ownerID, "Standup")
	addTranscript(t, st, note.ID, []model.Segment{
		{StartMS: 0, EndMS: 1000, Text: "Let's get started.", Source: "whisper", Speaker: "SPEAKER_00"},
	})
	// No aliases set at all.

	r := chat.NewRetriever(st, nil, config.Config{})
	got, err := r.NoteTranscript(context.Background(), ownerID, note.ID)
	if err != nil {
		t.Fatalf("NoteTranscript: %v", err)
	}

	if strings.Contains(got.Text, "Speakers:") || strings.Contains(got.Text, "verbatim") {
		t.Fatalf("Text = %q, want no alias preface when no aliases apply", got.Text)
	}
	if got.Text != "SPEAKER_00: Let's get started." {
		t.Fatalf("Text = %q, want unchanged raw-speaker assembly", got.Text)
	}
	if got.Segments[0].Speaker != "SPEAKER_00" {
		t.Fatalf("Segments = %+v, want unchanged raw speaker label", got.Segments)
	}
}

func TestNoteTranscriptNonMatchingAliasUnchanged(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	note := createNote(t, st, ownerID, "Standup")
	addTranscript(t, st, note.ID, []model.Segment{
		{StartMS: 0, EndMS: 1000, Text: "Hello.", Source: "whisper", Speaker: "SPEAKER_00"},
	})
	// Alias for a speaker label that never appears in this transcript.
	setAlias(t, st, ownerID, note.ID, "SPEAKER_99", "Nobody")

	r := chat.NewRetriever(st, nil, config.Config{})
	got, err := r.NoteTranscript(context.Background(), ownerID, note.ID)
	if err != nil {
		t.Fatalf("NoteTranscript: %v", err)
	}
	if strings.Contains(got.Text, "Speakers:") {
		t.Fatalf("Text = %q, want no preface for a non-applying alias", got.Text)
	}
	if got.Text != "SPEAKER_00: Hello." {
		t.Fatalf("Text = %q, want unchanged assembly", got.Text)
	}
}

func TestNoteTranscriptNotOwnedReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	other, err := st.CreateUser(context.Background(), "other+"+t.Name()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	note := createNote(t, st, ownerID, "Owner-only note")
	addTranscript(t, st, note.ID, []model.Segment{
		{StartMS: 0, EndMS: 1000, Text: "Secret content.", Source: "whisper"},
	})

	r := chat.NewRetriever(st, nil, config.Config{})
	_, err = r.NoteTranscript(context.Background(), other.ID, note.ID)
	if err != store.ErrNotFound {
		t.Fatalf("NoteTranscript by non-owner: err = %v, want store.ErrNotFound", err)
	}
}

func TestNoteTranscriptNoTranscriptReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	note := createNote(t, st, ownerID, "No transcript yet")

	r := chat.NewRetriever(st, nil, config.Config{})
	_, err := r.NoteTranscript(context.Background(), ownerID, note.ID)
	if err != store.ErrNotFound {
		t.Fatalf("NoteTranscript with no transcript: err = %v, want store.ErrNotFound", err)
	}
}

func TestTopKRanksAndCapsWithSegmentCitations(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	ctx := context.Background()
	vec := fixedVec()

	alpha := createNote(t, st, ownerID, "Alpha rollout meeting")
	if err := st.UpsertEmbedding(ctx, alpha.ID, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding alpha: %v", err)
	}
	trAlpha := addTranscript(t, st, alpha.ID, []model.Segment{
		{StartMS: 1100, EndMS: 1600, Text: "We discussed the alpha rollout and risks.", Source: "whisper"},
	})

	beta := createNote(t, st, ownerID, "Beta planning")
	if err := st.UpsertEmbedding(ctx, beta.ID, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding beta: %v", err)
	}
	addTranscript(t, st, beta.ID, []model.Segment{
		{StartMS: 200, EndMS: 700, Text: "Unrelated beta content.", Source: "whisper"},
	})

	gamma := createNote(t, st, ownerID, "Gamma retro")
	if err := st.UpsertEmbedding(ctx, gamma.ID, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding gamma: %v", err)
	}
	addTranscript(t, st, gamma.ID, []model.Segment{
		{StartMS: 300, EndMS: 800, Text: "Unrelated gamma content.", Source: "whisper"},
	})

	r := chat.NewRetriever(st, fakeEmbedder{vec: vec}, config.Config{EmbeddingsModel: testModel, EmbeddingsMinScore: 0})
	refs, err := r.TopK(ctx, ownerID, "alpha", 2)
	if err != nil {
		t.Fatalf("TopK: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("TopK returned %d refs, want cap of 2: %+v", len(refs), refs)
	}
	if refs[0].NoteID != alpha.ID {
		t.Fatalf("TopK[0].NoteID = %q, want %q (lexical+semantic match ranks first): %+v", refs[0].NoteID, alpha.ID, refs)
	}
	if refs[0].SegmentID != trAlpha.Segments[0].ID {
		t.Fatalf("TopK[0].SegmentID = %q, want %q", refs[0].SegmentID, trAlpha.Segments[0].ID)
	}
	if refs[0].SegmentIndex != 0 {
		t.Fatalf("TopK[0].SegmentIndex = %d, want 0", refs[0].SegmentIndex)
	}
	if refs[0].StartMS != 1100 {
		t.Fatalf("TopK[0].StartMS = %d, want 1100", refs[0].StartMS)
	}
	if !strings.Contains(strings.ToLower(refs[0].Snippet), "alpha") {
		t.Fatalf("TopK[0].Snippet = %q, want it to contain the query text", refs[0].Snippet)
	}
	if refs[0].NoteTitle != alpha.Title {
		t.Fatalf("TopK[0].NoteTitle = %q, want %q", refs[0].NoteTitle, alpha.Title)
	}
}

func TestTopKFallsBackToNoteSnippetWithoutSegmentMatch(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	ctx := context.Background()
	vec := fixedVec()

	note := createNote(t, st, ownerID, "Delta review")
	if err := st.UpsertEmbedding(ctx, note.ID, testModel, vec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}
	// No transcript at all for this note: TopK must still surface it (title
	// match) but with no segment citation, falling back to note.Snippet.

	r := chat.NewRetriever(st, fakeEmbedder{vec: vec}, config.Config{EmbeddingsModel: testModel, EmbeddingsMinScore: 0})
	refs, err := r.TopK(ctx, ownerID, "delta", 5)
	if err != nil {
		t.Fatalf("TopK: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("TopK = %+v, want exactly 1 ref", refs)
	}
	if refs[0].SegmentID != "" || refs[0].SegmentIndex != -1 {
		t.Fatalf("TopK[0] = %+v, want no segment citation for a note with no transcript", refs[0])
	}
}

func TestTopKGracefulFallbackNilEmbedder(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)

	createNote(t, st, ownerID, "Lexical only match")
	createNote(t, st, ownerID, "Something else entirely")

	r := chat.NewRetriever(st, nil, config.Config{EmbeddingsModel: testModel})
	refs, err := r.TopK(context.Background(), ownerID, "lexical", 5)
	if err != nil {
		t.Fatalf("TopK with nil embedder: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("TopK with nil embedder = %+v, want 1 lexical match", refs)
	}
}

func TestTopKGracefulFallbackEmbedderErrors(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)

	createNote(t, st, ownerID, "Lexical fallback match")
	createNote(t, st, ownerID, "Something else entirely")

	r := chat.NewRetriever(st, erroringEmbedder{}, config.Config{EmbeddingsModel: testModel})
	refs, err := r.TopK(context.Background(), ownerID, "fallback", 5)
	if err != nil {
		t.Fatalf("TopK with erroring embedder: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("TopK with erroring embedder = %+v, want 1 lexical match", refs)
	}
}

func TestTopKEmptyQueryReturnsEmpty(t *testing.T) {
	t.Parallel()
	st, ownerID := newStoreAndOwner(t)
	r := chat.NewRetriever(st, nil, config.Config{})
	refs, err := r.TopK(context.Background(), ownerID, "   ", 5)
	if err != nil {
		t.Fatalf("TopK empty query: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("TopK empty query = %+v, want empty slice", refs)
	}
}
