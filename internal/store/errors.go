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
