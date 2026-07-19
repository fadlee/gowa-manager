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
	"syscall"
	"time"

	"github.com/fadlee/gowa-manager/internal/buildinfo"
	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/ownership"
	staticassets "github.com/fadlee/gowa-manager/internal/static"
	"github.com/fadlee/gowa-manager/internal/system"
	"github.com/fadlee/gowa-manager/internal/versions"
)

type Releaser interface{ Release() error }
type Closer interface{ Close() error }

type DBHandle interface {
	Closer
	SQLDB() *database.DB
}

type Options struct {
	Config        config.Config
	Logger        *slog.Logger
	AcquireLock   func(dataDir string) (Releaser, error)
	OpenDB        func(context.Context, string) (Closer, error)
	BuildHTTPDeps func(context.Context, httpDepsOptions) (httpapi.Dependencies, error)
	Listen        func(network, address string) (net.Listener, error)
	OnStarted     func(addr string)
}

func Run(ctx context.Context, opts Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
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
	}

	ln, err := listenFirstAvailable(listen, opts.Config.Port)
	if err != nil {
		return err
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

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if err := dbCloser.Close(); err != nil {
		return err
	}
	closeDB = false
	if err := lock.Release(); err != nil {
		return err
	}
	releaseLock = false
	return nil
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
	lifecycle := runtimeNotReadyLifecycle{}
	serviceLifecycle := runtimeNotReadyInstanceLifecycle{}
	deviceClient := instances.NewDeviceClient(instances.DeviceClientOptions{})
	releases := versions.NewGitHubClient("", nil)
	versionService := versions.NewService(opts.DataDir, releases)
	versionInstaller := versions.NewInstaller(opts.DataDir, releases, nil)
	instanceService := instances.NewService(repo, filesystem, portAllocator, serviceLifecycle, instances.WithDeviceCacheCleaner(deviceClient))
	return httpapi.Dependencies{
		Logger:            opts.Logger,
		StaticFS:          staticassets.FS(),
		Instances:         instanceService,
		InstanceLifecycle: lifecycle,
		DeviceClient:      deviceClient,
		ConnectionTester:  instances.NewConnectionTester(instances.ConnectionTesterOptions{}),
		AdminLinkIssuer:   runtimeNotReadyAdminLinks{},
		System:            system.NewSystemService(repo, opts.DataDir, buildinfo.DisplayVersion()),
		PortAllocator:     portAllocator,
		PortChecker:       appPortChecker{},
		Versions:          versionServiceAdapter{service: versionService},
		VersionInstaller:  versionInstaller,
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

type runtimeNotReadyLifecycle struct{}

func (runtimeNotReadyLifecycle) Start(context.Context, int64) (httpapi.InstanceStatus, error) {
	return httpapi.InstanceStatus{}, instances.ErrRuntimeNotReady
}

func (runtimeNotReadyLifecycle) Stop(context.Context, int64) (httpapi.InstanceStatus, error) {
	return httpapi.InstanceStatus{}, instances.ErrRuntimeNotReady
}

func (runtimeNotReadyLifecycle) Kill(context.Context, int64) (httpapi.InstanceStatus, error) {
	return httpapi.InstanceStatus{}, instances.ErrRuntimeNotReady
}

func (runtimeNotReadyLifecycle) Restart(context.Context, int64) (httpapi.InstanceStatus, error) {
	return httpapi.InstanceStatus{}, instances.ErrRuntimeNotReady
}

func (runtimeNotReadyLifecycle) Status(context.Context, int64) (httpapi.InstanceStatus, error) {
	return httpapi.InstanceStatus{}, instances.ErrRuntimeNotReady
}

type runtimeNotReadyInstanceLifecycle struct{}

func (runtimeNotReadyInstanceLifecycle) Stop(context.Context, int64) (instances.Status, error) {
	return instances.Status{}, instances.ErrRuntimeNotReady
}

func (runtimeNotReadyInstanceLifecycle) Status(context.Context, int64) (instances.Status, error) {
	return instances.Status{}, instances.ErrRuntimeNotReady
}

type runtimeNotReadyAdminLinks struct{}

func (runtimeNotReadyAdminLinks) CreateAdminLink(context.Context, instances.Instance) (httpapi.AdminLink, error) {
	return httpapi.AdminLink{}, instances.ErrRuntimeNotReady
}

type appPortChecker struct{}

func (appPortChecker) IsPortAvailable(port int) bool { return system.IsPortAvailable(port) }

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
