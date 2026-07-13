package api

import (
	"testing"
	"time"
)

func TestDecideStreamOutboundAction(t *testing.T) {
	cases := []struct {
		name  string
		state streamOutboundQueueState
		kind  streamOutboundMessageKind
		want  streamOutboundAction
	}{
		{
			name:  "first partial queues",
			state: streamOutboundQueueState{hasPendingPartial: false},
			kind:  streamOutboundMessagePartial,
			want:  streamOutboundActionQueuePartial,
		},
		{
			name:  "second partial coalesces",
			state: streamOutboundQueueState{hasPendingPartial: true},
			kind:  streamOutboundMessagePartial,
			want:  streamOutboundActionCoalescePartial,
		},
		{
			name:  "final bypasses partial lane",
			state: streamOutboundQueueState{hasPendingPartial: true},
			kind:  streamOutboundMessageFinal,
			want:  streamOutboundActionQueuePriority,
		},
		{
			name:  "partial drops once final is pending",
			state: streamOutboundQueueState{hasPendingFinal: true, hasPendingPartial: true},
			kind:  streamOutboundMessagePartial,
			want:  streamOutboundActionDropPartial,
		},
		{
			name:  "final still prioritizes when idle",
			state: streamOutboundQueueState{hasPendingPartial: false},
			kind:  streamOutboundMessageFinal,
			want:  streamOutboundActionQueuePriority,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideStreamOutboundAction(tc.state, tc.kind); got != tc.want {
				t.Fatalf("decideStreamOutboundAction(%+v, %v) = %v, want %v", tc.state, tc.kind, got, tc.want)
			}
		})
	}
}

func TestStreamOutboundWriterFinalNotStarvedUnderPartialFlood(t *testing.T) {
	q := newStreamOutboundMailbox()
	written := make(chan streamSegmentMessage, 1024)
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		errCh <- q.runWriter(func(msg streamSegmentMessage) error {
			written <- msg
			return nil
		})
	}()

	partial := streamSegmentMessage{Type: "segment", Text: "partial", Final: false}
	final := streamSegmentMessage{Type: "segment", Text: "final", Final: true}

	const floodCount = 500
	go func() {
		for i := 0; i < floodCount; i++ {
			q.enqueue(partial)
			if i == floodCount/2 {
				q.enqueue(final)
			}
		}
		q.close()
	}()

	deadline := time.After(1 * time.Second)
	for {
		select {
		case msg := <-written:
			if !msg.Final {
				continue
			}
			if msg.Text != "final" {
				t.Fatalf("final message text = %q, want final", msg.Text)
			}
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("writer returned error: %v", err)
				}
			case <-done:
			case <-deadline:
				t.Fatal("timed out waiting for writer shutdown after final")
			}
			return
		case err := <-errCh:
			if err != nil {
				t.Fatalf("writer returned error: %v", err)
			}
			t.Fatal("writer exited before final was observed")
		case <-deadline:
			t.Fatal("timed out waiting for final to be written")
		}
	}
}
