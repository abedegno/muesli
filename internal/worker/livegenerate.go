package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
)

// liveJobClock is injected so worker tests can control "now" deterministically.
// Production leaves it as time.Now.
var liveJobClock = time.Now

// runLiveGenerate executes one live_generate job (issue #764), following the
// same (retryable, error) contract as runSummarize/runPreGenerate: the
// caller settles the job row and, on terminal failure, calls
// handleLiveGenerateTerminalFailure.
//
// It first performs the live-specific half of claiming: rechecking current
// stream identity, active-job identity, and full first-eight eligibility,
// and capturing target_revision exactly once (see
// store.ClaimLiveGenerateJobTx). An invalid claim is a clean no-op -- not a
// job failure -- mirroring runPreGenerate's ineligibility handling. Only a
// valid claim ever reads a transcript prefix or calls the generalized
// executor, and it reads EXACTLY the prefix named by the fixed
// target_revision, regardless of any later growth.
func (p *Processor) runLiveGenerate(ctx context.Context, job model.Job) (bool, error) {
	if job.TemplateID == "" || job.StreamID == "" || job.LiveOutputID == "" {
		return false, errors.New("invalid live_generate job: missing template/stream/live_output target")
	}

	claim, err := p.store.ClaimLiveGenerateJobTx(ctx, job, liveJobClock())
	if err != nil {
		return true, err
	}
	if !claim.Valid {
		slog.InfoContext(ctx, "live_generate: no longer eligible at claim, skipping", "job_id", job.ID,
			"note_id", job.NoteID, "template_id", job.TemplateID, "stream_id", job.StreamID)
		return false, nil
	}

	tmpl, err := p.store.GetTemplate(ctx, claim.Output.OwnerID, claim.Output.TemplateID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil // template gone since claim; next completion fence would have caught it anyway
	}
	if err != nil {
		return true, err
	}

	plug, err := p.resolveDefaultAgent(ctx)
	if err != nil {
		return false, err // ErrPluginNotConfigured -> agent_unavailable, terminal (see classifyLiveError)
	}

	segments, err := p.store.LiveTranscriptPrefix(ctx, claim.TranscriptID, claim.TargetRevision)
	if err != nil {
		return true, err
	}
	body, err := p.store.NoteBody(ctx, claim.Output.NoteID)
	if err != nil {
		return true, err
	}
	aliasMap, err := p.store.SpeakerAliasMap(ctx, claim.Output.OwnerID, claim.Output.NoteID)
	if err != nil {
		return true, err
	}
	aliasedSegments, notesMarkdown := applySpeakerAliases(segments, body, aliasMap)

	client := plugin.New(plug.EndpointURL, plug.Token)
	genReq := plugin.GenerateRequest{
		Transcript:    aliasedSegments,
		NotesMarkdown: notesMarkdown,
		Template:      plugin.TemplatePayload{Sections: tmpl.Sections},
		Config:        plug.Config,
		Source: &plugin.GenerateSource{
			Kind: plugin.GenerateSourceTranscript,
			Transcript: &plugin.TranscriptSource{
				StreamID:       claim.Output.StreamID,
				TargetRevision: claim.TargetRevision,
			},
		},
	}
	applyTemplateOverrides(&genReq, tmpl)

	resp, err := client.Generate(ctx, genReq)
	if err != nil {
		return isRetryable(err), err
	}

	published, err := p.store.CompleteLiveGenerateSuccessTx(ctx, job.ID, claim.TargetRevision, plug.Name, resp.Model, resp.Summary.Sections, liveJobClock())
	if err != nil {
		return true, err
	}
	if !published {
		slog.InfoContext(ctx, "live_generate: publish discarded, ineligible after claim", "job_id", job.ID,
			"note_id", job.NoteID, "template_id", job.TemplateID)
	}
	return false, nil
}

// classifyLiveError maps a live_generate failure to one of the safe,
// client-visible error codes (issue #764) -- never raw provider text or
// credentials.
func classifyLiveError(err error) string {
	if errors.Is(err, ErrPluginNotConfigured) {
		return model.LiveErrorAgentUnavailable
	}
	var re *plugin.ResponseError
	if errors.As(err, &re) {
		return model.LiveErrorInvalidOutput
	}
	return model.LiveErrorProviderFailed
}

// handleLiveGenerateTerminalFailure publishes a live_generate job's terminal
// failure once its attempts are exhausted (or the failure was
// non-retryable), fenced the same way successful completion is (see
// store.CompleteLiveGenerateFailureTx): eligibility changed after claim
// discards the failure state entirely rather than publishing it.
//
// target_revision is read from the job's own row rather than recomputed --
// it was captured once at first claim and must never be recomputed here.
func (p *Processor) handleLiveGenerateTerminalFailure(ctx context.Context, job model.Job, terminalErr error) {
	if job.LiveOutputID == "" {
		return
	}
	target := 0
	if job.TargetRevision != nil {
		target = *job.TargetRevision
	}
	code := model.LiveErrorProviderFailed
	if terminalErr != nil {
		code = classifyLiveError(terminalErr)
	}
	if _, err := p.store.CompleteLiveGenerateFailureTx(ctx, job.ID, target, code, liveJobClock()); err != nil {
		slog.ErrorContext(ctx, "terminal live_generate: publish failure", "error", err, "job_id", job.ID, "note_id", job.NoteID)
	}
}
