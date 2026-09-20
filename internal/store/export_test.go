package store

import "context"

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

// ContextWithCrossAnalysisFaultInjection returns a context that scopes the
// given fault-injection hooks to the SPECIFIC AppendCrossAnalysisTurn call
// made with the returned context -- see crossAnalysisFaultInjection's doc
// comment in cross_analysis.go for why this is call-scoped (via
// context.Value) rather than a global mutable hook variable: this file's
// tests run under t.Parallel(), and AppendCrossAnalysisTurn calls made by
// any OTHER test (or by production code) always use a context without this
// value, so they can never observe -- or trigger -- a hook installed here.
// Any of the three hook parameters may be nil to leave that step
// uninjected.
func ContextWithCrossAnalysisFaultInjection(
	ctx context.Context,
	beforeAssistantMessageInsert func(cancel context.CancelFunc),
	beforeConversationTimestampUpdate func(cancel context.CancelFunc),
	beforeCommit func(cancel context.CancelFunc),
) context.Context {
	return context.WithValue(ctx, crossAnalysisFaultInjectionKey{}, &crossAnalysisFaultInjection{
		beforeAssistantMessageInsert:      beforeAssistantMessageInsert,
		beforeConversationTimestampUpdate: beforeConversationTimestampUpdate,
		beforeCommit:                      beforeCommit,
	})
}
