package store

// SetTestHookAfterPriorTranscriptRead installs the hook for tests in
// package store_test and returns a restore function.
func SetTestHookAfterPriorTranscriptRead(f func()) func() {
	prev := testHookAfterPriorTranscriptRead
	testHookAfterPriorTranscriptRead = f
	return func() { testHookAfterPriorTranscriptRead = prev }
}
