package worker

import "context"

// RecoverStartupJobsForTest exports the unexported recoverStartupJobs for
// use in integration tests within the worker_test package.
func RecoverStartupJobsForTest(ctx context.Context, store jobRecoverer) {
	recoverStartupJobs(ctx, store)
}
