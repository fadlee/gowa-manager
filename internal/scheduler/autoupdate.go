package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/versions"
)

// VersionLister discovers available GOWA versions. versions.Service satisfies
// this interface.
type VersionLister interface {
	GetAvailableVersions(ctx context.Context, limit int) ([]versions.VersionInfo, error)
}

// VersionInstaller installs a specific GOWA version. versions.VersionInstaller
// satisfies this interface.
type VersionInstaller interface {
	Install(ctx context.Context, version string) (versions.InstallResult, error)
}

// InstanceRestarter restarts a single instance by id. In production (Task 11)
// an adapter wraps instances.LifecycleService.Restart, which returns
// (LifecycleStatus, error); the adapter discards the status and returns only
// the error.
type InstanceRestarter interface {
	Restart(ctx context.Context, id int64) error
}

// AutoUpdateOptions configures an AutoUpdate service. Now and After default to
// time.Now and time.After when nil. OnCheck, if set, is invoked at the start
// of every Check call (used by tests to count scheduled checks).
type AutoUpdateOptions struct {
	Versions  VersionLister
	Installer VersionInstaller
	Lister    InstanceLister
	Restarter InstanceRestarter
	Logger    *slog.Logger
	Now       func() time.Time
	After     func(time.Duration) <-chan time.Time
	OnCheck   func()
}

// autoUpdateState is the mutable, mutex-protected internal status. Times are
// zero-valued when never set.
type autoUpdateState struct {
	lastCheck     time.Time
	lastUpdate    time.Time
	latestVersion string
	nextCheck     time.Time
}

// AutoUpdate periodically checks for new GOWA versions, installs them, and
// restarts running instances that track the "latest" alias. It mirrors the
// legacy Bun AutoUpdater behavior:
//
//   - getStatus returns a copy each call.
//   - checkAndUpdate skips if a check is already in progress.
//   - It finds the concrete isLatest release (not the "latest" alias).
//   - It installs the new version without cleaning the active or previous
//     version (preserving them for rollback).
//   - It does NOT persist a concrete version to instance.gowa_version during
//     auto-update; instances keep gowa_version="latest" (or empty) and resolve
//     to the newest installed binary at start time. This matches the legacy
//     Bun behavior where "persist instance version only at legacy-compatible
//     point" means: do not write a concrete version during auto-update.
//   - Restarts are sequential (one at a time). Bounded/parallel restarts are
//     a future option.
//   - Per-instance restart failures are logged and do not abort the remaining
//     restarts.
type AutoUpdate struct {
	versions  VersionLister
	installer VersionInstaller
	lister    InstanceLister
	restarter InstanceRestarter
	logger    *slog.Logger
	now       func() time.Time
	after     func(time.Duration) <-chan time.Time
	onCheck   func()

	mu       sync.Mutex
	state    autoUpdateState
	checking atomic.Bool

	runnerMu      sync.Mutex
	runner        *Runner
	runnerActive  atomic.Bool
	intervalNanos atomic.Int64
}

