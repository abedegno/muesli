package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchParentDeath(t *testing.T) {
	t.Run("calls onDeath when parent changes", func(t *testing.T) {
		const parentPID = 42
		var currentPID atomic.Int64
		currentPID.Store(parentPID)
		called := make(chan struct{}, 1)
		returned := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			watchParentDeath(ctx, parentPID, time.Millisecond, func() int {
				return int(currentPID.Load())
			}, func() {
				called <- struct{}{}
			})
			close(returned)
		}()

		currentPID.Store(parentPID + 1)
		select {
		case <-called:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("onDeath was not called after parent changed")
		}
		select {
		case <-returned:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("watch did not return after parent changed")
		}
	})

	t.Run("stays quiet while parent is unchanged", func(t *testing.T) {
		const parentPID = 42
		var currentPID atomic.Int64
		currentPID.Store(parentPID)
		called := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		returned := make(chan struct{})
		go func() {
			watchParentDeath(ctx, parentPID, time.Millisecond, func() int {
				return int(currentPID.Load())
			}, func() {
				called <- struct{}{}
			})
			close(returned)
		}()

		ticks := time.NewTicker(time.Millisecond)
		defer ticks.Stop()
		for range 3 {
			<-ticks.C
		}
		select {
		case <-called:
			t.Fatal("onDeath was called while parent was unchanged")
		default:
		}
		cancel()
		select {
		case <-returned:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("watch did not return after cancellation")
		}
	})

	t.Run("returns promptly when cancelled", func(t *testing.T) {
		const parentPID = 42
		var currentPID atomic.Int64
		currentPID.Store(parentPID)
		called := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		returned := make(chan struct{})
		go func() {
			watchParentDeath(ctx, parentPID, time.Millisecond, func() int {
				return int(currentPID.Load())
			}, func() {
				called <- struct{}{}
			})
			close(returned)
		}()

		cancel()
		select {
		case <-returned:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("watch did not return after cancellation")
		}
		select {
		case <-called:
			t.Fatal("onDeath was called after context cancellation")
		default:
		}
	})
}
