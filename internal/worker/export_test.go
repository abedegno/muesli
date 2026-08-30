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

// SetTestHookBeforeAudioRetention installs the hook after the advisory
// generation read and before the conditional retention write.
// Returns a restore func.
func SetTestHookBeforeAudioRetention(f func()) func() {
	prev := testHookBeforeAudioRetention
	testHookBeforeAudioRetention = f
	return func() { testHookBeforeAudioRetention = prev }
}
