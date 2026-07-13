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
	mu          sync.Mutex
	partialWake chan struct{}
	finalWake   chan struct{}
	closeWake   chan struct{}
	partial     *streamSegmentMessage
	finals      []streamSegmentMessage
	closed      bool
}

func newStreamOutboundMailbox() *streamOutboundMailbox {
	return &streamOutboundMailbox{
		partialWake: make(chan struct{}, 1),
		finalWake:   make(chan struct{}, 1),
		closeWake:   make(chan struct{}, 1),
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
		q.signalFinal()
	case streamOutboundActionQueuePartial, streamOutboundActionCoalescePartial:
		msgCopy := msg
		q.partial = &msgCopy
		q.signalPartial()
	case streamOutboundActionDropPartial:
		// A final is already pending. Drop the stale partial.
	}
	q.mu.Unlock()
}

func (q *streamOutboundMailbox) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signalClose()
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

func (q *streamOutboundMailbox) signalPartial() {
	select {
	case q.partialWake <- struct{}{}:
	default:
	}
}

func (q *streamOutboundMailbox) signalFinal() {
	select {
	case q.finalWake <- struct{}{}:
	default:
	}
}

func (q *streamOutboundMailbox) signalClose() {
	select {
	case q.closeWake <- struct{}{}:
	default:
	}
}

func (q *streamOutboundMailbox) runWriter(write func(streamSegmentMessage) error) error {
	var partialTimer *time.Timer
	stopTimer := func() {
		if partialTimer == nil {
			return
		}
		if !partialTimer.Stop() {
			select {
			case <-partialTimer.C:
			default:
			}
		}
		partialTimer = nil
	}
	startTimer := func() {
		stopTimer()
		partialTimer = time.NewTimer(streamOutboundPartialDebounce)
	}
	defer stopTimer()
	for {
		if msg, ok := q.nextPriority(); ok {
			stopTimer()
			if err := write(msg); err != nil {
				return err
			}
			continue
		}
		if q.closedAndEmpty() {
			return nil
		}
		if q.hasPendingPartial() {
			if partialTimer == nil {
				startTimer()
			}
			select {
			case <-q.finalWake:
				stopTimer()
				continue
			case <-q.partialWake:
				continue
			case <-partialTimer.C:
				if msg, ok := q.nextWritablePartial(); ok {
					stopTimer()
					if err := write(msg); err != nil {
						return err
					}
				} else {
					stopTimer()
				}
			case <-q.closeWake:
				stopTimer()
				continue
			}
			continue
		}
		stopTimer()
		select {
		case <-q.finalWake:
		case <-q.partialWake:
		case <-q.closeWake:
			if q.closedAndEmpty() {
				return nil
			}
		}
	}
}

func streamOutboundMessageKindFromMessage(msg streamSegmentMessage) streamOutboundMessageKind {
	if msg.Final {
		return streamOutboundMessageFinal
	}
	return streamOutboundMessagePartial
}
