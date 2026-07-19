package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/fadlee/gowa-manager/internal/buildinfo"
	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/monitoring"
	"github.com/fadlee/gowa-manager/internal/ownership"
	"github.com/fadlee/gowa-manager/internal/scheduler"
	staticassets "github.com/fadlee/gowa-manager/internal/static"
	"github.com/fadlee/gowa-manager/internal/supervisor"
	"github.com/fadlee/gowa-manager/internal/system"
	"github.com/fadlee/gowa-manager/internal/versions"
)

type Releaser interface{ Release() error }
type Closer interface{ Close() error }

type DBHandle interface {
	Closer
	SQLDB() *database.DB
}

// Schedulers is the handle returned by BuildSchedulers. Start launches the
// background schedulers (cleanup + auto-update); Stop halts them and waits
// for in-flight jobs to finish. Both are idempotent.
type Schedulers interface {
	Start(ctx context.Context) error
	Stop()
}

// RuntimeConnections is an optional hook for closing runtime-level
// connections (device client, process monitor) during shutdown. It is
// invoked after HTTP drain and before child-process cleanup. Implementations
// must be idempotent. If nil, the step is a no-op (documented below).
type RuntimeConnections interface {
	Close() error
}

type Options struct {
	Config               config.Config
	Logger               *slog.Logger
	AcquireLock          func(dataDir string) (Releaser, error)
	OpenDB               func(context.Context, string) (Closer, error)
	BuildHTTPDeps        func(context.Context, httpDepsOptions) (httpapi.Dependencies, error)
	BuildSchedulers      func(context.Context, httpapi.Dependencies) (Schedulers, error)
	Listen               func(network, address string) (net.Listener, error)
	OnStarted            func(addr string)
	OnReadiness          func(*httpapi.AtomicReadiness)
	ReconcileConcurrency int
	// AutoUpdateInterval is the interval between auto-update checks. Defaults
	// to 1 hour when zero, matching the legacy Bun AutoUpdater.start(60*60*1000).
	AutoUpdateInterval time.Duration
	// ForceShutdown, when closed, forces an immediate (non-graceful)
	// shutdown: the HTTP server is closed without draining and the shutdown
	// timeout is skipped. This models the second SIGINT/SIGTERM force path.
	// When nil, only ctx.Done triggers shutdown.
	ForceShutdown <-chan struct{}
	// OnEvent, when non-nil, is called at each lifecycle step (startup and
	// shutdown) with a short event tag. It is intended for lifecycle-order
	// tests and must not block. Tags: "reconcile-start", "reconcile-done",
	// "ready", "unready", "stop-http-intake", "drain-done",
	// "runtime-connections-closed", "child-policy".
	OnEvent func(string)
}

