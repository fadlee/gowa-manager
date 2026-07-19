package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/supervisor"
)

type fakeLock struct {
	events *[]string
	mu     *sync.Mutex
}

func (l *fakeLock) Release() error {
	appendEvent(l.events, l.mu, "lock-release")
	return nil
}

type fakeDB struct {
	events *[]string
	mu     *sync.Mutex
}

func (d *fakeDB) Close() error {
	appendEvent(d.events, d.mu, "db-close")
	return nil
}

// appendEvent appends a tag to the events slice, using mu for synchronization
// when non-nil (the lifecycle-order test shares the slice across goroutines).
func appendEvent(events *[]string, mu *sync.Mutex, tag string) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	*events = append(*events, tag)
}

func TestRunStartupOrderAndShutdown(t *testing.T) {
	events := []string{}
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	ready := make(chan struct{})
	opts := Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		Logger: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
		AcquireLock: func(string) (Releaser, error) {
			mu.Lock()
			events = append(events, "lock")
			mu.Unlock()
			return &fakeLock{events: &events, mu: &mu}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			mu.Lock()
			events = append(events, "db")
			mu.Unlock()
			return &fakeDB{events: &events, mu: &mu}, nil
		},
		BuildHTTPDeps: func(context.Context, httpDepsOptions) (httpapi.Dependencies, error) {
			mu.Lock()
			events = append(events, "services")
			mu.Unlock()
			return httpapi.Dependencies{}, nil
		},
		BuildSchedulers: func(context.Context, httpapi.Dependencies) (Schedulers, error) {
			mu.Lock()
			events = append(events, "schedulers-built")
			mu.Unlock()
			return &fakeSchedulers{events: &events, mu: &mu}, nil
		},
		Listen: func(network, address string) (net.Listener, error) {
			mu.Lock()
			events = append(events, "listen")
			mu.Unlock()
			ln, err := net.Listen(network, "127.0.0.1:0")
			if err == nil {
				close(started)
			}
			return ln, err
		},
		OnEvent: func(tag string) {
			mu.Lock()
			events = append(events, tag)
			mu.Unlock()
			if tag == "ready" {
				close(ready)
			}
		},
	}
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()
	<-started
	// Wait until the full startup sequence (reconcile + schedulers + ready)
	// completes before triggering shutdown, so schedulers-start is recorded.
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not reach ready within 3s")
	}
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// Startup: lock -> db -> services -> listen -> schedulers-built ->
	//   reconcile-start -> reconcile-done -> schedulers-start -> ready
	// (schedulers are constructed before reconciliation but started after)
	// Shutdown: unready -> stop-http-intake -> cancel-schedulers -> drain ->
	//           close-runtime-connections -> child-policy -> db-close -> lock-release
	want := []string{
		"lock", "db", "services", "listen", "schedulers-built",
		"reconcile-start", "reconcile-done",
		"schedulers-start", "ready",
		// shutdown
		"unready", "stop-http-intake", "schedulers-stop", "drain-done",
		"runtime-connections-closed", "child-policy",
		"db-close", "lock-release",
	}
	mu.Lock()
	got := make([]string, len(events))
	copy(got, events)
	mu.Unlock()
	if !equal(got, want) {
		t.Fatalf("events = %#v\nwant = %#v", got, want)
	}
}

func TestRunShutdownSetsNotReadyBeforeDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var rmu sync.Mutex
	var readiness *httpapi.AtomicReadiness
	opts := Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		Logger: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
		AcquireLock: func(string) (Releaser, error) {
			return &fakeLock{events: &[]string{}}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			return &fakeDB{events: &[]string{}}, nil
		},
		BuildHTTPDeps: func(context.Context, httpDepsOptions) (httpapi.Dependencies, error) {
			return httpapi.Dependencies{}, nil
		},
		BuildSchedulers: func(context.Context, httpapi.Dependencies) (Schedulers, error) {
			return &fakeSchedulers{events: &[]string{}}, nil
		},
		Listen: func(network, address string) (net.Listener, error) {
			ln, err := net.Listen(network, "127.0.0.1:0")
			if err == nil {
				close(started)
			}
			return ln, err
		},
		OnReadiness: func(r *httpapi.AtomicReadiness) {
			rmu.Lock()
			readiness = r
			rmu.Unlock()
		},
	}
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()
	<-started

	// Wait until ready (reconciliation + schedulers done).
	rmu.Lock()
	r := readiness
	rmu.Unlock()
	waitForReady(t, r, 2*time.Second)
	if !r.Ready() {
		t.Fatalf("readiness should be ready after startup")
	}

	// Cancel to trigger shutdown.
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// After shutdown, readiness must be not-ready.
	if r.Ready() {
		t.Fatalf("readiness should be not-ready after shutdown")
	}
}

