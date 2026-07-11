package worker_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/worker"
)

// fakeEmbedder records the last text it was asked to embed and returns a fixed
// 768-dim vector, so tests can assert on both the input text and that the vector
// was upserted (by searching for it).
type fakeEmbedder struct {
	lastText string
	calls    int
	vec      []float32
}

func newFakeEmbedder() *fakeEmbedder {
	vec := make([]float32, embed.Dim)
	for i := range vec {
		vec[i] = 0.123 // unit-ish; only the upsert/search round-trip matters
	}
	return &fakeEmbedder{vec: vec}
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.lastText = text
	f.calls++
	return f.vec, nil
}

func (f *fakeEmbedder) Dim() int { return embed.Dim }

// embedProcessor builds a Processor wired to fake (the embedder) over the same
// store/crypto/storage/config the standard pipeline fixture uses. It returns the
// processor and the store so a test can enqueue + drain embed jobs.
func embedProcessor(t *testing.T, st *store.Store, emb embed.Embedder) *worker.Processor {
	t.Helper()
	cr := testCrypto(t)
	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	return worker.NewProcessor(st, cr, prov, config.Config{EmbeddingsDocPrefix: "search_document: ", EmbeddingsModel: "test-model"}, emb)
}

// TestRunEmbedUpsertsVector drives a ready note (with transcript + summaries)
// through a JobEmbed against a fake embedder and asserts the fake saw the title
// and the resulting vector is searchable.
func TestRunEmbedUpsertsVector(t *testing.T) {
	ctx := context.Background()
	// Build a fully-ready note via the standard pipeline (nil embedder, so no
	// embed job is auto-enqueued — we add one ourselves below).
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	drain(t, proc, st)

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("setup: note status = %q, want ready", n.Status)
	}
	ownerID := n.OwnerID

	fake := newFakeEmbedder()
	eproc := embedProcessor(t, st, fake)

	if _, err := st.EnqueueJob(ctx, noteID, model.JobEmbed, nil); err != nil {
		t.Fatalf("enqueue embed: %v", err)
	}
	drain(t, eproc, st)

	if fake.calls != 1 {
		t.Fatalf("fake embedder called %d times, want 1", fake.calls)
	}
	if !strings.HasPrefix(fake.lastText, "search_document: ") {
		t.Fatalf("embed text %q does not start with doc prefix %q", fake.lastText, "search_document: ")
	}
	if !strings.Contains(fake.lastText, n.Title) {
		t.Fatalf("embed text %q does not contain note title %q", fake.lastText, n.Title)
	}

	res, err := st.SearchEmbeddings(ctx, ownerID, "test-model", fake.vec, 768, 5)
	if err != nil {
		t.Fatalf("SearchEmbeddings: %v", err)
	}
	var found bool
	for _, r := range res {
		if r.ID == noteID {
			found = true
		}
	}
	if !found {
		t.Fatalf("SearchEmbeddings did not return embedded note %s (got %+v)", noteID, res)
	}
}

// offDimEmbedder returns a non-768-dim vector, simulating a model with a
// different dimensionality than nomic. The column is now unsized, so this must
// store successfully rather than fail.
type offDimEmbedder struct {
	calls int
	vec   []float32
}

func newOffDimEmbedder() *offDimEmbedder {
	vec := make([]float32, 384) // 384 != 768
	vec[0] = 1
	return &offDimEmbedder{vec: vec}
}

func (o *offDimEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	o.calls++
	return o.vec, nil
}
func (o *offDimEmbedder) Dim() int { return 384 }

// TestRunEmbedStoresOffDimensionVector asserts that an off-768 (here 384-dim)
// vector is stored successfully — wrong-dim is now valid behaviour because the
// column is unsized and search filters by model. The note is no longer "missing"
// for the configured model, and a matching-dim search returns it.
func TestRunEmbedStoresOffDimensionVector(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	drain(t, proc, st)

	n, _ := st.GetNoteByID(ctx, noteID)
	ownerID := n.OwnerID

	off := newOffDimEmbedder()
	eproc := embedProcessor(t, st, off)
	if _, err := st.EnqueueJob(ctx, noteID, model.JobEmbed, nil); err != nil {
		t.Fatalf("enqueue embed: %v", err)
	}
	drain(t, eproc, st)

	if off.calls != 1 {
		t.Fatalf("embedder called %d times, want 1", off.calls)
	}
	// The embedding WAS written under the configured model at dim=384 → no longer missing at that dim.
	ids, err := st.NotesMissingEmbedding(ctx, "test-model", 384, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == noteID {
			t.Fatal("off-dimension embed should have stored an embedding (note still missing)")
		}
	}
	// And a matching-dim search under the same model returns it.
	res, err := st.SearchEmbeddings(ctx, ownerID, "test-model", off.vec, 384, 5)
	if err != nil {
		t.Fatalf("SearchEmbeddings: %v", err)
	}
	var found bool
	for _, r := range res {
		if r.ID == noteID {
			found = true
		}
	}
	if !found {
		t.Fatalf("off-dimension embedding not searchable; got %+v", res)
	}
}

