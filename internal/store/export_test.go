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
