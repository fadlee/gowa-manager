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
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
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

func TestBuildHTTPDepsLifecycleRoutesReturnRuntimeNotReady(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/instances/"+int64Text(created.ID)+"/start", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("start status = %d body = %s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(instances.ErrRuntimeNotReady.Error())) {
		t.Fatalf("start body = %s, want runtime-not-ready error", resp.Body.String())
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
