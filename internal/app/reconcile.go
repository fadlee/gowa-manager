package app

import (
	"context"
	"log/slog"

	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// defaultReconcileConcurrency is the bounded concurrency used when
// ReconcilerOptions.Concurrency is not set (0). It keeps startup restart
// traffic bounded so the manager does not fork a large number of processes
// simultaneously on boot.
const defaultReconcileConcurrency = 4

// InstanceLister lists instances to consider for reconciliation.
type InstanceLister interface {
	List(ctx context.Context) ([]instances.Instance, error)
}

// InstanceStarter starts a single instance by id. It is expected to persist
// the resulting status (running/failed) itself, mirroring
// instances.LifecycleService.Start. A non-nil error indicates the instance
// could not be started; the reconciler logs it and continues with the next
// instance so a single failure does not block recovery of the others.
type InstanceStarter interface {
	Start(ctx context.Context, id int64) error
}

// lifecycleStarterAdapter adapts *instances.LifecycleService, whose Start
// returns (LifecycleStatus, error), to the simpler InstanceStarter interface
// used by the reconciler.
type lifecycleStarterAdapter struct {
	service *instances.LifecycleService
}

func (a lifecycleStarterAdapter) Start(ctx context.Context, id int64) error {
	_, err := a.service.Start(ctx, id)
	return err
}

// ReconcilerOptions configures a Reconciler.
type ReconcilerOptions struct {
	Lister      InstanceLister
	Starter     InstanceStarter
	Logger      *slog.Logger
	Concurrency int
	// Readiness, if non-nil, is flipped to ready once Reconcile finishes
	// (success, partial failure, or cancellation). This lets /api/ready
	// report readiness after startup reconciliation completes.
	Readiness *httpapi.AtomicReadiness
}

// Reconciler restarts instances that were marked running when the manager
// previously exited. Because reliably adopting arbitrary orphan processes
// across platforms is not feasible, the manager instead records the state it
// can prove (the persisted status) and restarts previously-running instances
// under new ownership. Rows with status stopped/failed are left untouched.
type Reconciler struct {
	lister      InstanceLister
	starter     InstanceStarter
	logger      *slog.Logger
	concurrency int
	readiness   *httpapi.AtomicReadiness
}

// NewReconciler builds a Reconciler from opts. A concurrency of 0 selects
// the safe default (defaultReconcileConcurrency).
func NewReconciler(opts ReconcilerOptions) *Reconciler {
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = defaultReconcileConcurrency
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		lister:      opts.Lister,
		starter:     opts.Starter,
		logger:      logger,
		concurrency: concurrency,
		readiness:   opts.Readiness,
	}
}

// Reconcile lists all instances and restarts every one whose persisted
// status is "running". Restarts run with bounded concurrency. A failure to
// start one instance (missing version, unavailable port, supervisor error)
// is logged and does not block the others; the underlying starter persists
// a "failed" status for that instance.
//
// The context is respected: when it is cancelled, no further instances are
// started. Instances already in-flight complete; instances not yet started
// are left with their existing persisted status (the manager does not mark
// them). Readiness is flipped to ready when Reconcile returns, regardless of
// outcome, so /api/ready does not stay not-ready forever.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	defer r.markReady()

	items, err := r.lister.List(ctx)
	if err != nil {
		r.logger.Error("reconcile: list instances failed", "error", err)
		return err
	}

	var running []instances.Instance
	for _, item := range items {
		if item.Status == "running" {
			running = append(running, item)
		}
	}
	if len(running) == 0 {
		r.logger.Info("reconcile: no running instances to restart")
		return nil
	}

	r.logger.Info("reconcile: restarting previously-running instances", "count", len(running), "concurrency", r.concurrency)

	sem := make(chan struct{}, r.concurrency)
	var started int
	done := make(chan struct{}, len(running))
loop:
	for _, item := range running {
		item := item
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			r.logger.Info("reconcile: cancelled before starting remaining instances", "remaining", len(running)-started)
			break loop
		}
		started++
		go func() {
			defer func() { <-sem; done <- struct{}{} }()
			if err := r.starter.Start(ctx, item.ID); err != nil {
				if ctx.Err() != nil {
					r.logger.Info("reconcile: start cancelled", "id", item.ID, "error", err)
					return
				}
				r.logger.Error("reconcile: failed to restart instance", "id", item.ID, "name", item.Name, "error", err)
				return
			}
			r.logger.Info("reconcile: restarted instance", "id", item.ID, "name", item.Name)
		}()
		if ctx.Err() != nil {
			break loop
		}
	}

	// Wait for all in-flight starts to finish.
	for i := 0; i < started; i++ {
		<-done
	}
	return nil
}

func (r *Reconciler) markReady() {
	if r.readiness != nil {
		r.readiness.SetReady()
	}
}