func Run(ctx context.Context, opts Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	emit := func(tag string) {
		if opts.OnEvent != nil {
			opts.OnEvent(tag)
		}
	}
	acquireLock := opts.AcquireLock
	if acquireLock == nil {
		acquireLock = func(dataDir string) (Releaser, error) { return ownership.Acquire(dataDir) }
	}
	openDB := opts.OpenDB
	if openDB == nil {
		openDB = func(ctx context.Context, dataDir string) (Closer, error) { return database.Open(ctx, dataDir) }
	}
	listen := opts.Listen
	if listen == nil {
		listen = net.Listen
	}
	buildDeps := opts.BuildHTTPDeps
	if buildDeps == nil {
		buildDeps = buildHTTPDeps
	}
	buildSchedulers := opts.BuildSchedulers
	if buildSchedulers == nil {
		buildSchedulers = buildDefaultSchedulers(opts.AutoUpdateInterval)
	}

	// --- Startup: lock -> DB -> services -> listen -> reconcile -> schedulers -> ready ---

	lock, err := acquireLock(opts.Config.DataDir)
	if err != nil {
		return err
	}
	releaseLock := true
	defer func() {
		if releaseLock {
			_ = lock.Release()
		}
	}()

	dbCloser, err := openDB(ctx, opts.Config.DataDir)
	if err != nil {
		return err
	}
	closeDB := true
	defer func() {
		if closeDB {
			_ = dbCloser.Close()
		}
	}()
	db, ok := dbFromCloser(dbCloser)
	deps := httpapi.Dependencies{Logger: logger, StaticFS: staticassets.FS()}
	if ok {
		builtDeps, err := buildDeps(ctx, httpDepsOptions{DB: db, DataDir: opts.Config.DataDir, Logger: logger})
		if err != nil {
			return err
		}
		deps = builtDeps
	} else if opts.BuildHTTPDeps != nil {
		builtDeps, err := buildDeps(ctx, httpDepsOptions{DataDir: opts.Config.DataDir, Logger: logger})
		if err != nil {
			return err
		}
		deps = builtDeps
	} else {
		return errors.New("database handle does not expose sqlite connection")
	}

	ln, err := listenFirstAvailable(listen, opts.Config.Port)
	if err != nil {
		return err
	}
	readiness := httpapi.NewReadiness()
	deps.Readiness = readiness
	if opts.OnReadiness != nil {
		opts.OnReadiness(readiness)
	}
	server := &http.Server{Handler: httpapi.New(deps)}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ln)
	}()
	if opts.OnStarted != nil {
		opts.OnStarted(ln.Addr().String())
	}
	logger.Info("Go manager HTTP server started", "addr", ln.Addr().String())

	// Build schedulers now (after deps are wired) but do not start them until
	// reconciliation completes, matching the startup order:
	// reconciliation -> schedulers -> ready.
	schedulers, err := buildSchedulers(ctx, deps)
	if err != nil {
		logger.Error("failed to build schedulers", "error", err)
		// Schedulers are non-fatal: the manager can still serve without
		// background cleanup/auto-update. We continue with a no-op so
		// shutdown ordering remains consistent.
		schedulers = noopSchedulers{}
	}

	// Reconcile previously-running instances after the HTTP server is
	// listening so /api/ready can be polled. Readiness stays not-ready until
	// BOTH reconciliation AND scheduler start complete (per the plan order:
	// reconciliation -> schedulers -> ready).
	reconcileCtx, cancelReconcile := context.WithCancel(ctx)
	reconcileDone := make(chan struct{})
	schedulersStarted := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		defer cancelReconcile()
		emit("reconcile-start")
		runReconciliation(reconcileCtx, deps, logger, opts.ReconcileConcurrency, nil) // readiness flipped after schedulers start
		emit("reconcile-done")
		// Start schedulers after reconciliation completes (or is cancelled).
		// On cancellation we skip scheduler start to avoid launching
		// background work during shutdown.
		if reconcileCtx.Err() == nil {
			if err := schedulers.Start(ctx); err != nil {
				logger.Error("failed to start schedulers", "error", err)
			}
		}
		close(schedulersStarted)
		emit("ready")
		readiness.SetReady()
	}()

	// --- Wait for shutdown trigger (ctx.Done, server error, or force signal) ---
	force := false
	select {
	case <-ctx.Done():
	case <-opts.ForceShutdown:
		force = true
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancelReconcile()
			<-reconcileDone
			return err
		}
	}

	// --- Shutdown: unready -> stop HTTP intake -> cancel schedulers -> drain ->
	//   close runtime connections -> child policy -> close DB -> release lock ---

	// 1. Mark not-ready so /api/ready returns 503 during drain.
	readiness.SetNotReady()
	emit("unready")

	// 2. Stop HTTP intake and drain in-flight requests. server.Shutdown
	//    closes the listener (stopping new connections) and waits for active
	//    requests to finish. On the force path (second signal), server.Close
	//    immediately closes all connections without draining.
	//    "stop-http-intake" and "drain" are both handled by Shutdown/Close;
	//    we emit the intake event first, then perform the drain, then emit
	//    drain-done.
	emit("stop-http-intake")

	// 3. Cancel reconciliation (if still running) and stop schedulers BEFORE
	//    draining so background work does not touch the DB during drain.
	cancelReconcile()
	<-reconcileDone
	<-schedulersStarted
	schedulers.Stop()

	// 4. Drain in-flight HTTP requests (graceful) or force-close (second signal).
	if force {
		_ = server.Close()
	} else {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
		cancel()
	}
	emit("drain-done")

	// 5. Close runtime connections hook (device client, monitor). No-op when
	//    the deps don't expose a RuntimeConnections closer. This is
	//    documented as a no-op for degraded builds.
	if rc, ok := extractRuntimeConnections(deps); ok {
		_ = rc.Close()
	}
	emit("runtime-connections-closed")

	// 6. Child-process policy: per the legacy Bun SIGINT handler
	//    (src/index.ts lines 75-82), children are LEFT RUNNING on shutdown
	//    (orphaned). Reconciliation picks them up on the next start. The
	//    supervisor's exit callbacks are guarded by the reconcile-done wait
	//    above so they do not race with DB close. No explicit StopAll/KillAll
	//    is invoked — this matches the legacy policy.
	emit("child-policy")
	//
	//    NOTE: if a future policy requires stopping children on shutdown,
	//    iterate running instances and call lifecycle.Stop here, BEFORE DB
	//    close, and wait for supervisor operations to quiesce.

	// Collect the server error from errCh. By this point server.Shutdown or
	// server.Close has returned, which means server.Serve has also returned
	// and errCh is guaranteed to have a value.
	serverErr := <-errCh

	// 7-8. Close DB and release lock, collecting all errors via errors.Join
	//      so a failure in one step does not skip later cleanup.
	var errs []error
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		errs = append(errs, serverErr)
	}
	if err := dbCloser.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close database: %w", err))
	}
	closeDB = false
	if err := lock.Release(); err != nil {
		errs = append(errs, fmt.Errorf("release lock: %w", err))
	}
	releaseLock = false
	return errors.Join(errs...)
}

