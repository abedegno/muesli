package store

// SetTestHookAfterPriorTranscriptRead installs the hook for tests in
// package store_test and returns a restore function.
func SetTestHookAfterPriorTranscriptRead(f func()) func() {
	prev := testHookAfterPriorTranscriptRead
	testHookAfterPriorTranscriptRead = f
	return func() { testHookAfterPriorTranscriptRead = prev }
}

// SetTestHookAfterConfirmSegmentSpeakerRead installs the hook for tests in
// package store_test and returns a restore function.
func SetTestHookAfterConfirmSegmentSpeakerRead(f func()) func() {
	prev := testHookAfterConfirmSegmentSpeakerRead
	testHookAfterConfirmSegmentSpeakerRead = f
	return func() { testHookAfterConfirmSegmentSpeakerRead = prev }
}

// SetTestHookAfterDeleteNoteSummariesGenerationCheck installs the hook for
// tests in package store_test and returns a restore function.
func SetTestHookAfterDeleteNoteSummariesGenerationCheck(f func()) func() {
	prev := testHookAfterDeleteNoteSummariesGenerationCheck
	testHookAfterDeleteNoteSummariesGenerationCheck = f
	return func() { testHookAfterDeleteNoteSummariesGenerationCheck = prev }
}

// SetTestHookAfterListReadableNotesRowsLoaded installs the hook for tests in
// package store_test and returns a restore function. The hook is
// package-global and invoked by EVERY ListReadableNotes call (issue #12
// round-2 review finding), so f must check its requesterID argument and
// no-op for any requester it does not recognize — otherwise it can block or
// interfere with an unrelated ListReadableNotes call made by a different,
// concurrently-running (t.Parallel()) test. Get/set is mutex-guarded so
// arming/restoring the hook is itself race-free under concurrent callers.
func SetTestHookAfterListReadableNotesRowsLoaded(f func(requesterID string)) func() {
	testHookAfterListReadableNotesRowsLoadedMu.Lock()
	prev := testHookAfterListReadableNotesRowsLoaded
	testHookAfterListReadableNotesRowsLoaded = f
	testHookAfterListReadableNotesRowsLoadedMu.Unlock()
	return func() {
		testHookAfterListReadableNotesRowsLoadedMu.Lock()
		testHookAfterListReadableNotesRowsLoaded = prev
		testHookAfterListReadableNotesRowsLoadedMu.Unlock()
	}
}
