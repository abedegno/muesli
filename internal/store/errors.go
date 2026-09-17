package store

import "errors"

// ValidationError is returned by store validation functions when the input
// is invalid. Handlers may expose ValidationError.Error() to the client;
// all other store errors are internal and must NOT be sent to the client.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrInvalidTransition is returned by UpdateReviewState when the requested
// state transition is not permitted by the diarization review lifecycle.
var ErrInvalidTransition = errors.New("invalid state transition")

// ErrInvalidMerge is returned when a merge request is invalid, such as when
// the source and target company IDs are the same.
var ErrInvalidMerge = errors.New("invalid merge")

// ErrInvalidOwner is returned when an action item owner assignment references
// a person that does not belong to the caller.
var ErrInvalidOwner = errors.New("invalid owner")

// ErrSelfLink is returned when a note-link request tries to link a note to itself.
var ErrSelfLink = errors.New("self link")

// ErrAlreadySetUp is returned when first-run setup has already created a user.
var ErrAlreadySetUp = errors.New("already set up")

// ErrGenerationMismatch is returned when a caller carries a transcript
// generation that is no longer current — the transcript was replaced after the
// caller read it. Expected generation 0 means "expected no transcript".
var ErrGenerationMismatch = errors.New("transcript generation mismatch")

// ErrForbidden is returned when a resource is visible to the requester (a live
// shared folder, or a note readable through one) but the specific operation
// requested requires an authority the requester does not have -- e.g. a
// non-owner mutating a shared folder, or filing a note the requester does not
// own. Distinct from ErrNotFound, which is returned for absent, trashed, or
// private non-owned resources so a guessed id is never an existence oracle.
var ErrForbidden = errors.New("forbidden")

// ErrIneligible is returned by RetryPreBriefJob when a pre_generate job's
// (event, template) pair still exists but is no longer eligible for
// generation (the event has started, or the template is no longer
// pre/auto-run/visible), or the job's generation is no longer the brief's
// current one. Distinct from ErrNotFound, which is returned when the job,
// event, brief, or template is simply gone.
var ErrIneligible = errors.New("no longer applicable")
