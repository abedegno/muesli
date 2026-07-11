package worker

import (
	"context"
	"log"
	"time"
)

const staleSweepInterval = time.Minute

// jobRecoverer is the store interface needed for stale-job recovery.
type jobRecoverer interface {
	ResetRunningJobs(ctx context.Context) (int64, error)
	ResetExpiredRunningJobs(ctx context.Context) (int64, error)
}

// recoverStartupJobs resets ALL running jobs to pending at startup.
// At startup, ANY running job is orphaned — its process is gone.
func recoverStartupJobs(ctx context.Context, store jobRecoverer) {
	n, err := store.ResetRunningJobs(ctx)
	if err != nil {
		log.Printf("worker: startup recovery failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("worker: startup: recovered %d orphaned running job(s)", n)
	}
}

// runStaleSweep periodically resets running jobs with expired leases to pending.
// This catches jobs whose worker goroutine died at runtime (lease-expiry gated).
func runStaleSweep(ctx context.Context, store jobRecoverer) {
	t := time.NewTicker(staleSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := store.ResetExpiredRunningJobs(ctx)
			if err != nil {
				log.Printf("worker: stale sweep failed: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("worker: stale sweep: recovered %d expired running job(s)", n)
			}
		}
	}
}