type httpDepsOptions struct {
	DB      *database.DB
	DataDir string
	Logger  *slog.Logger
}

func buildHTTPDeps(_ context.Context, opts httpDepsOptions) (httpapi.Dependencies, error) {
	if opts.DB == nil || opts.DB.SQL == nil {
		return httpapi.Dependencies{}, errors.New("database handle is required")
	}
	repo := instances.NewSQLiteRepository(opts.DB.SQL)
	filesystem, err := instances.NewFilesystem(opts.DataDir)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	portAllocator := system.NewPortAllocator(repo)
	deviceClient := instances.NewDeviceClient(instances.DeviceClientOptions{})
	processMonitor := monitoring.New(monitoring.MonitorOptions{})
	releases := versions.NewGitHubClient("", nil)
	versionService := versions.NewService(opts.DataDir, releases)
	versionInstaller := versions.NewInstaller(opts.DataDir, releases, nil)
	lifecycleCallbacks := appLifecycleCallbacks{repo: repo, cache: deviceClient, monitor: processMonitor}
	processSupervisor := supervisor.New(supervisor.SupervisorConfig{StatusCallback: lifecycleCallbacks.PersistSupervisorStatus, ExitCallback: lifecycleCallbacks.PersistSupervisorExit})
	lifecycle := instances.NewLifecycleService(instances.LifecycleOptions{Repository: repo, Filesystem: filesystem, PortAllocator: portAllocator, PortChecker: appPortChecker{}, VersionResolver: appVersionResolver{service: versionService}, Supervisor: processSupervisor, DeviceCache: deviceClient, Monitor: processMonitor})
	instanceService := instances.NewService(repo, filesystem, portAllocator, appServiceLifecycle{service: lifecycle}, instances.WithDeviceCacheCleaner(deviceClient), instances.WithMonitorCacheCleaner(processMonitor))

	// Build the auto-update scheduler service. It is wired into deps.AutoUpdate
	// so the HTTP /api/system/auto-update/* routes surface real status, and
	// started/stopped by the lifecycle orchestrator (buildDefaultSchedulers).
	autoupdate := scheduler.NewAutoUpdate(scheduler.AutoUpdateOptions{
		Versions:  versionServiceAdapter{service: versionService},
		Installer: versionInstaller,
		Lister:    repo,
		Restarter: lifecycleRestarterAdapter{service: lifecycle},
		Logger:    opts.Logger,
	})

	return httpapi.Dependencies{
		Logger:            opts.Logger,
		StaticFS:          staticassets.FS(),
		Instances:         instanceService,
		InstanceLifecycle: appHTTPLifecycle{service: lifecycle},
		DeviceClient:      deviceClient,
		ConnectionTester:  instances.NewConnectionTester(instances.ConnectionTesterOptions{}),
		AdminLinkIssuer:   runtimeNotReadyAdminLinks{},
		System:            system.NewSystemService(repo, opts.DataDir, buildinfo.DisplayVersion()),
		PortAllocator:     portAllocator,
		PortChecker:       appPortChecker{},
		AutoUpdate:        autoupdate,
		Versions:          versionServiceAdapter{service: versionService},
		VersionInstaller:  versionInstaller,
		InstanceDirResolver: filesystem,
	}, nil
}

