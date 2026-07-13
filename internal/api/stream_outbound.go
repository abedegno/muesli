package api

import (
	"sync"
	"time"
)

const streamOutboundPartialDebounce = 25 * time.Millisecond

type streamOutboundMessageKind int

const (
	streamOutboundMessagePartial streamOutboundMessageKind = iota
	streamOutboundMessageFinal
)

type streamOutboundQueueState struct {
	hasPendingFinal   bool
	hasPendingPartial bool
}

type streamOutboundAction int

const (
	streamOutboundActionQueuePartial streamOutboundAction = iota
	streamOutboundActionCoalescePartial
	streamOutboundActionDropPartial
	streamOutboundActionQueuePriority
)

func decideStreamOutboundAction(state streamOutboundQueueState, kind streamOutboundMessageKind) streamOutboundAction {
	switch kind {
	case streamOutboundMessageFinal:
		return streamOutboundActionQueuePriority
	case streamOutboundMessagePartial:
		if state.hasPendingFinal {
			return streamOutboundActionDropPartial
		}
		if state.hasPendingPartial {
			return streamOutboundActionCoalescePartial
		}
		return streamOutboundActionQueuePartial
	default:
		return streamOutboundActionQueuePriority
	}
}

type streamOutboundMailbox struct {
	mu      sync.Mutex
	notify  chan struct{}
	partial *streamSegmentMessage
	finals  []streamSegmentMessage
	closed  bool
}

func newStreamOutboundMailbox() *streamOutboundMailbox {
	return &streamOutboundMailbox{
		notify: make(chan struct{}, 1),
	}
}

func (q *streamOutboundMailbox) enqueue(msg streamSegmentMessage) {
	q.mu.Lock()
	switch decideStreamOutboundAction(streamOutboundQueueState{
		hasPendingFinal:   len(q.finals) > 0,
		hasPendingPartial: q.partial != nil,
	}, streamOutboundMessageKindFromMessage(msg)) {
	case streamOutboundActionQueuePriority:
		q.finals = append(q.finals, msg)
		q.partial = nil
	case streamOutboundActionQueuePartial, streamOutboundActionCoalescePartial:
		msgCopy := msg
		q.partial = &msgCopy
	case streamOutboundActionDropPartial:
		// A final is already pending. Drop the stale partial.
	}
	q.mu.Unlock()
	q.signal()
}

func (q *streamOutboundMailbox) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *streamOutboundMailbox) closedAndEmpty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed && len(q.finals) == 0 && q.partial == nil
}

func (q *streamOutboundMailbox) nextPriority() (streamSegmentMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.finals) == 0 {
		return streamSegmentMessage{}, false
	}
	msg := q.finals[0]
	q.finals = q.finals[1:]
	return msg, true
}

func (q *streamOutboundMailbox) nextWritablePartial() (streamSegmentMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.finals) > 0 || q.partial == nil {
		return streamSegmentMessage{}, false
	}
	msg := *q.partial
	q.partial = nil
	return msg, true
}

func (q *streamOutboundMailbox) hasPendingPartial() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.partial != nil
}

func (q *streamOutboundMailbox) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func streamOutboundMessageKindFromMessage(msg streamSegmentMessage) streamOutboundMessageKind {
	if msg.Final {
		return streamOutboundMessageFinal
	}
	return streamOutboundMessagePartial
}
