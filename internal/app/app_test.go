package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/config"
)

type fakeLock struct {
	events *[]string
}

func (l *fakeLock) Release() error {
	*l.events = append(*l.events, "lock-release")
	return nil
}

type fakeDB struct {
	events *[]string
}

func (d *fakeDB) Close() error {
	*d.events = append(*d.events, "db-close")
	return nil
}

func TestRunStartupOrderAndShutdown(t *testing.T) {
	events := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	opts := Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		Logger: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
		AcquireLock: func(string) (Releaser, error) {
			events = append(events, "lock")
			return &fakeLock{events: &events}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			events = append(events, "db")
			return &fakeDB{events: &events}, nil
		},
		Listen: func(network, address string) (net.Listener, error) {
			events = append(events, "listen")
			ln, err := net.Listen(network, "127.0.0.1:0")
			if err == nil {
				close(started)
			}
			return ln, err
		},
	}
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()
	<-started
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"lock", "db", "listen", "db-close", "lock-release"}
	if !equal(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestRunDatabaseFailureReleasesLock(t *testing.T) {
	events := []string{}
	wantErr := errors.New("db fail")
	err := Run(context.Background(), Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		AcquireLock: func(string) (Releaser, error) {
			events = append(events, "lock")
			return &fakeLock{events: &events}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			events = append(events, "db")
			return nil, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if !equal(events, []string{"lock", "db", "lock-release"}) {
		t.Fatalf("events = %#v", events)
	}
}

func TestRunFallsBackToNextAvailablePort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var startedPort int
	started := make(chan struct{})
	once := sync.Once{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			Config:      config.Config{Port: port, DataDir: t.TempDir()},
			Logger:      slog.New(slog.NewTextHandler(discardWriter{}, nil)),
			AcquireLock: func(string) (Releaser, error) { return &fakeLock{events: &[]string{}}, nil },
			OpenDB:      func(context.Context, string) (Closer, error) { return &fakeDB{events: &[]string{}}, nil },
			OnStarted: func(addr string) {
				startedPort = parsePortFromAddr(addr)
				once.Do(func() { close(started) })
			},
		})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not start")
	}
	if startedPort != port+1 {
		t.Fatalf("started port = %d, want %d", startedPort, port+1)
	}
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parsePortFromAddr(addr string) int {
	_, portText, _ := net.SplitHostPort(addr)
	var port int
	for _, ch := range portText {
		port = port*10 + int(ch-'0')
	}
	return port
}
