package api

import "sync"

type streamOutboundMessageKind int

const (
	streamOutboundMessagePartial streamOutboundMessageKind = iota
	streamOutboundMessageFinal
)

type streamOutboundQueueState struct {
	hasPendingPartial bool
}

type streamOutboundAction int

const (
	streamOutboundActionQueuePartial streamOutboundAction = iota
	streamOutboundActionCoalescePartial
	streamOutboundActionQueuePriority
)

func decideStreamOutboundAction(state streamOutboundQueueState, kind streamOutboundMessageKind) streamOutboundAction {
	switch kind {
	case streamOutboundMessageFinal:
		return streamOutboundActionQueuePriority
	case streamOutboundMessagePartial:
		if state.hasPendingPartial {
			return streamOutboundActionCoalescePartial
		}
		return streamOutboundActionQueuePartial
	default:
		return streamOutboundActionQueuePriority
	}
}

type streamOutboundQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	partial  *streamSegmentMessage
	priority []streamSegmentMessage
	closed   bool
}

func newStreamOutboundQueue() *streamOutboundQueue {
	q := &streamOutboundQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *streamOutboundQueue) enqueue(msg streamSegmentMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()

	switch decideStreamOutboundAction(streamOutboundQueueState{hasPendingPartial: q.partial != nil}, streamOutboundMessageKindFromMessage(msg)) {
	case streamOutboundActionQueuePriority:
		q.priority = append(q.priority, msg)
	case streamOutboundActionQueuePartial, streamOutboundActionCoalescePartial:
		msgCopy := msg
		q.partial = &msgCopy
	}
	q.cond.Signal()
}

func (q *streamOutboundQueue) next() (streamSegmentMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for {
		if len(q.priority) > 0 {
			msg := q.priority[0]
			q.priority = q.priority[1:]
			return msg, true
		}
		if q.partial != nil {
			msg := *q.partial
			q.partial = nil
			return msg, true
		}
		if q.closed {
			return streamSegmentMessage{}, false
		}
		q.cond.Wait()
	}
}

func (q *streamOutboundQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

func streamOutboundMessageKindFromMessage(msg streamSegmentMessage) streamOutboundMessageKind {
	if msg.Final {
		return streamOutboundMessageFinal
	}
	return streamOutboundMessagePartial
}
