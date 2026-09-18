package worker

import (
	"context"
	"log"
	"time"
)

const staleSweepInterval = time.Minute

// liveRecoverySweepInterval bounds how often the live-template-output
// recovery sweep runs (issue #764). Recovery keys on a one-hour staleness
// window (store.LiveStaleRecoveryAge), so this can safely run far less often
// than the job-lease sweep above.
const liveRecoverySweepInterval = 5 * time.Minute

// liveRecoveryPageSize bounds each RecoverLiveTemplateOutputs page so a large
// backlog cannot monopolize one sweep.
const liveRecoveryPageSize = 200

// liveRecoverer is the store interface needed for live-template-output
// recovery.
type liveRecoverer interface {
	RecoverLiveTemplateOutputs(ctx context.Context, now time.Time, limit int) ([]string, int, error)
}

// RunLiveRecoverySweep periodically removes non-current and stale live
// prompt rows (issue #764's recovery sweep), paging until a sweep returns
// fewer than a full page.
func RunLiveRecoverySweep(ctx context.Context, st liveRecoverer) {
	t := time.NewTicker(liveRecoverySweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				noteIDs, removed, err := st.RecoverLiveTemplateOutputs(ctx, time.Now(), liveRecoveryPageSize)
				if err != nil {
					log.Printf("worker: live recovery sweep failed: %v", err)
					break
				}
				if removed > 0 {
					log.Printf("worker: live recovery sweep: removed %d stale row(s) across %d note(s)", removed, len(noteIDs))
				}
				if removed < liveRecoveryPageSize {
					break
				}
			}
		}
	}
}

// jobRecoverer is the store interface needed for stale-job recovery.
type jobRecoverer interface {
	ResetExpiredRunningJobs(ctx context.Context) (int64, error)
}

// recoverStartupJobs reclaims startup-orphaned running jobs once their lease
// has expired, using the same gate as the periodic stale sweep.
// This avoids stealing in-flight work from a still-live sibling process while
// still recovering genuinely crashed workers after defaultJobLease elapses.
func recoverStartupJobs(ctx context.Context, store jobRecoverer) {
	n, err := store.ResetExpiredRunningJobs(ctx)
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
