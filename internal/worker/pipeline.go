// Package worker runs the Muesli processing pipeline: a pool of goroutines that
// claim leased jobs and drive notes from uploaded → transcribing → summarizing
// → ready, calling external transcriber/agent plugins over HTTP/JSON.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/abedegno/muesli/internal/actionitems"
	"github.com/abedegno/muesli/internal/audiohash"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
)

// downloadTTL bounds how long the signed audio URL handed to a plugin is valid.
const downloadTTL = 30 * time.Minute

// hashNormalizedTimeout bounds the ffmpeg normalization step so a stalled child
// process cannot hold the worker indefinitely.
const hashNormalizedTimeout = 2 * time.Minute

// ErrPluginNotConfigured marks a missing required default plugin as a terminal
// operator configuration issue rather than an infrastructure failure.
var ErrPluginNotConfigured = errors.New("no default plugin configured")

// Processor executes one job at a time. It is safe for concurrent use across
// goroutines because all state lives in the store/storage it wraps.
type Processor struct {
	store                *store.Store
	crypto               *crypto.Crypto
	storage              storage.Provider
	cfg                  config.Config
	embedder             embed.Embedder
	actionItemsExtractor ActionItemsExtractor
}

// ActionItemsExtractor extracts structured action items and decisions from a
// finished note snapshot.
type ActionItemsExtractor func(context.Context, actionitems.Input) (actionitems.Result, error)

// NewProcessor builds a Processor from its collaborators. emb may be nil, which
// disables embedding generation (the system degrades to lexical-only search).
func NewProcessor(st *store.Store, cr *crypto.Crypto, prov storage.Provider, cfg config.Config, emb embed.Embedder) *Processor {
	return &Processor{store: st, crypto: cr, storage: prov, cfg: cfg, embedder: emb}
}

// SetActionItemsExtractor wires an optional action-items extraction hook.
func (p *Processor) SetActionItemsExtractor(extractor ActionItemsExtractor) {
	p.actionItemsExtractor = extractor
}

