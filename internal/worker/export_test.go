package worker

import "context"

// RecoverStartupJobsForTest exports the unexported recoverStartupJobs for
// use in integration tests within the worker_test package.
func RecoverStartupJobsForTest(ctx context.Context, store jobRecoverer) {
	recoverStartupJobs(ctx, store)
}

// SetTestHookAfterTranscriptPublished installs the hook that runs between
// runTranscribe recording its publication checkpoint and re-checking that the
// transcript it published is still current. Returns a restore func.
func SetTestHookAfterTranscriptPublished(f func()) func() {
	prev := testHookAfterTranscriptPublished
	testHookAfterTranscriptPublished = f
	return func() { testHookAfterTranscriptPublished = prev }
}

// SetTestHookBeforeAudioRetention installs the hook that runs immediately
// before runTranscribe re-checks the generation and applies audio retention.
// Returns a restore func.
func SetTestHookBeforeAudioRetention(f func()) func() {
	prev := testHookBeforeAudioRetention
	testHookBeforeAudioRetention = f
	return func() { testHookBeforeAudioRetention = prev }
}

// SetTestHookAfterGroupCheckBeforeNoteWrites installs the hook that runs
// after runTranscribe's group transcriptStillCurrent re-check has PASSED and
// before the first individually-guarded note-level write. Returns a restore
// func.
func SetTestHookAfterGroupCheckBeforeNoteWrites(f func()) func() {
	prev := testHookAfterGroupCheckBeforeNoteWrites
	testHookAfterGroupCheckBeforeNoteWrites = f
	return func() { testHookAfterGroupCheckBeforeNoteWrites = prev }
}

// SetTestHookAfterRetentionCheckBeforeClaim installs the hook that runs after
// applyAudioRetention's own transcriptStillCurrent re-check has PASSED and
// before it writes retention state (SetRetentionStateIfGeneration /
// ClaimAudioDiscard). Returns a restore func.
func SetTestHookAfterRetentionCheckBeforeClaim(f func()) func() {
	prev := testHookAfterRetentionCheckBeforeClaim
	testHookAfterRetentionCheckBeforeClaim = f
	return func() { testHookAfterRetentionCheckBeforeClaim = prev }
}
