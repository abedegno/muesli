package api

import "sync"

// inFlightGuard is an in-process, per-key mutual-exclusion guard used to
// reject concurrent requests that would otherwise race on the same
// resource (e.g. two concurrent resummarize calls for the same note id
// both deleting + enqueueing summaries). It is a request-level API guard
// only, not a DB-level lock: it protects a single API process from
// double-processing the same key, and is orthogonal to (and does not
// replace) the jobs table's `FOR UPDATE SKIP LOCKED` worker leasing.
//
// The zero value is ready to use.
type inFlightGuard struct {
	inFlight sync.Map // key (string) -> struct{}
}

// tryAcquire attempts to claim key for the duration of an in-flight
// operation and reports whether the claim succeeded. LoadOrStore performs
// the check-and-set atomically, so concurrent callers can never both
// "win" for the same key. Callers that acquire successfully must call
// release(key) exactly once, typically via `defer`, to free the slot on
// every exit path (success, error, or client-cancellation).
func (g *inFlightGuard) tryAcquire(key string) bool {
	_, loaded := g.inFlight.LoadOrStore(key, struct{}{})
	return !loaded
}

// release frees the slot for key so a future caller may acquire it.
func (g *inFlightGuard) release(key string) {
	g.inFlight.Delete(key)
}