// Process dispatches a claimed job, recording completion or failure. It never
// panics out to the caller; plugin/transport errors become job failures.
//
// Ordering matters: the worker must settle this job's row (CompleteJob/FailJob)
// BEFORE running any readiness/finalization check. CountActiveSummarizeJobs
// counts status IN ('pending','running'); if FinalizeNote ran while this job was
// still 'running' the note could get stuck in 'summarizing' forever. So we settle
// first, then decide note-level outcome from the now-updated job rows.
func (p *Processor) Process(ctx context.Context, job model.Job) {
	// Skip work for a note that was trashed after the job was enqueued: complete the
	// job without acting on the (now hidden) note so it isn't retried indefinitely.
	if trashed, terr := p.store.NoteIsTrashed(ctx, job.NoteID); terr == nil && trashed {
		slog.InfoContext(ctx, "job skipped: note is trashed", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
		if cerr := p.store.CompleteJob(ctx, job.ID); cerr != nil {
			slog.WarnContext(ctx, "failed to mark skipped job done", "error", cerr, "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
		}
		return
	}

	var err error
	var retryable bool
	switch job.Type {
	case model.JobTranscribe:
		retryable, err = p.runTranscribe(ctx, job)
	case model.JobSummarize:
		retryable, err = p.runSummarize(ctx, job)
	case model.JobEmbed:
		retryable, err = p.runEmbed(ctx, job)
	default:
		err = errors.New("unknown job type: " + job.Type)
	}

	if err != nil {
		if errors.Is(err, ErrPluginNotConfigured) {
			slog.WarnContext(ctx, "job skipped: required plugin not configured", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID, "retryable", retryable, "error", err)
		} else {
			slog.ErrorContext(ctx, "job failed", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID, "retryable", retryable, "error", err)
		}
		// Settle the job row FIRST so terminal-vs-retry is reflected in the queue
		// before any readiness check reads it.
		if ferr := p.store.FailJob(ctx, job.ID, err.Error(), retryable); ferr != nil {
			slog.ErrorContext(ctx, "failed to record job failure", "error", ferr, "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
			return
		}
		// A failure is terminal when it is non-retryable or has exhausted attempts.
		terminal := !retryable || job.Attempts >= store.MaxJobAttempts
		if !terminal {
			return // will be retried by another claim
		}
		p.handleTerminalFailure(ctx, job)
		return
	}
	if cerr := p.store.CompleteJob(ctx, job.ID); cerr != nil {
		slog.WarnContext(ctx, "failed to mark job done", "error", cerr, "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
		return
	}
	// Job row is now 'done'; safe to check note readiness.
	if job.Type == model.JobSummarize {
		p.FinalizeNote(ctx, job.NoteID)
	}
}

// handleTerminalFailure applies note-level effects once a job has terminally
// failed and its row is already settled (so FinalizeNote sees the true count).
//
//   - transcribe: no transcript will ever exist → the note is failed.
//   - summarize: this one panel is failed, but the note can still be ready once
//     the remaining summarize jobs settle (partial success).
func (p *Processor) handleTerminalFailure(ctx context.Context, job model.Job) {
	switch job.Type {
	case model.JobTranscribe:
		if err := p.store.SetNoteStatus(ctx, job.NoteID, model.NoteFailed); err != nil {
			slog.ErrorContext(ctx, "terminal transcribe: set note failed", "error", err, "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
		}
	case model.JobSummarize:
		var pl summarizePayload
		if json.Unmarshal(job.Payload, &pl) == nil && pl.SummaryID != "" {
			if err := p.store.FailSummary(ctx, pl.SummaryID); err != nil {
				slog.ErrorContext(ctx, "terminal summarize: mark summary failed", "error", err, "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
			}
		}
		p.FinalizeNote(ctx, job.NoteID)
	}
}

type transcribePayload struct {
	AudioKey string `json:"audio_key"`
	Model    string `json:"model,omitempty"`
	Language string `json:"language,omitempty"`
	// ExpectedGeneration is the transcript generation observed when this job was
	// enqueued; 0 when the note had no transcript. SaveTranscript rejects the
	// job if the transcript has moved on, so a stale job cannot overwrite a
	// newer result.
	ExpectedGeneration int `json:"expected_generation"`
	// ClaimedPriorStatus is set by runTranscribe itself (never by an enqueue
	// site) the first time this job claims the note, and persisted back onto
	// the job row. A retry of the SAME job re-claims the note it already holds
	// — ClaimNoteForTranscription then can't hand back a fresh "prior status"
	// (the note's status column already reads "transcribing", overwritten by
	// this job's own first claim) — so this field is what lets the retry still
	// know what to restore on release, instead of losing it.
	ClaimedPriorStatus string `json:"claimed_prior_status,omitempty"`
}

// mergeModelOverride returns a per-call copy of baseConfig with its "model"
// key set to modelOverride, without mutating the stored plugin config. The
// whisper transcriber reads the model override from config.model, not options.
func mergeModelOverride(baseConfig json.RawMessage, modelOverride string) (json.RawMessage, error) {
	if modelOverride == "" {
		return baseConfig, nil
	}
	m := map[string]any{}
	if len(baseConfig) > 0 {
		if err := json.Unmarshal(baseConfig, &m); err != nil {
			return nil, err
		}
	}
	m["model"] = modelOverride
	return json.Marshal(m)
}

func buildTranscribeRequest(signedURL string, plug model.Plugin, cfg config.Config, pl transcribePayload) (plugin.TranscribeRequest, error) {
	req := plugin.TranscribeRequest{
		AudioURL:     signedURL,
		LanguageHint: cfg.TranscribeLanguage,
		Config:       plug.Config,
	}
	if pl.Language != "" {
		req.LanguageHint = pl.Language
	}
	if pl.Model != "" {
		mergedConfig, err := mergeModelOverride(plug.Config, pl.Model)
		if err != nil {
			return plugin.TranscribeRequest{}, err
		}
		req.Config = mergedConfig
	}
	if cfg.Diarization {
		req.Options = json.RawMessage(`{"diarize":true}`)
	}
	return req, nil
}

// persistTranscribePayload marshals pl and writes it back onto job's row, so a
// retry of this same job (a fresh decode of job.Payload on the next
// runTranscribe invocation) sees whatever this attempt learned, rather than
// the stale snapshot captured at enqueue time. The error is returned, not
// logged-and-swallowed: a payload write that silently fails here would make
// the NEXT attempt misjudge itself as stale, or lose the status its own claim
// displaced, and quietly stop making progress instead of actually retrying.
func (p *Processor) persistTranscribePayload(ctx context.Context, jobID string, pl transcribePayload) error {
	updated, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("marshal transcribe payload: %w", err)
	}
	if err := p.store.UpdateJobPayload(ctx, jobID, updated); err != nil {
		return fmt.Errorf("persist transcribe payload: %w", err)
	}
	return nil
}

// runTranscribe returns (retryable, error). On success it stores the transcript,
// applies retention, fans out summarize jobs, and advances the note.
func (p *Processor) runTranscribe(ctx context.Context, job model.Job) (bool, error) {
	// Decode the payload and check the expected generation BEFORE anything is
	// mutated, so a stale job (enqueued against a generation a newer job has
	// since replaced) touches nothing. This must happen ahead of the claim
	// below, not just ahead of SaveTranscript. A malformed payload fails the
	// decode outright rather than silently falling back to its zero value,
	// which would otherwise pass the generation check on a note with no
	// transcript (0 == 0) and let a corrupt job proceed with empty fields.
	var pl transcribePayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return false, fmt.Errorf("invalid transcribe payload: %w", err)
	}
	currentGeneration, err := p.store.CurrentTranscriptGeneration(ctx, job.NoteID)
	if err != nil {
		return true, err
	}
	if currentGeneration != pl.ExpectedGeneration {
		slog.InfoContext(ctx, "stale transcribe job discarded before claim: transcript generation has moved on",
			"job_id", job.ID, "note_id", job.NoteID, "expected_generation", pl.ExpectedGeneration, "current_generation", currentGeneration)
		return false, nil
	}

	// Claim the note (recording this job's id) rather than a bare status write,
	// so a late mismatch — detected only after SaveTranscript, below — can
	// restore precisely what this job displaced instead of guessing.
	//
	// freshPrior is "" when this call re-claimed a note THIS SAME job already
	// holds (a retry after an earlier attempt claimed, then failed before
	// saving): the note's status column already reads "transcribing" from that
	// earlier claim, so ClaimNoteForTranscription has nothing fresh to report
	// and deliberately does not overwrite status again. Use the prior status
	// this job captured (and persisted) on its ORIGINAL claim instead — trusting
	// a fresh "transcribing" here would make a later release restore
	// "transcribing" over whatever the note's real prior status was (e.g.
	// "ready"), losing it permanently.
	freshPrior, err := p.store.ClaimNoteForTranscription(ctx, job.NoteID, job.ID)
	if err != nil {
		return true, err
	}
	priorStatus := pl.ClaimedPriorStatus
	if freshPrior != "" {
		priorStatus = freshPrior
		pl.ClaimedPriorStatus = freshPrior
		if err := p.persistTranscribePayload(ctx, job.ID, pl); err != nil {
			return true, err
		}
	}

	note, err := p.store.GetNoteByID(ctx, job.NoteID)
	if err != nil {
		return false, err
	}
	audioKey := note.AudioObjectKey
	if pl.AudioKey != "" {
		audioKey = pl.AudioKey
	}
	if audioKey == "" {
		return false, errors.New("note has no audio object key")
	}

	plug, err := p.store.DefaultPlugin(ctx, p.crypto, model.PluginTranscriber)
	if errors.Is(err, store.ErrNotFound) {
		return false, fmt.Errorf("no default transcriber plugin configured: %w", ErrPluginNotConfigured)
	}
	if err != nil {
		return true, err
	}

	signedURL, err := p.storage.PresignDownload(audioKey, downloadTTL)
	if err != nil {
		return true, err
	}

	client := plugin.New(plug.EndpointURL, plug.Token)
	req, err := buildTranscribeRequest(signedURL, plug, p.cfg, pl)
	if err != nil {
		return true, err
	}
	resp, err := client.Transcribe(ctx, req)
	if err != nil {
		return isRetryable(err), err
	}

	saved, err := p.store.SaveTranscript(ctx, model.Transcript{
		NoteID:            job.NoteID,
		TranscriberPlugin: plug.Name,
		Model:             resp.Model,
		Segments:          resp.Segments,
	}, pl.ExpectedGeneration)
	if errors.Is(err, store.ErrGenerationMismatch) {
		// Detected late: a newer job won while this one was mid-transcription.
		// Restore whatever status this job's claim displaced. If the winner has
		// already saved, its own SaveTranscript cleared transcribing_job_id, so
		// this release matches nothing and is a harmless no-op.
		if priorStatus == "" {
			// Should not happen: this job's own claim never got its prior status
			// persisted (a crash between the claim and persistTranscribePayload
			// above, on some earlier attempt). Releasing with an empty status
			// would corrupt the note's status column, so leave the claim in
			// place instead — the note stays "transcribing" rather than wrong.
			slog.ErrorContext(ctx, "stale transcribe: no prior status to restore, leaving claim in place",
				"job_id", job.ID, "note_id", job.NoteID)
		} else if rerr := p.store.ReleaseNoteTranscriptionClaim(ctx, job.NoteID, job.ID, priorStatus); rerr != nil {
			slog.WarnContext(ctx, "stale transcribe: failed to release claim", "error", rerr, "job_id", job.ID, "note_id", job.NoteID)
		}
		slog.InfoContext(ctx, "stale transcribe job discarded after claim: transcript generation moved on while running",
			"job_id", job.ID, "note_id", job.NoteID, "expected_generation", pl.ExpectedGeneration)
		return false, nil
	}
	if err != nil {
		return true, err
	}

	// This job's own transcript is now durably saved at saved.Generation —
	// always past pl.ExpectedGeneration (SaveTranscript only succeeds when
	// saved.Generation == pl.ExpectedGeneration+1). If a later step below fails
	// retryably, Process reclaims this same job and runTranscribe runs again
	// from a fresh decode of job.Payload — so without persisting the advance
	// here, that retry would still carry the expected_generation captured at
	// enqueue time and reject its own already-saved work as stale on every
	// subsequent attempt. This only ever advances a generation this job itself
	// just produced; a different job's newer save is still caught the normal
	// way, by this job's next attempt observing a generation beyond what it
	// persisted here.
	pl.ExpectedGeneration = saved.Generation
	if err := p.persistTranscribePayload(ctx, job.ID, pl); err != nil {
		return true, err
	}

	// The audio is now confirmed to belong to the transcript this job just
	// won the right to publish — hash it after the authoritative check, not
	// before. Hashing (and this store write) earlier, ahead of SaveTranscript,
	// would let a job that turns out to be stale still overwrite the note's
	// audio_hash/normalized_audio_hash using its own (possibly superseded)
	// audio key before the generation check ever rejects it — touching the
	// note despite losing the race, which the design does not allow.
	if rawHash, normalizedHash, err := p.hashNoteAudio(ctx, audioKey); err != nil {
		slog.WarnContext(ctx, "failed to hash note audio", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID, "audio_key", audioKey, "error", err)
	} else if rawHash != "" {
		if err := p.store.SetNoteHashes(ctx, job.NoteID, rawHash, normalizedHash); err != nil {
			slog.WarnContext(ctx, "failed to persist note audio hashes", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID, "audio_key", audioKey, "error", err)
		}
	}

	// If the plugin returned a partial result, mark the note and fail
	// the job terminally (non-retryable) so handleTerminalFailure sets note→failed.
	// The user can then retry from the UI which re-runs full transcription.
	if resp.Partial {
		if perr := p.store.SetNotePartialTranscript(ctx, job.NoteID, true); perr != nil {
			slog.WarnContext(ctx, "partial: failed to set flag", "error", perr, "note_id", job.NoteID)
		}
		return false, fmt.Errorf("partial transcript: %s", resp.PartialReason)
	}

	if err := p.store.SetNotePartialTranscript(ctx, job.NoteID, false); err != nil {
		slog.WarnContext(ctx, "failed to clear partial transcript flag", "error", err, "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID)
	}

	if saved.ReviewState != model.ReviewStateCompleted {
		slog.InfoContext(ctx, "summarize held pending diarization review", "job_id", job.ID, "note_id", job.NoteID, "review_state", saved.ReviewState)
		if p.cfg.Embedded {
			// saved.Generation is this job's own just-persisted save (see the
			// comment above pl.ExpectedGeneration): the only generation this
			// auto-complete can legitimately be racing against.
			if err := p.store.SetReviewState(ctx, job.NoteID, model.ReviewStateCompleted, saved.Generation); err != nil {
				slog.ErrorContext(ctx, "embedded diarization review auto-complete failed", "error", err, "job_id", job.ID, "note_id", job.NoteID)
				return true, err
			}
		} else {
			// It is safe to discard audio here because resummarize/review release works
			// from the transcript, not the audio object.
			if p.cfg.AudioRetention == "discard" {
				if derr := p.storage.Delete(audioKey); derr != nil {
					slog.WarnContext(ctx, "retention: failed to delete audio", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID, "audio_key", audioKey, "error", derr)
				} else {
					_ = p.store.SetRetentionState(ctx, job.NoteID, "discarded")
				}
			} else {
				_ = p.store.SetRetentionState(ctx, job.NoteID, "kept")
			}
			return false, nil
		}
	}

	// Fan out one summarize job per template the note's owner sees (built-ins + their
	// own). Shared with the resummarize endpoint via the store (no drift).
	owner, err := p.store.NoteOwnerID(ctx, job.NoteID)
	if err != nil {
		return true, err
	}
	if err := p.store.DeleteNoteSummaries(ctx, owner, job.NoteID); err != nil {
		return true, err
	}
	if err := p.store.EnqueueSummarizeJobs(ctx, owner, job.NoteID); err != nil {
		return true, err
	}

	// Audio must not be deleted until the note is either parked for review or the
	// summarize jobs are durably enqueued, so no retryable path can strand a note
	// without the audio required for a retry.
	if p.cfg.AudioRetention == "discard" {
		if derr := p.storage.Delete(audioKey); derr != nil {
			slog.WarnContext(ctx, "retention: failed to delete audio", "job_id", job.ID, "job_type", job.Type, "note_id", job.NoteID, "audio_key", audioKey, "error", derr)
		} else {
			_ = p.store.SetRetentionState(ctx, job.NoteID, "discarded")
		}
	} else {
		_ = p.store.SetRetentionState(ctx, job.NoteID, "kept")
	}
	return false, nil
}

func (p *Processor) hashNoteAudio(ctx context.Context, audioKey string) (string, string, error) {
	rc, err := p.storage.Open(audioKey)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()

	tmp, err := os.CreateTemp("", "muesli-audio-*"+filepath.Ext(audioKey))
	if err != nil {
		return "", "", err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	rawHash, err := audiohash.HashRaw(io.TeeReader(rc, tmp))
	if err != nil {
		return "", "", err
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}

	hashCtx, cancel := context.WithTimeout(ctx, hashNormalizedTimeout)
	defer cancel()

	normalizedHash, _ := audiohash.HashNormalized(hashCtx, tmp.Name())
	return rawHash, normalizedHash, nil
}

type summarizePayload struct {
	TemplateID string `json:"template_id"`
	SummaryID  string `json:"summary_id"`
}

// runSummarize returns (retryable, error). On success it stores the summary;
// note readiness and the failure handling are decided by Process AFTER it settles
// this job's row (see Process), so runSummarize itself never calls FinalizeNote or
// FailSummary.
func (p *Processor) runSummarize(ctx context.Context, job model.Job) (bool, error) {
	var pl summarizePayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.SummaryID == "" || pl.TemplateID == "" {
		return false, errors.New("invalid summarize payload")
	}

	transcript, err := p.store.GetTranscript(ctx, job.NoteID)
	if err != nil {
		return false, err
	}
	body, err := p.store.NoteBody(ctx, job.NoteID)
	if err != nil {
		return true, err
	}
	noteOwner, err := p.store.NoteOwnerID(ctx, job.NoteID)
	if err != nil {
		return true, err
	}
	tmpl, err := p.store.GetTemplate(ctx, noteOwner, pl.TemplateID)
	if errors.Is(err, store.ErrNotFound) {
		return false, errors.New("template not found: " + pl.TemplateID)
	}
	if err != nil {
		return true, err
	}

	plug, err := p.store.DefaultPlugin(ctx, p.crypto, model.PluginAgent)
	if errors.Is(err, store.ErrNotFound) {
		return false, fmt.Errorf("no default agent plugin configured: %w", ErrPluginNotConfigured)
	}
	if err != nil {
		return true, err
	}

	// Substitute note-scoped speaker aliases (raw label -> user-chosen name)
	// into the segments and notes_markdown sent to the plugin only. The
	// stored transcript_segments rows and note body are never modified.
	aliasMap, err := p.store.SpeakerAliasMap(ctx, noteOwner, job.NoteID)
	if err != nil {
		return true, err
	}
	segments, notesMarkdown := applySpeakerAliases(transcript.Segments, body, aliasMap)

	client := plugin.New(plug.EndpointURL, plug.Token)
	genReq := plugin.GenerateRequest{
		Transcript:    segments,
		NotesMarkdown: notesMarkdown,
		Template:      plugin.TemplatePayload{Sections: tmpl.Sections},
		Config:        plug.Config,
	}
	// Only set the optional per-template agent overrides when the resolved
	// template actually has them; absent template values leave the request
	// fields at their zero value, preserving current behaviour (the agent
	// falls back to its own default system prompt / plugin Config).
	if tmpl.SystemPrompt != "" {
		genReq.SystemPrompt = tmpl.SystemPrompt
	}
	if tmpl.Model != "" {
		genReq.Model = tmpl.Model
	}
	if tmpl.Temperature != nil {
		genReq.Temperature = tmpl.Temperature
	}
	resp, err := client.Generate(ctx, genReq)
	if err != nil {
		// Do NOT mark the summary failed or finalize here: the job row is still
		// 'running' at this point. Process settles the job first, then (on a
		// terminal failure) marks the summary failed and re-checks readiness —
		// regardless of whether the error was retryable or attempts were exhausted.
		return isRetryable(err), err
	}

	truncated := DetectTruncation(resp.Summary.Sections, resp.Usage)
	if err := p.store.CompleteSummary(ctx, pl.SummaryID, plug.Name, resp.Model, resp.Summary.Sections, truncated); err != nil {
		return true, err
	}
	// Readiness is checked by Process AFTER it marks this job done, so the
	// just-finished job is no longer counted as active.
	return false, nil
}

// FinalizeNote sets a note to ready once it has a transcript and no summarize
// jobs remain pending/running. Idempotent. Partial success: failed summarize
// jobs do not block readiness.
//
// FinalizeNote can be invoked concurrently for the same note (e.g. two
// summarize-job completions both observing pending==0). MarkNoteReady is the
// ONLY thing that decides who "wins" the ready transition (a single atomic
// UPDATE ... WHERE status <> 'ready'), so the embed-enqueue and webhook-enqueue
// side effects below are gated on won==true and therefore fire exactly once
// per note, even under a race.
func (p *Processor) FinalizeNote(ctx context.Context, noteID string) {
	transcript, err := p.store.GetTranscript(ctx, noteID)
	if err != nil {
		return // no transcript yet → not ready
	}
	pending, err := p.store.CountActiveSummarizeJobs(ctx, noteID)
	if err != nil {
		slog.ErrorContext(ctx, "finalize: count jobs", "error", err, "note_id", noteID)
		return
	}
	if pending > 0 {
		return
	}
	won, err := p.store.MarkNoteReady(ctx, noteID)
	if err != nil {
		slog.ErrorContext(ctx, "finalize: set ready", "error", err, "note_id", noteID)
		return
	}
	if !won {
		// Someone else already won the ready transition (or this call lost a
		// genuine race). They already ran the enqueue steps below; nothing more
		// to do here.
		return
	}
	p.maybeExtractActionItems(ctx, noteID, transcript)
	// Embedding is config-gated: only enqueue when an embedder is wired in. This
	// runs once per ready transition (MarkNoteReady above just won it).
	if p.embedder != nil {
		if _, err := p.store.EnqueueJob(ctx, noteID, model.JobEmbed, nil); err != nil {
			slog.WarnContext(ctx, "finalize: enqueue embed", "error", err, "note_id", noteID)
		}
	}
	p.enqueueNoteCompletedWebhooks(ctx, noteID, transcript)
}

func (p *Processor) maybeExtractActionItems(ctx context.Context, noteID string, transcript model.Transcript) {
	if p.actionItemsExtractor == nil {
		return
	}
	sections, ok := p.readySummarySections(ctx, noteID)
	if !ok {
		return
	}
	result, err := p.actionItemsExtractor(ctx, actionitems.Input{
		Transcript: transcript.Segments,
		Summary:    sections,
	})
	if err != nil {
		slog.DebugContext(ctx, "action items extraction failed", "error", err, "note_id", noteID)
		return
	}
	slog.DebugContext(ctx, "action items extracted", "note_id", noteID, "action_items", len(result.ActionItems), "decisions", len(result.Decisions))

	ownerID, err := p.store.NoteOwnerID(ctx, noteID)
	if err != nil {
		slog.WarnContext(ctx, "action items persist: resolve note owner", "error", err, "note_id", noteID)
		return
	}

	items := make([]model.ActionItem, len(result.ActionItems))
	for i, item := range result.ActionItems {
		items[i] = model.ActionItem{
			Text:    item.Text,
			DueHint: item.DueHint,
		}
	}
	decisions := make([]model.Decision, len(result.Decisions))
	for i, decision := range result.Decisions {
		decisions[i] = model.Decision{Text: decision.Text}
	}

	if err := p.store.ReplaceActionItemsForNote(ctx, ownerID, noteID, items, decisions); err != nil {
		slog.WarnContext(ctx, "action items persist failed", "error", err, "note_id", noteID)
	}
}

func (p *Processor) readySummarySections(ctx context.Context, noteID string) ([]model.SummarySection, bool) {
	summaries, err := p.store.GetSummaries(ctx, noteID)
	if err != nil {
		slog.WarnContext(ctx, "finalize: get summaries for extraction", "error", err, "note_id", noteID)
		return nil, false
	}
	for _, sm := range summaries {
		if sm.Status == model.SummaryReady {
			sections := sm.Sections
			if sections == nil {
				sections = []model.SummarySection{}
			}
			return sections, true
		}
	}
	return nil, false
}

// webhookNotePayload is the JSON body delivered for the note.completed event
// (see enqueueNoteCompletedWebhooks). Shape:
//
//	{
//	  "event": "note.completed",
//	  "note": {"id": "...", "title": "..."},
//	  "summary": {"sections": [{"heading": "...", "content_markdown": "...", "refs": [0,1]}]},
//	  "segments": [{"start_ms": 0, "end_ms": 1500, "text": "...", "speaker": "SPEAKER_00"}]
//	}
//
// "summary" is ALWAYS present, even when the note reached ready via the
// partial-success path with zero ready summaries (failed summarize jobs do
// not block readiness — see FinalizeNote): in that case "sections" is an
// empty array ([]), never a missing key or null. When a note has more than
// one ready summary (one per template), the FIRST ready summary returned by
// GetSummaries (which orders by template name) is used — a single
// representative summary rather than an array, to keep the payload shape
// simple for consumers. "segments" is always present (possibly empty).
type webhookNotePayload struct {
	Event    string             `json:"event"`
	Note     webhookNoteInfo    `json:"note"`
	Summary  webhookSummaryInfo `json:"summary"`
	Segments []webhookSegment   `json:"segments"`
}

type webhookNoteInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type webhookSummaryInfo struct {
	Sections []model.SummarySection `json:"sections"`
}

type webhookSegment struct {
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
	Text    string `json:"text"`
	Speaker string `json:"speaker,omitempty"`
}

// enqueueNoteCompletedWebhooks builds a note.completed payload and creates one
// pending webhook_deliveries row per ENABLED webhook belonging to the note's
// owner. Zero enabled webhooks (or none configured) is a no-op: no rows
// created, no error — there is no single global webhook to fall back to,
// per-owner registration is the only source of truth. Any per-webhook or
// lookup error is logged and swallowed: a webhook enqueue failure must never
// fail note finalization (matches the embed-enqueue error-handling style
// above).
func (p *Processor) enqueueNoteCompletedWebhooks(ctx context.Context, noteID string, transcript model.Transcript) {
	note, err := p.store.GetNoteByID(ctx, noteID)
	if err != nil {
		slog.WarnContext(ctx, "finalize: webhook lookup note", "error", err, "note_id", noteID)
		return
	}
	webhooks, err := p.store.ListEnabledWebhooksForOwner(ctx, note.OwnerID)
	if err != nil {
		slog.WarnContext(ctx, "finalize: list webhooks", "error", err, "note_id", noteID)
		return
	}
	if len(webhooks) == 0 {
		return
	}

	payload := webhookNotePayload{
		Event: "note.completed",
		Note:  webhookNoteInfo{ID: note.ID, Title: note.Title},
		// Default to an empty (non-nil) slice so "summary":{"sections":[]} is
		// what a note with zero ready summaries gets — never a missing/null
		// summary key (the partial-success path can reach ready this way; see
		// FinalizeNote). Overwritten below if a ready summary is found.
		Summary: webhookSummaryInfo{Sections: []model.SummarySection{}},
	}
	if summaries, err := p.store.GetSummaries(ctx, noteID); err != nil {
		slog.WarnContext(ctx, "finalize: webhook get summaries", "error", err, "note_id", noteID)
	} else {
		for _, sm := range summaries {
			if sm.Status == model.SummaryReady {
				sections := sm.Sections
				if sections == nil {
					sections = []model.SummarySection{}
				}
				payload.Summary = webhookSummaryInfo{Sections: sections}
				break
			}
		}
	}
	payload.Segments = make([]webhookSegment, len(transcript.Segments))
	for i, seg := range transcript.Segments {
		payload.Segments[i] = webhookSegment{
			StartMS: seg.StartMS,
			EndMS:   seg.EndMS,
			Text:    seg.Text,
			Speaker: seg.Speaker,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "finalize: marshal webhook payload", "error", err, "note_id", noteID)
		return
	}
	for _, wh := range webhooks {
		if err := p.store.CreateDelivery(ctx, wh.ID, body); err != nil {
			slog.WarnContext(ctx, "finalize: enqueue webhook delivery", "error", err, "note_id", noteID, "webhook_id", wh.ID)
		}
	}
}

// isRetryable classifies a plugin error: HTTP 5xx/429 and transport errors
// (timeouts, refused connections) retry; 4xx fails fast.
func isRetryable(err error) bool {
	var re *plugin.ResponseError
	if errors.As(err, &re) {
		return false
	}
	var he *plugin.HTTPError
	if errors.As(err, &he) {
		return he.Retryable()
	}
	return true // transport-level error (timeout, connection refused) → retry
}