// TestNilEmbedderEnqueuesNoEmbedJob asserts the default (nil-embedder) pipeline
// reaches ready without writing any embedding — the note remains "missing".
func TestNilEmbedderEnqueuesNoEmbedJob(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	drain(t, proc, st)

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}

	// No embed job should have been enqueued, so there's nothing left to claim.
	if job, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil {
		t.Fatalf("claim: %v", err)
	} else if ok {
		t.Fatalf("unexpected leftover job after drain: %+v", job)
	}

	// And no embedding row exists for the note (it stays "missing").
	ids, err := st.NotesMissingEmbedding(ctx, "test-model", 768, 100)
	if err != nil {
		t.Fatalf("NotesMissingEmbedding: %v", err)
	}
	var present bool
	for _, id := range ids {
		if id == noteID {
			present = true
		}
	}
	if !present {
		t.Fatalf("note %s should be missing an embedding (nil embedder writes none)", noteID)
	}
}

// TestEnqueueBackfillEmbedsLimitCap verifies that when fewer notes are requested
// than are available, the returned count equals the limit (not the total available).
func TestEnqueueBackfillEmbedsLimitCap(t *testing.T) {
	ctx := context.Background()
	// Build three ready notes via the pipeline fixture for the first one, then
	// manually create two more and drive them to ready.
	proc, st, note1, _, _ := pipelineFixture(t, "keep")
	drain(t, proc, st)

	owner1, _ := st.GetNoteByID(ctx, note1)

	for i, title := range []string{"Second", "Third"} {
		n, err := st.CreateNote(ctx, owner1.OwnerID, title)
		if err != nil {
			t.Fatalf("CreateNote #%d: %v", i+2, err)
		}
		key := "notes/" + n.ID + "/audio/a.webm"
		if err := st.SetNoteAudio(ctx, owner1.OwnerID, n.ID, key); err != nil {
			t.Fatalf("SetNoteAudio #%d: %v", i+2, err)
		}
		if _, err := st.EnqueueJob(ctx, n.ID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+key+`"}`)); err != nil {
			t.Fatalf("enqueue transcribe #%d: %v", i+2, err)
		}
		drain(t, proc, st)
	}

	// 3 notes available, limit to 2 — should enqueue exactly 2.
	n, err := worker.EnqueueBackfillEmbeds(ctx, st, "test-model", 768, 2)
	if err != nil {
		t.Fatalf("EnqueueBackfillEmbeds: %v", err)
	}
	if n != 2 {
		t.Fatalf("backfill with limit=2 enqueued %d, want 2", n)
	}

	// Drain the queued embed jobs and count them.
	embedJobs := 0
	for i := 0; i < 10; i++ {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			break
		}
		if job.Type == model.JobEmbed {
			embedJobs++
		}
	}
	if embedJobs != 2 {
		t.Fatalf("claimed %d embed jobs, want 2", embedJobs)
	}
}

// TestEnqueueBackfillEmbeds enqueues embed jobs for ready notes lacking an
// embedding and confirms the count and the queued job types.
func TestEnqueueBackfillEmbeds(t *testing.T) {
	ctx := context.Background()
	// First ready note via the standard fixture + drain.
	proc, st, note1, _, _ := pipelineFixture(t, "keep")
	drain(t, proc, st)

	// Second ready note: reuse the same store/owner. Create + drive to ready by
	// enqueuing a transcribe job against the same default plugins.
	owner1, _ := st.GetNoteByID(ctx, note1)
	n2, err := st.CreateNote(ctx, owner1.OwnerID, "Second")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	// Attach audio so the transcribe job has something to fetch (presigned only).
	key := "notes/" + n2.ID + "/audio/a.webm"
	if err := st.SetNoteAudio(ctx, owner1.OwnerID, n2.ID, key); err != nil {
		t.Fatalf("SetNoteAudio: %v", err)
	}
	if _, err := st.EnqueueJob(ctx, n2.ID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+key+`"}`)); err != nil {
		t.Fatalf("enqueue transcribe: %v", err)
	}
	drain(t, proc, st)

	if got, _ := st.GetNoteByID(ctx, n2.ID); got.Status != model.NoteReady {
		t.Fatalf("second note status = %q, want ready", got.Status)
	}

	// Both ready notes have no embedding → backfill should enqueue 2 embed jobs.
	n, err := worker.EnqueueBackfillEmbeds(ctx, st, "test-model", 768, 10)
	if err != nil {
		t.Fatalf("EnqueueBackfillEmbeds: %v", err)
	}
	if n != 2 {
		t.Fatalf("backfill enqueued %d, want 2", n)
	}

	// Claim and count the enqueued embed jobs.
	embedJobs := 0
	for i := 0; i < 10; i++ {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			break
		}
		if job.Type == model.JobEmbed {
			embedJobs++
		}
	}
	if embedJobs != 2 {
		t.Fatalf("claimed %d embed jobs, want 2", embedJobs)
	}
}
