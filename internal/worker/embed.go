package worker

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// embedMaxRunes caps the text sent to the embedding model (it truncates anyway).
const embedMaxRunes = 8000

// runEmbed builds the note's embedding text (title + summaries + transcript + body),
// embeds it, and upserts the vector. No-op when embeddings are disabled.
func (p *Processor) runEmbed(ctx context.Context, job model.Job) (bool, error) {
	if p.embedder == nil {
		return false, nil // disabled — complete as a no-op
	}
	text, err := p.embedText(ctx, job.NoteID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(text) == "" {
		return false, nil // nothing to embed
	}
	// Prefix the assembled (already rune-capped) text with the doc task prefix
	// so asymmetric models (nomic) score document-vs-query consistently.
	vec, err := p.embedder.Embed(ctx, p.cfg.EmbeddingsDocPrefix+text)
	if err != nil {
		return true, err // transient (network) → retry
	}
	// The embedding column is unsized (mixed-dimension), so any model's vector is
	// accepted. UpsertEmbedding derives dim from len(vec) to maintain the invariant
	// that stored dim always matches actual vector length. Search compares only
	// same-model, same-dim vectors. The embedder's non-finite-component guard
	// (in Embed) still protects against vectors pgvector would reject.
	if err := p.store.UpsertEmbedding(ctx, job.NoteID, p.cfg.EmbeddingsModel, vec); err != nil {
		return true, err
	}
	return false, nil
}

// embedText assembles the text used to embed a note: its title, each summary
// section (heading + content), the transcript text, and the note body. Missing
// transcript/summaries/body are tolerated (a note may have one but not others) —
// only a real DB error on the note row itself is fatal.
func (p *Processor) embedText(ctx context.Context, noteID string) (string, error) {
	note, err := p.store.GetNoteByID(ctx, noteID)
	if err != nil {
		return "", err // a real DB error (or missing note) is fatal
	}

	var parts []string
	if t := strings.TrimSpace(note.Title); t != "" {
		parts = append(parts, t)
	}

	// Summaries: GetSummaries returns an empty slice (no error) when none exist;
	// tolerate any not-found-style error defensively.
	if sums, err := p.store.GetSummaries(ctx, noteID); err == nil {
		for _, s := range sums {
			for _, sec := range s.Sections {
				if h := strings.TrimSpace(sec.Heading); h != "" {
					parts = append(parts, h)
				}
				if c := strings.TrimSpace(sec.ContentMarkdown); c != "" {
					parts = append(parts, c)
				}
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}

	// Transcript: ErrNotFound when the note has no transcript yet — skip it.
	if tr, err := p.store.GetTranscript(ctx, noteID); err == nil {
		var segs []string
		for _, seg := range tr.Segments {
			if txt := strings.TrimSpace(seg.Text); txt != "" {
				segs = append(segs, txt)
			}
		}
		if len(segs) > 0 {
			parts = append(parts, strings.Join(segs, " "))
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}

	// Body: ErrNotFound when there's no body — skip it.
	if body, err := p.store.NoteBody(ctx, noteID); err == nil {
		if b := strings.TrimSpace(body); b != "" {
			parts = append(parts, b)
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}

	text := strings.Join(parts, "\n")
	if r := []rune(text); len(r) > embedMaxRunes {
		text = string(r[:embedMaxRunes])
	}
	return text, nil
}

// EnqueueBackfillEmbeds enqueues embed jobs for up to limit ready notes that have
// no embedding for the given (model, dim) pair yet. Returns the count enqueued. Called once on
// startup when embeddings are on, and by `muesli reembed` after a model/dim switch.
func EnqueueBackfillEmbeds(ctx context.Context, st *store.Store, modelName string, dim, limit int) (int, error) {
	ids, err := st.NotesMissingEmbedding(ctx, modelName, dim, limit)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := st.EnqueueJob(ctx, id, model.JobEmbed, nil); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		slog.InfoContext(ctx, "embed backfill enqueued", "count", len(ids), "model", modelName, "dim", dim)
	} else {
		slog.DebugContext(ctx, "embed backfill: nothing to enqueue", "count", len(ids), "model", modelName, "dim", dim)
	}
	return len(ids), nil
}