func dbFromCloser(closer Closer) (*database.DB, bool) {
	if db, ok := closer.(*database.DB); ok {
		return db, true
	}
	if handle, ok := closer.(DBHandle); ok {
		return handle.SQLDB(), true
	}
	return nil, false
}

type appHTTPLifecycle struct{ service *instances.LifecycleService }

func (a appHTTPLifecycle) Start(ctx context.Context, id int64) (httpapi.InstanceStatus, error) {
	return toHTTPInstanceStatus(a.service.Start(ctx, id))
}

func (a appHTTPLifecycle) Stop(ctx context.Context, id int64) (httpapi.InstanceStatus, error) {
	return toHTTPInstanceStatus(a.service.Stop(ctx, id))
}

func (a appHTTPLifecycle) Kill(ctx context.Context, id int64) (httpapi.InstanceStatus, error) {
	return toHTTPInstanceStatus(a.service.Kill(ctx, id))
}

func (a appHTTPLifecycle) Restart(ctx context.Context, id int64) (httpapi.InstanceStatus, error) {
	return toHTTPInstanceStatus(a.service.Restart(ctx, id))
}

func (a appHTTPLifecycle) Status(ctx context.Context, id int64) (httpapi.InstanceStatus, error) {
	return toHTTPInstanceStatus(a.service.Status(ctx, id))
}

type appServiceLifecycle struct{ service *instances.LifecycleService }

type appLifecycleCallbacks struct {
	repo    instances.Repository
	cache   instances.DeviceCacheCleaner
	monitor instances.ProcessMonitor
}

func (a appLifecycleCallbacks) service() *instances.LifecycleService {
	return instances.NewLifecycleService(instances.LifecycleOptions{Repository: a.repo, DeviceCache: a.cache, Monitor: a.monitor})
}

func (a appLifecycleCallbacks) PersistSupervisorStatus(ctx context.Context, snapshot supervisor.ProcessSnapshot) error {
	return a.service().PersistSupervisorStatus(ctx, snapshot)
}

func (a appLifecycleCallbacks) PersistSupervisorExit(snapshot supervisor.ProcessSnapshot) {
	a.service().PersistSupervisorExit(snapshot)
}

func (a appServiceLifecycle) Stop(ctx context.Context, id int64) (instances.Status, error) {
	status, err := a.service.Stop(ctx, id)
	return instances.Status{State: status.Status}, err
}

func (a appServiceLifecycle) Status(ctx context.Context, id int64) (instances.Status, error) {
	status, err := a.service.Status(ctx, id)
	return instances.Status{State: status.Status}, err
}

func toHTTPInstanceStatus(status instances.LifecycleStatus, err error) (httpapi.InstanceStatus, error) {
	return httpapi.InstanceStatus{ID: status.ID, Name: status.Name, Status: status.Status, Port: status.Port, PID: status.PID, Uptime: status.Uptime, Resources: status.Resources}, err
}

type runtimeNotReadyAdminLinks struct{}

func (runtimeNotReadyAdminLinks) CreateAdminLink(context.Context, instances.Instance) (httpapi.AdminLink, error) {
	return httpapi.AdminLink{}, instances.ErrRuntimeNotReady
}

type appPortChecker struct{}

func (appPortChecker) IsPortAvailable(port int) bool { return system.IsPortAvailable(port) }

type appVersionResolver struct{ service *versions.Service }

func (a appVersionResolver) ResolveVersionPath(_ context.Context, version string) (string, error) {
	return a.service.GetVersionBinaryPathSafe(version)
}

type versionServiceAdapter struct{ service *versions.Service }

func (a versionServiceAdapter) GetInstalledVersions() ([]versions.VersionInfo, error) {
	return a.service.GetInstalledVersions()
}

func (a versionServiceAdapter) GetAvailableVersions(ctx context.Context, limit int) ([]versions.VersionInfo, error) {
	return a.service.GetAvailableVersions(ctx, limit)
}

func (a versionServiceAdapter) IsVersionAvailable(ctx context.Context, version string) (bool, error) {
	return a.service.IsVersionAvailable(ctx, version)
}

func (a versionServiceAdapter) GetVersionBinaryPath(version string) string {
	return a.service.GetVersionBinaryPath(version)
}

func (a versionServiceAdapter) GetVersionsSize() (map[string]int64, error) {
	return a.service.GetVersionsSize()
}

func (a versionServiceAdapter) RemoveVersion(_ context.Context, version string) error {
	return a.service.RemoveVersion(version)
}

