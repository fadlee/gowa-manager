package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// TestRunSmokeFakeGOWA is a simplified native smoke test (Task 11 Step 3).
//
// It exercises the full Run lifecycle with a real SQLite database and a real
// fake GOWA binary (internal/testutil/fakegowa):
//  1. Build fakegowa and install it as version "v1.0.0".
//  2. Start the manager via Run.
//  3. Create an instance via the HTTP API and start it.
//  4. Verify the instance reaches "running" status.
//  5. Terminate the manager (cancel context) — per legacy policy, children
//     are left running (orphaned).
//  6. Verify DB integrity after shutdown.
//  7. Restart the manager and verify reconciliation restarts the instance.
//
// Simplification: the test does NOT verify that the child process is still
// alive after the first manager exit (the legacy policy orphans children, but
// verifying orphan liveness is platform-flaky in CI). Instead it verifies DB
// integrity and that reconciliation marks the instance running again on
// restart. The fakegowa binary from the first run is terminated before the
// reconciliation check to avoid port conflicts.
func TestRunSmokeFakeGOWA(t *testing.T) {
	if testing.Short() {
		t.Skip("smoke test requires building fakegowa binary")
	}
	ctx := context.Background()
	dataDir := t.TempDir()

	// 1. Build and install fakegowa as version v1.0.0.
	binaryPath := installFakeGOWA(t, dataDir)

	// 2. Start the manager.
	runCtx1, cancel1 := context.WithCancel(context.Background())
	addr1, errCh1 := startManager(t, runCtx1, dataDir)

	// 3. Create an instance via the API and start it.
	created := createInstanceViaHTTP(t, addr1, "smoke-instance")
	startInstanceViaHTTP(t, addr1, created.ID)

	// 4. Verify the instance reaches "running".
	waitForInstanceStatus(t, addr1, created.ID, "running", 10*time.Second)

	// 5. Terminate the manager (graceful shutdown via context cancel).
	cancel1()
	if err := <-errCh1; err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	// 6. Verify DB integrity after shutdown.
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatalf("DB integrity after shutdown: %v", err)
	}
	repo := instances.NewSQLiteRepository(db.SQL)
	item, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID after shutdown: %v", err)
	}
	if item.Status != "running" {
		t.Fatalf("instance status after shutdown = %q, want running (persisted for reconciliation)", item.Status)
	}
	_ = db.Close()

	// Clean up any orphaned fakegowa process from the first run to avoid port
	// conflicts during reconciliation. In production, children are left
	// running; here we terminate them for test isolation.
	cleanupOrphanedGOWA(t, binaryPath)

	// 7. Restart the manager and verify reconciliation restarts the instance.
	runCtx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	addr2, errCh2 := startManager(t, runCtx2, dataDir)
	_ = addr2

	// Reconciliation should restart the instance. Poll until running.
	waitForInstanceStatusViaRepo(t, dataDir, created.ID, "running", 15*time.Second)

	cancel2()
	if err := <-errCh2; err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	// Final cleanup.
	cleanupOrphanedGOWA(t, binaryPath)
}

// installFakeGOWA builds the fakegowa binary and installs it at the version
// path expected by the version resolver: {dataDir}/bin/versions/v1.0.0/gowa[.exe].
func installFakeGOWA(t *testing.T, dataDir string) string {
	t.Helper()
	binaryName := "gowa"
	if runtime.GOOS == "windows" {
		binaryName = "gowa.exe"
	}
	versionDir := filepath.Join(dataDir, "bin", "versions", "v1.0.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(versionDir, binaryName)

	// Build the fakegowa binary from the testutil package.
	source := "github.com/fadlee/gowa-manager/internal/testutil/fakegowa"
	cmd := exec.Command("go", "build", "-o", binaryPath, source)
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fakegowa: %v\n%s", err, out)
	}
	return binaryPath
}

// startManager launches Run in a goroutine and returns the listen address and
// an error channel. It uses the real buildHTTPDeps and default schedulers.
func startManager(t *testing.T, ctx context.Context, dataDir string) (string, <-chan error) {
	t.Helper()
	started := make(chan struct{})
	var addr string
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			Config: config.Config{Port: 0, DataDir: dataDir},
			Logger: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
			OpenDB: func(_ context.Context, _ string) (Closer, error) {
				return database.Open(context.Background(), dataDir)
			},
			OnStarted: func(a string) {
				addr = a
				close(started)
			},
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("manager did not start within 5s")
	}
	// Wait for readiness so the API is fully wired.
	waitForReadyHTTP(t, addr, 10*time.Second)
	return addr, errCh
}

// waitForReadyHTTP polls /api/ready until it returns 200.
func waitForReadyHTTP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/api/ready")
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return
			}
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("/api/ready did not return 200 within %v", timeout)
}

type smokeInstanceResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	GOWAVersion string `json:"gowa_version"`
}

func createInstanceViaHTTP(t *testing.T, addr, name string) smokeInstanceResponse {
	t.Helper()
	body := bytes.NewBufferString(`{"name":"` + name + `","gowa_version":"v1.0.0"}`)
	resp, err := http.Post("http://"+addr+"/api/instances", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create instance status = %d", resp.StatusCode)
	}
	var created smokeInstanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func startInstanceViaHTTP(t *testing.T, addr string, id int64) {
	t.Helper()
	resp, err := http.Post("http://"+addr+"/api/instances/"+fmt.Sprintf("%d", id)+"/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start instance status = %d", resp.StatusCode)
	}
}

func waitForInstanceStatus(t *testing.T, addr string, id int64, want string, timeout time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/api/instances/" + fmt.Sprintf("%d", id) + "/status")
		if err == nil {
			var status struct {
				Status string `json:"status"`
			}
			if json.NewDecoder(resp.Body).Decode(&status) == nil && status.Status == want {
				resp.Body.Close()
				return
			}
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("instance %d did not reach status %q within %v", id, want, timeout)
}

func waitForInstanceStatusViaRepo(t *testing.T, dataDir string, id int64, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		db, err := database.Open(context.Background(), dataDir)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		repo := instances.NewSQLiteRepository(db.SQL)
		item, err := repo.FindByID(context.Background(), id)
		db.Close()
		if err == nil && item.Status == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("instance %d did not reach status %q via repo within %v", id, want, timeout)
}

// cleanupOrphanedGOWA kills any running fakegowa processes to ensure test
// isolation. In production, children are left running per legacy policy.
func cleanupOrphanedGOWA(t *testing.T, binaryPath string) {
	t.Helper()
	// On all platforms, try to kill processes running the fakegowa binary.
	// This is best-effort; failures are ignored.
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/IM", filepath.Base(binaryPath), "/F").Run()
	} else {
		_ = exec.Command("pkill", "-f", filepath.Base(binaryPath)).Run()
	}
}

// projectRoot returns the repository root by searching upwards from the test
// file location for a go.mod file.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find project root (go.mod)")
	return ""
}