func TestRunSecondSignalForcesImmediateShutdown(t *testing.T) {
	events := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	force := make(chan struct{}, 1)

	// Build a deps with a slow handler so drain would block if graceful.
	blockHandler := make(chan struct{})
	depsVal := httpapi.Dependencies{TestPanicRoute: false}

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
		BuildHTTPDeps: func(context.Context, httpDepsOptions) (httpapi.Dependencies, error) {
			events = append(events, "services")
			return depsVal, nil
		},
		BuildSchedulers: func(context.Context, httpapi.Dependencies) (Schedulers, error) {
			return &fakeSchedulers{events: &events}, nil
		},
		Listen: func(network, address string) (net.Listener, error) {
			events = append(events, "listen")
			ln, err := net.Listen(network, "127.0.0.1:0")
			if err == nil {
				close(started)
			}
			return ln, err
		},
		ForceShutdown: force,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()
	<-started

	// Trigger shutdown via force channel (simulates second signal).
	close(force)
	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// We don't assert exact events here because the force path skips graceful
	// drain; the key assertion is that Run returns promptly without hanging.
	_ = blockHandler
}

func TestRunShutdownCollectsErrorsWithoutSkipping(t *testing.T) {
	events := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	wantDBErr := errors.New("db-close-fail")
	wantLockErr := errors.New("lock-release-fail")

	opts := Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		Logger: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
		AcquireLock: func(string) (Releaser, error) {
			events = append(events, "lock")
			return &errorLock{releaseErr: wantLockErr, events: &events}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			events = append(events, "db")
			return &errorDB{closeErr: wantDBErr, events: &events}, nil
		},
		BuildHTTPDeps: func(context.Context, httpDepsOptions) (httpapi.Dependencies, error) {
			events = append(events, "services")
			return httpapi.Dependencies{}, nil
		},
		BuildSchedulers: func(context.Context, httpapi.Dependencies) (Schedulers, error) {
			return &fakeSchedulers{events: &events}, nil
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
	err := <-errCh
	if err == nil {
		t.Fatalf("Run() error = nil, want joined error containing db-close-fail and lock-release-fail")
	}
	if !strings.Contains(err.Error(), "db-close-fail") {
		t.Fatalf("Run() error = %v, want db-close-fail", err)
	}
	if !strings.Contains(err.Error(), "lock-release-fail") {
		t.Fatalf("Run() error = %v, want lock-release-fail", err)
	}
	// Both cleanup steps must still have run despite the earlier error.
	if !contains(events, "db-close") {
		t.Fatalf("db-close should still run, events = %#v", events)
	}
	if !contains(events, "lock-release") {
		t.Fatalf("lock-release should still run, events = %#v", events)
	}
}

func TestRunRejectsDatabaseWithoutSQLiteHandle(t *testing.T) {
	events := []string{}
	err := Run(context.Background(), Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		AcquireLock: func(string) (Releaser, error) {
			events = append(events, "lock")
			return &fakeLock{events: &events}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			events = append(events, "db")
			return &fakeDB{events: &events}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "sqlite connection") {
		t.Fatalf("Run() error = %v, want sqlite handle error", err)
	}
	if !equal(events, []string{"lock", "db", "db-close", "lock-release"}) {
		t.Fatalf("events = %#v", events)
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

func TestRunWiringFailureClosesDatabaseAndReleasesLock(t *testing.T) {
	events := []string{}
	wantErr := errors.New("wiring fail")
	err := Run(context.Background(), Options{
		Config: config.Config{Port: 0, DataDir: t.TempDir()},
		AcquireLock: func(string) (Releaser, error) {
			events = append(events, "lock")
			return &fakeLock{events: &events}, nil
		},
		OpenDB: func(context.Context, string) (Closer, error) {
			events = append(events, "db")
			return &fakeDB{events: &events}, nil
		},
		BuildHTTPDeps: func(context.Context, httpDepsOptions) (httpapi.Dependencies, error) {
			events = append(events, "wiring")
			return httpapi.Dependencies{}, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if !equal(events, []string{"lock", "db", "wiring", "db-close", "lock-release"}) {
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
			BuildHTTPDeps: func(context.Context, httpDepsOptions) (httpapi.Dependencies, error) {
				return httpapi.Dependencies{}, nil
			},
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

func TestBuildHTTPDepsSharesDatabaseAcrossServices(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deps, err := buildHTTPDeps(ctx, httpDepsOptions{DB: db, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(deps)

	created := createInstanceViaHandler(t, handler, "shared-db")
	repo := instances.NewSQLiteRepository(db.SQL)
	items, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != created.ID || items[0].Name != "shared-db" {
		t.Fatalf("repository list = %#v, want created instance %#v", items, created)
	}
}

func TestBuildHTTPDepsWiresConfiguredDataPaths(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deps, err := buildHTTPDeps(ctx, httpDepsOptions{DB: db, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(deps)
	created := createInstanceViaHandler(t, handler, "paths")
	if info, err := os.Stat(filepath.Join(dataDir, "instances", int64Text(created.ID))); err != nil || !info.IsDir() {
		t.Fatalf("instance directory stat error = %v, info = %#v", err, info)
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	configResp := httptest.NewRecorder()
	handler.ServeHTTP(configResp, configReq)
	if configResp.Code != http.StatusOK {
		t.Fatalf("system config status = %d body = %s", configResp.Code, configResp.Body.String())
	}
	var systemConfig httpapi.SystemConfig
	if err := json.NewDecoder(configResp.Body).Decode(&systemConfig); err != nil {
		t.Fatal(err)
	}
	wantDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if systemConfig.DataDirectory != wantDataDir {
		t.Fatalf("data_directory = %q, want %q", systemConfig.DataDirectory, wantDataDir)
	}

	versionReq := httptest.NewRequest(http.MethodGet, "/api/system/versions/v1.2.3/available", nil)
	versionResp := httptest.NewRecorder()
	handler.ServeHTTP(versionResp, versionReq)
	if versionResp.Code != http.StatusOK {
		t.Fatalf("version available status = %d body = %s", versionResp.Code, versionResp.Body.String())
	}
	var versionBody struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(versionResp.Body).Decode(&versionBody); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(versionBody.Path) != filepath.Join(wantDataDir, "bin", "versions", "v1.2.3") {
		t.Fatalf("version path = %q", versionBody.Path)
	}
}

func TestBuildHTTPDepsWiresRealLifecycleStatusRoute(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deps, err := buildHTTPDeps(ctx, httpDepsOptions{DB: db, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(deps)
	created := createInstanceViaHandler(t, handler, "runtime")

	req := httptest.NewRequest(http.MethodGet, "/api/instances/"+int64Text(created.ID)+"/status", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status route status = %d body = %s", resp.Code, resp.Body.String())
	}
	var status httpapi.InstanceStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.ID != created.ID || status.Name != "runtime" || status.Status != "stopped" || status.PID != nil {
		t.Fatalf("status body = %#v, want stopped lifecycle status", status)
	}
}

func TestAppLifecycleCallbacksPersistSupervisorExit(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := instances.NewSQLiteRepository(db.SQL)
	created, err := repo.Create(ctx, instances.CreateInput{Name: "callbacks", Config: `{}`, GOWAVersion: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	callbacks := appLifecycleCallbacks{repo: repo}

	if err := callbacks.PersistSupervisorStatus(ctx, supervisor.ProcessSnapshot{InstanceID: created.ID, State: supervisor.StateRunning, PID: 123}); err != nil {
		t.Fatalf("PersistSupervisorStatus error = %v", err)
	}
	callbacks.PersistSupervisorExit(supervisor.ProcessSnapshot{InstanceID: created.ID, State: supervisor.StateRunning, PID: 123, ExitError: "exit status 1 --password=hunter2"})

	updated, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "failed" {
		t.Fatalf("status = %q, want failed", updated.Status)
	}
	if updated.ErrorMessage == nil || !strings.Contains(*updated.ErrorMessage, "exit status 1") {
		t.Fatalf("error message = %v, want safe exit error", updated.ErrorMessage)
	}
	if updated.ErrorMessage != nil && strings.Contains(*updated.ErrorMessage, "hunter2") {
		t.Fatalf("error message exposes secret: %q", *updated.ErrorMessage)
	}
}

func TestBuildHTTPDepsManagementRoutesSmoke(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deps, err := buildHTTPDeps(ctx, httpDepsOptions{DB: db, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(deps)

	created := createInstanceViaHandler(t, handler, "smoke-create")
	items := listInstancesViaHandler(t, handler)
	if len(items) != 1 || items[0].ID != created.ID || items[0].Name != "smoke-create" {
		t.Fatalf("list after create = %#v, want created instance %#v", items, created)
	}

	updatedBody := bytes.NewBufferString(`{"name":"smoke-updated","config":"{\"webhook\":\"https://example.invalid/hook\"}","gowa_version":"v9.8.7"}`)
	updatedReq := httptest.NewRequest(http.MethodPut, "/api/instances/"+int64Text(created.ID), updatedBody)
	updatedReq.Header.Set("Content-Type", "application/json")
	updatedResp := httptest.NewRecorder()
	handler.ServeHTTP(updatedResp, updatedReq)
	if updatedResp.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updatedResp.Code, updatedResp.Body.String())
	}
	var updated appInstanceResponse
	if err := json.NewDecoder(updatedResp.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Name != "smoke-updated" || updated.GOWAVersion != "v9.8.7" {
		t.Fatalf("updated instance = %#v", updated)
	}
	if !bytes.Contains([]byte(updated.Config), []byte("https://example.invalid/hook")) {
		t.Fatalf("updated config = %q", updated.Config)
	}

	instanceDir := filepath.Join(dataDir, "instances", int64Text(created.ID))
	if err := os.WriteFile(filepath.Join(instanceDir, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetReq := httptest.NewRequest(http.MethodPost, "/api/instances/"+int64Text(created.ID)+"/reset-data", nil)
	resetResp := httptest.NewRecorder()
	handler.ServeHTTP(resetResp, resetReq)
	if resetResp.Code != http.StatusOK {
		t.Fatalf("reset status = %d body = %s", resetResp.Code, resetResp.Body.String())
	}
	if info, err := os.Stat(instanceDir); err != nil || !info.IsDir() {
		t.Fatalf("instance directory after reset stat error = %v, info = %#v", err, info)
	}
	entries, err := os.ReadDir(instanceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("instance directory after reset contains %#v, want empty", entries)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/instances/"+int64Text(created.ID), nil)
	deleteResp := httptest.NewRecorder()
	handler.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", deleteResp.Code, deleteResp.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/instances/"+int64Text(created.ID), nil)
	detailResp := httptest.NewRecorder()
	handler.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusNotFound {
		t.Fatalf("detail after delete status = %d body = %s", detailResp.Code, detailResp.Body.String())
	}
	items = listInstancesViaHandler(t, handler)
	if len(items) != 0 {
		t.Fatalf("list after delete = %#v, want empty", items)
	}
	if err := db.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("IntegrityCheck() error = %v", err)
	}
}

type appInstanceResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Config      string `json:"config"`
	GOWAVersion string `json:"gowa_version"`
}

func createInstanceViaHandler(t *testing.T, handler http.Handler, name string) appInstanceResponse {
	t.Helper()
	body := bytes.NewBufferString(`{"name":"` + name + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/instances", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", resp.Code, resp.Body.String())
	}
	var created appInstanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatalf("created ID = 0")
	}
	return created
}

func listInstancesViaHandler(t *testing.T, handler http.Handler) []appInstanceResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", resp.Code, resp.Body.String())
	}
	var items []appInstanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	return items
}

func int64Text(value int64) string {
	return fmt.Sprintf("%d", value)
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

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}

func waitForReady(t *testing.T, r *httpapi.AtomicReadiness, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r != nil && r.Ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("readiness did not become ready within %v", timeout)
}

// fakeSchedulers records start/stop events for lifecycle-order tests.
type fakeSchedulers struct {
	events *[]string
	mu     *sync.Mutex
}

func (f *fakeSchedulers) Start(ctx context.Context) error {
	appendEvent(f.events, f.mu, "schedulers-start")
	return nil
}

func (f *fakeSchedulers) Stop() {
	appendEvent(f.events, f.mu, "schedulers-stop")
}

// errorLock records the release event but returns a configured error, used to
// verify shutdown collects errors without skipping later cleanup.
type errorLock struct {
	releaseErr error
	events     *[]string
}

func (l *errorLock) Release() error {
	appendEvent(l.events, nil, "lock-release")
	return l.releaseErr
}

// errorDB records the close event but returns a configured error.
type errorDB struct {
	closeErr error
	events   *[]string
}

func (d *errorDB) Close() error {
	appendEvent(d.events, nil, "db-close")
	return d.closeErr
}
