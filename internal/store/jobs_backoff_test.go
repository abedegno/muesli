package store_test

import (
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
)

func TestRetryBackoff(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
		want     time.Duration
	}{
		{name: "zero", attempts: 0, want: 2 * time.Second},
		{name: "one", attempts: 1, want: 2 * time.Second},
		{name: "two", attempts: 2, want: 4 * time.Second},
		{name: "three", attempts: 3, want: 8 * time.Second},
		{name: "ten", attempts: 10, want: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := store.RetryBackoff(tt.attempts); got != tt.want {
				t.Fatalf("RetryBackoff(%d) = %v, want %v", tt.attempts, got, tt.want)
			}
		})
	}

	prev := time.Duration(0)
	for attempts := 1; attempts <= 6; attempts++ {
		got := store.RetryBackoff(attempts)
		if got < prev {
			t.Fatalf("RetryBackoff(%d) = %v, want non-decreasing sequence (prev %v)", attempts, got, prev)
		}
		prev = got
	}
}
