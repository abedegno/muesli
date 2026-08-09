package main

import (
	"context"
	"time"
)

// watchParentDeath calls onDeath once this process is reparented away from
// parentPID, then returns. It also returns when ctx is cancelled.
//
// It asks the kernel who our parent IS (getppid) rather than whether some pid
// still exists. That distinction matters: "does pid N exist" can be fooled by
// pid reuse, whereas getppid cannot — when the parent dies we are reparented
// and the value changes.
//
// getppid is injected so this is testable without manipulating real processes.
func watchParentDeath(ctx context.Context, parentPID int, interval time.Duration, getppid func() int, onDeath func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if getppid() != parentPID {
				onDeath()
				return
			}
		}
	}
}
