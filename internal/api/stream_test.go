package api

import (
	"context"
	"errors"
	"testing"
)

func TestStreamSecondsToMS(t *testing.T) {
	tests := []struct {
		name    string
		seconds float64
		want    int
	}{
		{name: "zero", seconds: 0, want: 0},
		{name: "exact", seconds: 1.25, want: 1250},
		{name: "positive half rounds away from zero", seconds: 0.0015, want: 2},
		{name: "negative half rounds away from zero", seconds: -0.0015, want: -2},
		{name: "negative exact", seconds: -1.25, want: -1250},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secondsToMS(tt.seconds); got != tt.want {
				t.Fatalf("secondsToMS(%v) = %d, want %d", tt.seconds, got, tt.want)
			}
		})
	}
}

func TestStreamEnqueueAudioFrameDropsOldest(t *testing.T) {
	ch := make(chan []byte, 2)
	oldest := []byte("oldest")
	survivor := []byte("survivor")
	ch <- oldest
	ch <- survivor

	frame := []byte("new")
	if err := enqueueAudioFrame(context.Background(), ch, frame); err != nil {
		t.Fatalf("enqueueAudioFrame() error = %v", err)
	}

	if got := string(<-ch); got != "survivor" {
		t.Fatalf("first queued frame = %q, want survivor", got)
	}
	queued := <-ch
	if got := string(queued); got != "new" {
		t.Fatalf("second queued frame = %q, want new", got)
	}

	// enqueueAudioFrame itself intentionally transfers the supplied slice without
	// copying it. handleNoteStream makes the defensive copy at its call site.
	frame[0] = 'N'
	if got := string(queued); got != "New" {
		t.Fatalf("queued frame = %q after caller mutation, want alias New", got)
	}
}

func TestStreamEnqueueAudioFrameReturnsCancelledContextWhenSendCannotProceed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := enqueueAudioFrame(ctx, make(chan []byte), []byte("frame")); !errors.Is(err, context.Canceled) {
		t.Fatalf("enqueueAudioFrame() error = %v, want context.Canceled", err)
	}
}
