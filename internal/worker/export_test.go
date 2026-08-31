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

// SetTestHookBeforeSetNoteHashes installs the hook that runs immediately
// before runTranscribe calls SetNoteHashesIfCurrent. Returns a restore func.
func SetTestHookBeforeSetNoteHashes(f func()) func() {
	prev := testHookBeforeSetNoteHashes
	testHookBeforeSetNoteHashes = f
	return func() { testHookBeforeSetNoteHashes = prev }
}

// SetTestHookBeforeSetNotePartialTranscript installs the hook that runs
// immediately before runTranscribe calls SetNotePartialTranscriptIfCurrent
// (at both call sites). Returns a restore func.
func SetTestHookBeforeSetNotePartialTranscript(f func()) func() {
	prev := testHookBeforeSetNotePartialTranscript
	testHookBeforeSetNotePartialTranscript = f
	return func() { testHookBeforeSetNotePartialTranscript = prev }
}

// SetTestHookBeforeDeleteNoteSummaries installs the hook that runs
// immediately before runTranscribe calls DeleteNoteSummariesIfCurrent.
// Returns a restore func.
func SetTestHookBeforeDeleteNoteSummaries(f func()) func() {
	prev := testHookBeforeDeleteNoteSummaries
	testHookBeforeDeleteNoteSummaries = f
	return func() { testHookBeforeDeleteNoteSummaries = prev }
}

// SetTestHookBeforeEnqueueSummarizeJobs installs the hook that runs
// immediately before runTranscribe calls EnqueueSummarizeJobsIfCurrent.
// Returns a restore func.
func SetTestHookBeforeEnqueueSummarizeJobs(f func()) func() {
	prev := testHookBeforeEnqueueSummarizeJobs
	testHookBeforeEnqueueSummarizeJobs = f
	return func() { testHookBeforeEnqueueSummarizeJobs = prev }
}
