package testutil

import (
	"testing"
	"time"
)

func TestFakeClockNow(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clk := NewFakeClock(fixed)
	if got := clk.Now(); !got.Equal(fixed) {
		t.Fatalf("Now() = %v, want %v", got, fixed)
	}
}

func TestFakeClockAdvance(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clk := NewFakeClock(fixed)
	clk.Advance(5 * time.Minute)
	want := fixed.Add(5 * time.Minute)
	if got := clk.Now(); !got.Equal(want) {
		t.Fatalf("Now() after Advance = %v, want %v", got, want)
	}
}

func TestFakeClockAdvanceMultiple(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clk := NewFakeClock(fixed)
	clk.Advance(1 * time.Hour)
	clk.Advance(30 * time.Minute)
	want := fixed.Add(90 * time.Minute)
	if got := clk.Now(); !got.Equal(want) {
		t.Fatalf("Now() after two Advances = %v, want %v", got, want)
	}
}

func TestFakeClockIsFixed(t *testing.T) {
	// FakeClock should return the same instant on repeated calls (no auto-advance).
	fixed := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	clk := NewFakeClock(fixed)
	first := clk.Now()
	second := clk.Now()
	if !first.Equal(second) {
		t.Fatalf("FakeClock should not advance by itself: first=%v second=%v", first, second)
	}
}
