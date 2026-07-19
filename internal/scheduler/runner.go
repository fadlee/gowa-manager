// Package scheduler runs periodic background jobs, such as the daily instance
// media cleanup. The Runner is generic: it invokes a Job on a Schedule and
// guarantees at most one job execution at a time. Time is injectable so tests
// never wait in real time.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Job is a unit of work executed by a Runner. It must respect ctx so a
// cancelled run terminates promptly.
type Job func(ctx context.Context) error

// Schedule computes the duration to wait before the next run, given the
// current time. DailyMidnightUTC is the default schedule used by the cleanup
// runner; tests inject shorter schedules.
type Schedule func(now time.Time) time.Duration

// RunnerOptions configures a Runner. Now, After and Logger default to
// time.Now, time.After and slog.Default when nil.
type RunnerOptions struct {
	Job      Job
	Schedule Schedule
	Now      func() time.Time
	After    func(d time.Duration) <-chan time.Time
	Logger   *slog.Logger
}

// Runner periodically fires a Job according to a Schedule. Only one job run
// is in flight at a time; overlapping ticks are skipped and logged.
type Runner struct {
	job      Job
	schedule Schedule
	now      func() time.Time
	after    func(d time.Duration) <-chan time.Time
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	runMu sync.Mutex
}

// NewRunner builds a Runner from opts. A nil Schedule defaults to
// DailyMidnightUTC.
func NewRunner(opts RunnerOptions) *Runner {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	after := opts.After
	if after == nil {
		after = time.After
	}
	sched := opts.Schedule
	if sched == nil {
		sched = DailyMidnightUTC
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		job:      opts.Job,
		schedule: sched,
		now:      now,
		after:    after,
		logger:   logger,
	}
}

// Start launches the runner loop. It is idempotent: a second call while the
// runner is already active returns false and does nothing. The loop exits
// when ctx is cancelled or Stop is called.
func (r *Runner) Start(ctx context.Context) bool {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return false
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.mu.Unlock()
	go r.loop(ctx)
	return true
}

// Stop signals the loop to exit and waits for it (and any in-flight job) to
// finish. It is idempotent: a second call returns false.
func (r *Runner) Stop() bool {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return false
	}
	r.running = false
	close(r.stopCh)
	done := r.doneCh
	r.mu.Unlock()
	<-done
	// Wait for any in-flight job goroutine to release the run lock.
	r.runMu.Lock()
	r.runMu.Unlock()
	return true
}

func (r *Runner) loop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		close(r.doneCh)
	}()
	for {
		d := r.schedule(r.now())
		select {
		case <-r.after(d):
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		}
		// Re-check cancellation before firing: the schedule wait may have
		// raced with a stop/cancel.
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		default:
		}
		r.fire(ctx)
	}
}

// fire attempts to start the job. If a previous run is still in progress the
// tick is skipped (and logged) so runs never overlap. The job executes in a
// goroutine so the loop can keep observing stop/cancel signals.
func (r *Runner) fire(ctx context.Context) {
	if !r.runMu.TryLock() {
		r.logger.Warn("scheduler: previous run still in progress, skipping")
		return
	}
	go func() {
		defer r.runMu.Unlock()
		if err := r.job(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("scheduler: job failed", "error", err)
		}
	}()
}

// DailyMidnightUTC is a Schedule that returns the duration until the next
// 00:00 UTC.
func DailyMidnightUTC(now time.Time) time.Duration {
	return NextRun(now)
}

// NextRun returns the duration from now until the next midnight UTC. If now
// is exactly midnight UTC, the next run is 24 hours away.
func NextRun(now time.Time) time.Duration {
	u := now.UTC()
	today := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	next := today.Add(24 * time.Hour)
	return next.Sub(now)
}
