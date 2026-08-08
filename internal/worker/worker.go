package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
)

const defaultJobLease = plugin.RequestTimeout + 2*time.Minute

// Pool runs N worker goroutines that poll for and process jobs.
type Pool struct {
	store    *store.Store
	proc     *Processor
	workers  int
	pollWait time.Duration
	lease    time.Duration
	claimJob func(context.Context, time.Duration) (model.Job, bool, error)
	process  func(context.Context, model.Job) error

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewPool builds a worker pool. workers is the goroutine count; the lease must
// exceed the maximum time a single job can legitimately block on a plugin
// call, or long-running jobs can be reclaimed and re-run concurrently by a
// second worker.
func NewPool(st *store.Store, proc *Processor, workers int) *Pool {
	if workers < 1 {
		workers = 1
	}
	p := &Pool{
		store:    st,
		proc:     proc,
		workers:  workers,
		pollWait: time.Second,
		lease:    defaultJobLease,
	}
	if st != nil {
		p.claimJob = st.ClaimJob
	}
	if proc != nil {
		p.process = func(ctx context.Context, job model.Job) error {
			proc.Process(ctx, job)
			return nil
		}
	}
	return p
}

// Run starts the worker goroutines and blocks until ctx is cancelled (or Stop is
// called), returning only after every goroutine has exited. Callers that run it
// in a goroutine should use Stop to shut down deterministically.
func (p *Pool) Run(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)

	// At startup, any job left in 'running' is orphaned (the process that owned
	// it is gone). Reset them all to 'pending' unconditionally so they can be
	// re-claimed by workers below.
	if p.store != nil {
		recoverStartupJobs(ctx, p.store)
	}

	// Launch a background goroutine that periodically resets running jobs whose
	// leases have expired (catches workers that die at runtime, not on startup).
	if p.store != nil {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			runStaleSweep(ctx, p.store)
		}()
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			p.loop(ctx, id)
		}(i)
	}
	p.wg.Wait()
}

// Stop cancels the pool's context and blocks until all worker goroutines have
// returned. Safe to call once after Run has started; this removes the shutdown
// race where the process (or a test harness tempdir) tears down while workers are
// still claiming/processing jobs.
func (p *Pool) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

func (p *Pool) loop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		job, ok, err := p.claimJob(ctx, p.lease)
		if err != nil {
			slog.ErrorContext(ctx, "claim error", "worker_id", id, "error", err)
			sleep(ctx, p.pollWait)
			continue
		}
		if !ok {
			sleep(ctx, p.pollWait)
			continue
		}
		if err := p.process(ctx, job); err != nil {
			slog.ErrorContext(ctx, "process error", "worker_id", id, "job_id", job.ID, "error", err)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
