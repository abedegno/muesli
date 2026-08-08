package calendar

import "fmt"

// HTTPError reports an upstream HTTP failure while preserving the status code
// for callers that need to classify it without parsing rendered error text.
type HTTPError struct {
	StatusCode int
	Err        error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %d: %v", e.StatusCode, e.Err)
}

func (e *HTTPError) Unwrap() error { return e.Err }