func (a versionServiceAdapter) Cleanup(_ context.Context, keepCount int) ([]string, error) {
	return a.service.Cleanup(keepCount)
}

// lifecycleRestarterAdapter adapts instances.LifecycleService.Restart, which
// returns (LifecycleStatus, error), to the scheduler.InstanceRestarter
// interface that returns only error.
type lifecycleRestarterAdapter struct{ service *instances.LifecycleService }

func (a lifecycleRestarterAdapter) Restart(ctx context.Context, id int64) error {
	_, err := a.service.Restart(ctx, id)
	return err
}

// noopSchedulers is a no-op Schedulers used when scheduler construction fails
// or when deps lack the required interfaces (degraded builds).
type noopSchedulers struct{}

func (noopSchedulers) Start(context.Context) error { return nil }
func (noopSchedulers) Stop()                       {}

// defaultSchedulers wires the cleanup runner and auto-update service from the
// HTTP dependencies. It is returned by buildDefaultSchedulers and started
// after reconciliation completes.
type defaultSchedulers struct {
	cleanupRunner *scheduler.Runner
	autoUpdate    *scheduler.AutoUpdate
	interval      time.Duration
	startCtx      context.Context
	mu            sync.Mutex
	started       bool
}

func (d *defaultSchedulers) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return nil
	}
	d.startCtx = ctx
	d.cleanupRunner.Start(ctx)
	if d.autoUpdate != nil {
		d.autoUpdate.Start(ctx, d.interval)
	}
	d.started = true
	return nil
}

func (d *defaultSchedulers) Stop() {
	d.mu.Lock()
	started := d.started
	d.mu.Unlock()
	if !started {
		return
	}
	d.cleanupRunner.Stop()
	if d.autoUpdate != nil {
		d.autoUpdate.Stop()
	}
}

// buildDefaultSchedulers returns a BuildSchedulers func that constructs the
// cleanup runner (daily midnight UTC) and auto-update service from the wired
// dependencies. If deps.AutoUpdate is already a *scheduler.AutoUpdate (set by
// buildHTTPDeps), it is reused so the HTTP routes and the scheduler share the
// same instance. Otherwise a new one is built from the deps interfaces. When
// the deps lack the required interfaces, a noopSchedulers is returned.
func buildDefaultSchedulers(interval time.Duration) func(context.Context, httpapi.Dependencies) (Schedulers, error) {
	if interval <= 0 {
		interval = time.Hour // default: 1 hour, matching legacy AutoUpdater.start(60*60*1000)
	}
	return func(ctx context.Context, deps httpapi.Dependencies) (Schedulers, error) {
		if deps.Instances == nil {
			return noopSchedulers{}, nil
		}
		lister := depsLister{svc: deps.Instances}

		// Cleanup runner: daily midnight UTC.
		var cleanupJob scheduler.Job
		if deps.Instances != nil {
			// Prefer the real filesystem resolver wired by buildHTTPDeps;
			// fall back to a nil-safe resolver only when deps don't
			// expose one (degraded builds).
			resolver := scheduler.DirResolver(nilResolver{})
			if deps.InstanceDirResolver != nil {
				resolver = depsDirResolver{svc: deps.InstanceDirResolver}
			}
			cleanup := scheduler.NewCleanup(scheduler.CleanupOptions{
				Lister:   lister,
				Resolver: resolver,
				Logger:   deps.Logger,
			})
			cleanupJob = func(ctx context.Context) error {
				_, err := cleanup.Run(ctx)
				return err
			}
		}
		cleanupRunner := scheduler.NewRunner(scheduler.RunnerOptions{
			Job:      cleanupJob,
			Schedule: scheduler.DailyMidnightUTC,
			Logger:   deps.Logger,
		})

		// Auto-update: reuse the one wired in deps.AutoUpdate if possible.
		var autoUpdate *scheduler.AutoUpdate
		if au, ok := deps.AutoUpdate.(*scheduler.AutoUpdate); ok {
			autoUpdate = au
		} else if deps.Versions != nil && deps.VersionInstaller != nil && deps.InstanceLifecycle != nil {
			autoUpdate = scheduler.NewAutoUpdate(scheduler.AutoUpdateOptions{
				Versions:  depsVersionLister{svc: deps.Versions},
				Installer: depsInstaller{svc: deps.VersionInstaller},
				Lister:    lister,
				Restarter: depsRestarter{svc: deps.InstanceLifecycle},
				Logger:    deps.Logger,
			})
		}

		return &defaultSchedulers{
			cleanupRunner: cleanupRunner,
			autoUpdate:    autoUpdate,
			interval:      interval,
		}, nil
	}
}