// NewAutoUpdate builds an AutoUpdate service from opts.
func NewAutoUpdate(opts AutoUpdateOptions) *AutoUpdate {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	after := opts.After
	if after == nil {
		after = time.After
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoUpdate{
		versions:  opts.Versions,
		installer: opts.Installer,
		lister:    opts.Lister,
		restarter: opts.Restarter,
		logger:    logger,
		now:       now,
		after:     after,
		onCheck:   opts.OnCheck,
	}
}

// Status returns a snapshot of the auto-update status. It satisfies
// httpapi.AutoUpdateService. Nullable fields are nil when never set; times
// are formatted as RFC3339 UTC.
func (a *AutoUpdate) Status(context.Context) (httpapi.AutoUpdateStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return httpapi.AutoUpdateStatus{
		LastCheck:     formatTimePtr(a.state.lastCheck),
		LastUpdate:    formatTimePtr(a.state.lastUpdate),
		LatestVersion: formatStrPtr(a.state.latestVersion),
		IsChecking:    a.checking.Load(),
		NextCheck:     formatTimePtr(a.state.nextCheck),
	}, nil
}

// Check runs a single check-and-update cycle. It satisfies
// httpapi.AutoUpdateService. If a check is already in progress it returns
// {Success: true, Updated: false} without doing work (overlapping-run
// prevention). Errors during the check are logged and swallowed (returning
// Success: false, Updated: false) to match legacy behavior where the HTTP
// caller always gets a 200 with the result.
func (a *AutoUpdate) Check(ctx context.Context) (httpapi.AutoUpdateCheckResult, error) {
	if a.onCheck != nil {
		a.onCheck()
	}
	// Overlapping-run prevention: atomically claim the check slot.
	if !a.checking.CompareAndSwap(false, true) {
		a.logger.Info("auto-update: check already in progress, skipping")
		return httpapi.AutoUpdateCheckResult{Success: true}, nil
	}
	defer func() {
		a.checking.Store(false)
		a.updateNextCheck()
	}()

	now := a.now()
	a.mu.Lock()
	a.state.lastCheck = now
	a.mu.Unlock()

	a.logger.Info("auto-update: checking for updates")

	// Get latest version from GitHub.
	available, err := a.versions.GetAvailableVersions(ctx, 1)
	if err != nil {
		a.logger.Error("auto-update: error during update check", "error", err)
		return httpapi.AutoUpdateCheckResult{Success: false}, nil
	}
	if len(available) == 0 {
		a.logger.Info("auto-update: no versions available from GitHub")
		return httpapi.AutoUpdateCheckResult{Success: true}, nil
	}

	// Find the concrete isLatest release (not the 'latest' alias).
	var latest *versions.VersionInfo
	for i := range available {
		v := &available[i]
		if v.Version != "latest" && v.IsLatest {
			latest = v
			break
		}
	}
	if latest == nil {
		a.logger.Info("auto-update: could not determine latest version")
		return httpapi.AutoUpdateCheckResult{Success: true}, nil
	}

	a.mu.Lock()
	a.state.latestVersion = latest.Version
	a.mu.Unlock()

	a.logger.Info("auto-update: latest version", "version", latest.Version)

	// Check if already installed.
	if latest.Installed {
		a.logger.Info("auto-update: version already installed", "version", latest.Version)
		return httpapi.AutoUpdateCheckResult{Success: true}, nil
	}

	// Download and install the new version. The old/active version is NOT
	// cleaned or removed — it is preserved for rollback.
	a.logger.Info("auto-update: installing version", "version", latest.Version)
	if _, err := a.installer.Install(ctx, latest.Version); err != nil {
		a.logger.Error("auto-update: install failed", "version", latest.Version, "error", err)
		return httpapi.AutoUpdateCheckResult{Success: false}, nil
	}
	a.logger.Info("auto-update: version installed successfully", "version", latest.Version)

	a.mu.Lock()
	a.state.lastUpdate = a.now()
	a.mu.Unlock()

	// Restart running instances that track 'latest'. The instance
	// gowa_version is NOT persisted to a concrete version — instances keep
	// "latest" (or empty) and resolve to the newest installed binary at
	// start time, matching legacy Bun behavior.
	restarted := a.restartLatestInstances(ctx)

	a.logger.Info("auto-update: update complete", "version", latest.Version, "restarted", restarted)
	v := latest.Version
	return httpapi.AutoUpdateCheckResult{
		Success:            true,
		Updated:            true,
		Version:            &v,
		RestartedInstances: restarted,
	}, nil
}

// Instances returns all instances eligible for auto-update: those whose
// gowa_version is "latest", empty, or missing. It satisfies
// httpapi.AutoUpdateService.
func (a *AutoUpdate) Instances(ctx context.Context) ([]httpapi.AutoUpdateInstance, error) {
	items, err := a.lister.List(ctx)
	if err != nil {
		return nil, err
	}
	result := []httpapi.AutoUpdateInstance{}
	for _, inst := range items {
		if inst.GOWAVersion == "latest" || inst.GOWAVersion == "" {
			result = append(result, httpapi.AutoUpdateInstance{
				ID:     inst.ID,
				Name:   inst.Name,
				Status: inst.Status,
			})
		}
	}
	return result, nil
}

// Start begins the periodic check loop. The first check runs after a short
// delay (1 minute by default) to let the server stabilize, then periodically
// at the given interval. Re-starting clears any previously scheduled loop
// (idempotent). The loop exits when ctx is cancelled or Stop is called.
func (a *AutoUpdate) Start(ctx context.Context, interval time.Duration) {
	// Clear any previously scheduled runner. We release runnerMu before
	// calling Stop to avoid a deadlock with updateNextCheck (which runs in
	// the job goroutine's defer and would block on runnerMu while Stop
	// waits for the job to release runMu).
	a.runnerMu.Lock()
	prev := a.runner
	a.runner = nil
	a.runnerMu.Unlock()
	if prev != nil {
		prev.Stop()
	}

	a.intervalNanos.Store(int64(interval))
	firstCheckDelay := time.Minute
	if interval < time.Minute {
		firstCheckDelay = interval
	}

	first := true
	schedule := func(time.Time) time.Duration {
		if first {
			first = false
			return firstCheckDelay
		}
		return interval
	}

	r := NewRunner(RunnerOptions{
		Job:      a.job,
		Schedule: schedule,
		Now:      a.now,
		After:    a.after,
		Logger:   a.logger,
	})
	a.runnerMu.Lock()
	a.runner = r
	a.runnerMu.Unlock()
	a.runnerActive.Store(true)
	r.Start(ctx)

	// Set nextCheck immediately so callers know when the first check will
	// happen.
	a.mu.Lock()
	a.state.nextCheck = a.now().Add(firstCheckDelay)
	a.mu.Unlock()
}

// Stop halts the periodic check loop and clears nextCheck. It is a safe no-op
// if the loop was never started.
func (a *AutoUpdate) Stop() {
	a.runnerActive.Store(false)

	a.runnerMu.Lock()
	r := a.runner
	a.runner = nil
	a.runnerMu.Unlock()

	if r != nil {
		r.Stop()
	}

	a.mu.Lock()
	a.state.nextCheck = time.Time{}
	a.mu.Unlock()
}

// job is the Runner job that wraps Check. The Runner already has one-at-a-time
// guard, but Check also has its own overlapping-run prevention (atomic
// CompareAndSwap) so a manual Check concurrent with a scheduled tick is safe.
func (a *AutoUpdate) job(ctx context.Context) error {
	_, _ = a.Check(ctx)
	return nil
}

// restartLatestInstances restarts running instances that track the "latest"
// version alias (or have no version set). Restarts are sequential; a failure
// for one instance is logged and does not abort the remaining restarts.
// Returns the count of successfully restarted instances.
func (a *AutoUpdate) restartLatestInstances(ctx context.Context) int {
	eligible, err := a.Instances(ctx)
	if err != nil {
		a.logger.Error("auto-update: failed to list instances", "error", err)
		return 0
	}

	var running []httpapi.AutoUpdateInstance
	for _, inst := range eligible {
		if inst.Status == "running" {
			running = append(running, inst)
		}
	}

	if len(running) == 0 {
		a.logger.Info("auto-update: no running instances using \"latest\" version")
		return 0
	}

	a.logger.Info("auto-update: restarting instances", "count", len(running))

	restarted := 0
	for _, inst := range running {
		if err := ctx.Err(); err != nil {
			break
		}
		a.logger.Info("auto-update: restarting instance", "id", inst.ID, "name", inst.Name)
		if err := a.restarter.Restart(ctx, inst.ID); err != nil {
			a.logger.Error("auto-update: failed to restart instance", "id", inst.ID, "error", err)
			continue
		}
		restarted++
		a.logger.Info("auto-update: instance restarted", "id", inst.ID)
	}
	return restarted
}

// updateNextCheck sets nextCheck to now + interval if the runner is active,
// or clears it if the runner is stopped. Uses atomics to avoid locking
// runnerMu, which would deadlock with Stop (Stop holds runnerMu while waiting
// for the job goroutine to release runMu; the job's defer calls
// updateNextCheck).
func (a *AutoUpdate) updateNextCheck() {
	if !a.runnerActive.Load() {
		a.mu.Lock()
		a.state.nextCheck = time.Time{}
		a.mu.Unlock()
		return
	}
	interval := time.Duration(a.intervalNanos.Load())
	a.mu.Lock()
	defer a.mu.Unlock()
	if interval > 0 {
		a.state.nextCheck = a.now().Add(interval)
	} else {
		a.state.nextCheck = time.Time{}
	}
}

// formatTimePtr returns a pointer to an RFC3339 UTC string for a non-zero
// time, or nil for a zero time.
func formatTimePtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// formatStrPtr returns a pointer to s if non-empty, or nil.
func formatStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Compile-time assertion that AutoUpdate satisfies httpapi.AutoUpdateService.
var _ httpapi.AutoUpdateService = (*AutoUpdate)(nil)
