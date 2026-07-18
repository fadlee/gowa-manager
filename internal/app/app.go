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

	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/ownership"
	staticassets "github.com/fadlee/gowa-manager/internal/static"
)

type Releaser interface{ Release() error }
type Closer interface{ Close() error }

type Options struct {
	Config      config.Config
	Logger      *slog.Logger
	AcquireLock func(dataDir string) (Releaser, error)
	OpenDB      func(context.Context, string) (Closer, error)
	Listen      func(network, address string) (net.Listener, error)
	OnStarted   func(addr string)
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

	db, err := openDB(ctx, opts.Config.DataDir)
	if err != nil {
		return err
	}
	closeDB := true
	defer func() {
		if closeDB {
			_ = db.Close()
		}
	}()

	ln, err := listenFirstAvailable(listen, opts.Config.Port)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: httpapi.New(httpapi.Dependencies{Logger: logger, StaticFS: staticassets.FS()})}
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
	if err := db.Close(); err != nil {
		return err
	}
	closeDB = false
	if err := lock.Release(); err != nil {
		return err
	}
	releaseLock = false
	return nil
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