// nilResolver is a DirResolver that always returns an error, used when the
// httpapi deps do not expose a filesystem resolver. The cleanup job logs the
// error per instance and continues (no-op).
type nilResolver struct{}

func (nilResolver) InstanceDir(id int64) (string, error) {
	return "", fmt.Errorf("no directory resolver available")
}

// depsDirResolver adapts httpapi.InstanceDirResolver to scheduler.DirResolver.
type depsDirResolver struct{ svc httpapi.InstanceDirResolver }

func (d depsDirResolver) InstanceDir(id int64) (string, error) {
	return d.svc.InstanceDir(id)
}

// depsVersionLister adapts httpapi.VersionService to scheduler.VersionLister.
type depsVersionLister struct{ svc httpapi.VersionService }

func (d depsVersionLister) GetAvailableVersions(ctx context.Context, limit int) ([]versions.VersionInfo, error) {
	return d.svc.GetAvailableVersions(ctx, limit)
}

// depsInstaller adapts httpapi.VersionInstaller to scheduler.VersionInstaller.
type depsInstaller struct{ svc httpapi.VersionInstaller }

func (d depsInstaller) Install(ctx context.Context, version string) (versions.InstallResult, error) {
	return d.svc.Install(ctx, version)
}

// depsRestarter adapts httpapi.InstanceLifecycle to scheduler.InstanceRestarter.
type depsRestarter struct{ svc httpapi.InstanceLifecycle }

func (d depsRestarter) Restart(ctx context.Context, id int64) error {
	_, err := d.svc.Restart(ctx, id)
	return err
}

// extractRuntimeConnections attempts to extract a RuntimeConnections closer
// from the deps. The device client and process monitor both expose cleanup
// methods, but the httpapi deps interfaces do not expose a generic Close. In
// the current wiring this returns false (no-op), documented as such. When a
// future deps field exposes a closeable runtime connection, wire it here.
func extractRuntimeConnections(deps httpapi.Dependencies) (RuntimeConnections, bool) {
	_ = deps
	return nil, false
}

func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	return ctx, stop
}

func listenFirstAvailable(listen func(network, address string) (net.Listener, error), startPort int) (net.Listener, error) {
	if startPort == 0 {
		return listen("tcp", "127.0.0.1:0")
	}
	var lastErr error
	for port := startPort; port < startPort+100 && port <= 65535; port++ {
		ln, err := listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no available port found from %d through %d: %w", startPort, startPort+99, lastErr)
}

// depsLister adapts httpapi.InstanceService to the reconciler's InstanceLister.
type depsLister struct{ svc httpapi.InstanceService }

func (d depsLister) List(ctx context.Context) ([]instances.Instance, error) {
	return d.svc.List(ctx)
}

// depsStarter adapts httpapi.InstanceLifecycle to the reconciler's
// InstanceStarter. A non-nil error from Start indicates the instance could
// not be restarted; the underlying LifecycleService persists a "failed"
// status in that case.
type depsStarter struct{ svc httpapi.InstanceLifecycle }

func (d depsStarter) Start(ctx context.Context, id int64) error {
	_, err := d.svc.Start(ctx, id)
	return err
}

// runReconciliation builds and runs the startup reconciler from the wired
// dependencies. When instance management dependencies are absent (e.g. a
// degraded build), it returns immediately; the caller is responsible for
// flipping readiness after schedulers start.
func runReconciliation(ctx context.Context, deps httpapi.Dependencies, logger *slog.Logger, concurrency int, readiness *httpapi.AtomicReadiness) {
	if deps.Instances == nil || deps.InstanceLifecycle == nil {
		// Degraded build: no instance management. The caller (reconcile
		// goroutine) flips readiness after schedulers start, so we do
		// NOT flip it here — readiness may be nil in that path.
		return
	}
	r := NewReconciler(ReconcilerOptions{
		Lister:      depsLister{svc: deps.Instances},
		Starter:     depsStarter{svc: deps.InstanceLifecycle},
		Logger:      logger,
		Concurrency: concurrency,
		Readiness:   readiness,
	})
	if err := r.Reconcile(ctx); err != nil {
		logger.Error("startup reconciliation completed with error", "error", err)
	}
}
