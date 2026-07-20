package benchmark

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	// modernc.org/sqlite is a pure-Go driver already in go.mod.
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Leak detection thresholds (mirror thresholds.json leakDetection section)
// ---------------------------------------------------------------------------

const (
	leakGoroutineMaxDelta   = 20
	leakRssMaxPercentGrowth = 10.0
	leakFdMaxDelta          = 20

	leakHTTPRequests  = 1000
	leakWSCycles      = 500
	leakProcessCycles = 100

	leakSettleSeconds   = 10
	leakFakeGOWAVersion = "v0.0.1-leak"
)

// ---------------------------------------------------------------------------
// Metrics snapshot
// ---------------------------------------------------------------------------

type leakSnapshot struct {
	goroutines int
	rssBytes   int64
	fds        int // file descriptors (Linux) or handles (Windows); -1 = unavailable
	label      string
}

// queryGoroutines fetches the goroutine count from the Go backend's /metrics
// endpoint (requires GOWA_METRICS_ENABLED=1).
func queryGoroutines(baseURL string) (int, error) {
	resp, err := http.Get(baseURL + "/metrics")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("/metrics returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	// Parse "gowa_goroutines <N>" from Prometheus text format.
	re := regexp.MustCompile(`gowa_goroutines\s+(\d+)`)
	m := re.FindSubmatch(body)
	if m == nil {
		return 0, fmt.Errorf("gowa_goroutines not found in /metrics output (status %d)", resp.StatusCode)
	}
	return strconv.Atoi(string(m[1]))
}

// measureProcessRSS measures the RSS of a process by PID.
// On Windows uses PowerShell Get-Process; on Linux reads /proc/<pid>/status;
// on macOS uses ps. Returns -1 if unavailable.
func measureProcessRSS(pid int) int64 {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("(Get-Process -Id %d).WorkingSet64", pid)).Output()
		if err != nil {
			return -1
		}
		rss, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil || rss <= 0 {
			return -1
		}
		return rss
	}
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return -1
		}
		re := regexp.MustCompile(`VmRSS:\s*(\d+)\s*kB`)
		m := re.FindSubmatch(data)
		if m == nil {
			return -1
		}
		kb, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err != nil {
			return -1
		}
		return kb * 1024
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("ps", "-o", "rss", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return -1
		}
		lines := strings.TrimSpace(string(out))
		parts := strings.Split(lines, "\n")
		if len(parts) < 2 {
			return -1
		}
		rss, err := strconv.ParseInt(strings.TrimSpace(parts[len(parts)-1]), 10, 64)
		if err != nil {
			return -1
		}
		return rss * 1024
	}
	return -1
}

// measureProcessFDs measures the file descriptor / handle count of a process.
// On Linux reads /proc/<pid>/fd; on Windows uses PowerShell HandleCount;
// returns -1 if unavailable.
func measureProcessFDs(pid int) int {
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		if err != nil {
			return -1
		}
		return len(entries)
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("(Get-Process -Id %d).HandleCount", pid)).Output()
		if err != nil {
			return -1
		}
		count, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			return -1
		}
		return count
	}
	return -1
}

// takeSnapshot records goroutines, RSS, and FDs/handles for the backend.
func takeSnapshot(label string, baseURL string, pid int) leakSnapshot {
	goros, err := queryGoroutines(baseURL)
	if err != nil {
		// If /metrics is not available, fall back to -1 (skip goroutine check).
		goros = -1
	}
	return leakSnapshot{
		goroutines: goros,
		rssBytes:   measureProcessRSS(pid),
		fds:        measureProcessFDs(pid),
		label:      label,
	}
}

// ---------------------------------------------------------------------------
// Backend startup with metrics enabled
// ---------------------------------------------------------------------------

// startLeakBackend starts a Go backend with GOWA_METRICS_ENABLED=1 so the
// /metrics endpoint is available for goroutine counting.
func startLeakBackend(t *testing.T) *benchBackend {
	t.Helper()
	root := findRepoRoot(t)
	port := findFreePort(t)

	dataDir, err := os.MkdirTemp("", "gowa-leak-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}

	// Pre-create dummy GOWA binary so the backend skips download.
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dummyName := "gowa"
	if runtime.GOOS == "windows" {
		dummyName = "gowa.exe"
	}
	dummyPath := filepath.Join(binDir, dummyName)
	if err := os.WriteFile(dummyPath, []byte("#!/bin/sh\necho dummy\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Build the Go binary if not present.
	binaryPath := filepath.Join(root, "gowa-manager-go.exe")
	if runtime.GOOS != "windows" {
		binaryPath = filepath.Join(root, "gowa-manager-go")
	}
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/gowa-manager-go")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, out)
		}
	}

	cmd := exec.Command(binaryPath,
		"-p", fmt.Sprintf("%d", port),
		"-u", benchAdminUser,
		"-P", benchAdminPass,
		"-d", dataDir,
	)
	cmd.Dir = root
	// Enable the /metrics endpoint for goroutine counting.
	cmd.Env = append(os.Environ(), "NODE_ENV=production", "GOWA_METRICS_ENABLED=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start backend: %v", err)
	}

	be := &benchBackend{
		cmd:       cmd,
		port:      port,
		dataDir:   dataDir,
		baseURL:   fmt.Sprintf("http://localhost:%d", port),
		adminAuth: base64.StdEncoding.EncodeToString([]byte(benchAdminUser + ":" + benchAdminPass)),
	}

	// Wait for health.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(be.baseURL + "/api/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Final verification.
	resp, err := http.Get(be.baseURL + "/api/health")
	if err != nil {
		t.Fatalf("backend not healthy: %v\nstderr: %s", err, stderr.String())
	}
	resp.Body.Close()

	// Verify /metrics is available.
	if _, err := queryGoroutines(be.baseURL); err != nil {
		t.Fatalf("/metrics endpoint not available (GOWA_METRICS_ENABLED=1 required): %v", err)
	}

	t.Cleanup(func() {
		if cmd.Process != nil {
			if runtime.GOOS == "windows" {
				_ = exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T", "/F").Run()
			} else {
				_ = cmd.Process.Signal(os.Interrupt)
			}
			_ = cmd.Wait()
		}
		os.RemoveAll(dataDir)
	})

	return be
}

// ---------------------------------------------------------------------------
// Fake GOWA binary for process start/stop cycles
// ---------------------------------------------------------------------------

var (
	fakeGOWABinaryOnce sync.Once
	fakeGOWABinaryPath string
)

func buildFakeGOWA(t *testing.T) string {
	t.Helper()
	fakeGOWABinaryOnce.Do(func() {
		root := findRepoRoot(t)
		dir, err := os.MkdirTemp("", "gowa-leak-fakegowa-")
		if err != nil {
			t.Fatal(err)
		}
		binaryName := "gowa"
		if runtime.GOOS == "windows" {
			binaryName = "gowa.exe"
		}
		path := filepath.Join(dir, binaryName)
		cmd := exec.Command("go", "build", "-o", path, "./internal/testutil/fakegowa")
		cmd.Dir = root
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("go build fakegowa: %v\n%s", err, out.String())
		}
		fakeGOWABinaryPath = path
	})
	return fakeGOWABinaryPath
}

// installFakeGOWA copies the cached fakegowa binary into the data directory's
// bin/versions/<version>/ path so the manager can resolve and spawn it.
func installFakeGOWA(t *testing.T, dataDir, version string) {
	t.Helper()
	src := buildFakeGOWA(t)
	binaryName := "gowa"
	if runtime.GOOS == "windows" {
		binaryName = "gowa.exe"
	}
	versionDir := filepath.Join(dataDir, "bin", "versions", version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(versionDir, binaryName)
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open fakegowa: %v", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create fakegowa dst: %v", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy fakegowa: %v", err)
	}
	out.Close()
	// Ensure the copied binary is executable (required on Linux).
	if err := os.Chmod(dst, 0o755); err != nil {
		t.Fatalf("chmod fakegowa dst: %v", err)
	}
}

// ---------------------------------------------------------------------------
// API helpers
// ---------------------------------------------------------------------------

func leakRequest(t *testing.T, be *benchBackend, method, path string, body []byte) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, be.baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Basic "+be.adminAuth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// createInstance creates an instance via the API and returns its ID and key.
func createInstance(t *testing.T, be *benchBackend, name, version string) (int64, string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"name":%q,"gowa_version":%q}`, name, version))
	status, raw := leakRequest(t, be, http.MethodPost, "/api/instances", body)
	if status != http.StatusCreated {
		t.Fatalf("create instance status=%d body=%s", status, string(raw))
	}
	var inst struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatal(err)
	}
	return inst.ID, inst.Key
}

// startInstanceViaAPI starts an instance via the HTTP API.
func startInstanceViaAPI(t *testing.T, be *benchBackend, id int64) {
	t.Helper()
	status, raw := leakRequest(t, be, http.MethodPost, fmt.Sprintf("/api/instances/%d/start", id), nil)
	if status != http.StatusOK {
		t.Fatalf("start instance %d status=%d body=%s", id, status, string(raw))
	}
}

// stopInstanceViaAPI stops an instance via the HTTP API.
func stopInstanceViaAPI(t *testing.T, be *benchBackend, id int64) {
	t.Helper()
	status, raw := leakRequest(t, be, http.MethodPost, fmt.Sprintf("/api/instances/%d/stop", id), nil)
	if status != http.StatusOK {
		t.Fatalf("stop instance %d status=%d body=%s", id, status, string(raw))
	}
}

// waitForInstanceStatus polls the status endpoint until the desired status.
func waitForInstanceStatus(t *testing.T, be *benchBackend, id int64, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, raw := leakRequest(t, be, http.MethodGet, fmt.Sprintf("/api/instances/%d/status", id), nil)
		if status == http.StatusOK {
			var st struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(raw, &st); err == nil && st.Status == want {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("instance %d did not reach status %q within %v", id, want, timeout)
}

// ---------------------------------------------------------------------------
// Leak test
// ---------------------------------------------------------------------------

// TestLeakDetection runs repeated load cycles (1,000 HTTP proxy requests,
// 500 WebSocket connect/send/close cycles, and 100 process start/stop cycles)
// and compares beginning/end goroutines, RSS, and file descriptors/handles.
//
// This test requires the Go backend's /metrics endpoint (enabled via
// GOWA_METRICS_ENABLED=1) to read goroutine counts.
func TestLeakDetection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	be := startLeakBackend(t)
	pid := be.cmd.Process.Pid

	// Install fake GOWA binary for process start/stop cycles.
	installFakeGOWA(t, be.dataDir, leakFakeGOWAVersion)

	// Set up a fake upstream with HTTP echo + WebSocket echo.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ws") {
			// WebSocket echo handler.
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				InsecureSkipVerify: true,
			})
			if err != nil {
				return
			}
			defer c.Close(websocket.StatusNormalClosure, "")
			c.SetReadLimit(1 << 20)
			for {
				msgType, data, err := c.Read(r.Context())
				if err != nil {
					return
				}
				if err := c.Write(r.Context(), msgType, data); err != nil {
					return
				}
			}
		}
		// HTTP echo.
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(body)
	}))
	defer upstream.Close()

	// Parse upstream port.
	upstreamPort := 0
	if _, err := fmt.Sscanf(strings.TrimPrefix(upstream.URL, "http://127.0.0.1:"), "%d", &upstreamPort); err != nil {
		t.Fatalf("parse upstream port: %v", err)
	}

	// Create a proxy instance pointing at the upstream (for HTTP + WS).
	proxyID, proxyKey := createInstance(t, be, "leak-proxy", "latest")
	dbPath := filepath.Join(be.dataDir, "gowa.db")
	updateInstanceDB(t, dbPath, proxyID, upstreamPort)

	// Create a runtime instance for process start/stop cycles.
	runtimeID, _ := createInstance(t, be, "leak-runtime", leakFakeGOWAVersion)

	// ── Baseline snapshot ──
	t.Log("Taking baseline snapshot...")
	before := takeSnapshot("baseline", be.baseURL, pid)
	t.Logf("  goroutines: %d", before.goroutines)
	t.Logf("  RSS: %.1f MB", float64(before.rssBytes)/1024/1024)
	t.Logf("  FDs/handles: %d", before.fds)

	// ── Phase 1: 1,000 HTTP proxy requests ──
	t.Logf("Phase 1: %d HTTP proxy requests...", leakHTTPRequests)
	proxyURL := be.baseURL + "/app/" + proxyKey + "/echo"
	reqBody := bytes.Repeat([]byte("x"), 1024)
	for i := 0; i < leakHTTPRequests; i++ {
		req, _ := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("proxy request %d: %v", i, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if i%200 == 0 {
			t.Logf("  HTTP proxy: %d/%d", i, leakHTTPRequests)
		}
	}
	t.Logf("  HTTP proxy: %d/%d done", leakHTTPRequests, leakHTTPRequests)

	// ── Phase 2: 500 WebSocket connect/send/close cycles ──
	t.Logf("Phase 2: %d WebSocket connect/send/close cycles...", leakWSCycles)
	wsURL := strings.Replace(be.baseURL, "http://", "ws://", 1) + "/app/" + proxyKey + "/ws"
	for i := 0; i < leakWSCycles; i++ {
		wsConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{})
		if err != nil {
			t.Fatalf("ws dial %d: %v", i, err)
		}
		// Give the proxy a moment to establish the upstream connection.
		time.Sleep(20 * time.Millisecond)
		msg := []byte(fmt.Sprintf("leak-msg-%d", i))
		if err := wsConn.Write(ctx, websocket.MessageText, msg); err != nil {
			wsConn.Close(websocket.StatusNormalClosure, "")
			t.Fatalf("ws write %d: %v", i, err)
		}
		_, _, err = wsConn.Read(ctx)
		if err != nil {
			wsConn.Close(websocket.StatusNormalClosure, "")
			t.Fatalf("ws read %d: %v", i, err)
		}
		wsConn.Close(websocket.StatusNormalClosure, "")
		if i%100 == 0 {
			t.Logf("  WebSocket: %d/%d", i, leakWSCycles)
		}
	}
	t.Logf("  WebSocket: %d/%d done", leakWSCycles, leakWSCycles)

	// ── Phase 3: 100 process start/stop cycles ──
	t.Logf("Phase 3: %d process start/stop cycles...", leakProcessCycles)
	for i := 0; i < leakProcessCycles; i++ {
		startInstanceViaAPI(t, be, runtimeID)
		waitForInstanceStatus(t, be, runtimeID, "running", 15*time.Second)
		stopInstanceViaAPI(t, be, runtimeID)
		waitForInstanceStatus(t, be, runtimeID, "stopped", 15*time.Second)
		if i%20 == 0 {
			t.Logf("  Process start/stop: %d/%d", i, leakProcessCycles)
		}
	}
	t.Logf("  Process start/stop: %d/%d done", leakProcessCycles, leakProcessCycles)

	// ── Settle: let goroutines and resources settle ──
	t.Logf("Settling for %d seconds (querying /metrics to trigger runtime snapshots)...", leakSettleSeconds)
	settleEnd := time.Now().Add(time.Duration(leakSettleSeconds) * time.Second)
	for time.Now().Before(settleEnd) {
		// Querying /metrics calls RecordRuntime() which reads MemStats,
		// encouraging GC activity. Take a few samples during settle.
		_, _ = queryGoroutines(be.baseURL)
		time.Sleep(2 * time.Second)
	}

	// ── End snapshot (take 3 samples and use the best) ──
	t.Log("Taking end snapshot (3 samples, using best)...")
	var after leakSnapshot
	for i := 0; i < 3; i++ {
		snap := takeSnapshot("end", be.baseURL, pid)
		if i == 0 {
			after = snap
		} else {
			// Use the lowest RSS and FDs, and the lowest goroutine count.
			if snap.rssBytes > 0 && (after.rssBytes < 0 || snap.rssBytes < after.rssBytes) {
				after.rssBytes = snap.rssBytes
			}
			if snap.fds >= 0 && (after.fds < 0 || snap.fds < after.fds) {
				after.fds = snap.fds
			}
			if snap.goroutines >= 0 && (after.goroutines < 0 || snap.goroutines < after.goroutines) {
				after.goroutines = snap.goroutines
			}
		}
		t.Logf("  sample %d: goroutines=%d RSS=%.1f MB FDs=%d",
			i+1, snap.goroutines, float64(snap.rssBytes)/1024/1024, snap.fds)
		time.Sleep(1 * time.Second)
	}
	t.Logf("  best: goroutines=%d RSS=%.1f MB FDs=%d",
		after.goroutines, float64(after.rssBytes)/1024/1024, after.fds)

	// ── Compare ──
	t.Log("\n── Leak Detection Results ──")

	leakFailed := false

	// Goroutines — -1 means unavailable
	goroDelta := -1
	if before.goroutines >= 0 && after.goroutines >= 0 {
		goroDelta = after.goroutines - before.goroutines
		if goroDelta < 0 {
			goroDelta = -goroDelta
		}
		if goroDelta > leakGoroutineMaxDelta {
			leakFailed = true
			t.Errorf("❌ goroutine leak: before=%d after=%d (delta=%d, max=%d)",
				before.goroutines, after.goroutines, goroDelta, leakGoroutineMaxDelta)
		} else {
			t.Logf("✅ goroutines: before=%d after=%d (delta=%d, max=%d)",
				before.goroutines, after.goroutines, goroDelta, leakGoroutineMaxDelta)
		}
	} else {
		t.Logf("⏭️ goroutines: skipped (metrics endpoint unavailable)")
	}

	// RSS — -1 means unavailable
	rssGrowth := -1.0
	if before.rssBytes > 0 && after.rssBytes > 0 {
		rssGrowth = float64(after.rssBytes-before.rssBytes) / float64(before.rssBytes) * 100
		if rssGrowth > leakRssMaxPercentGrowth {
			leakFailed = true
			t.Errorf("❌ RSS leak: before=%.1f MB after=%.1f MB (growth=%.1f%%, max=%.1f%%)",
				float64(before.rssBytes)/1024/1024, float64(after.rssBytes)/1024/1024,
				rssGrowth, leakRssMaxPercentGrowth)
		} else {
			t.Logf("✅ RSS: before=%.1f MB after=%.1f MB (growth=%.1f%%, max=%.1f%%)",
				float64(before.rssBytes)/1024/1024, float64(after.rssBytes)/1024/1024,
				rssGrowth, leakRssMaxPercentGrowth)
		}
	} else {
		t.Logf("⏭️ RSS: skipped (measurement unavailable)")
	}

	// FDs / handles — -1 means unavailable
	fdDelta := -1
	if before.fds >= 0 && after.fds >= 0 {
		fdDelta = after.fds - before.fds
		if fdDelta < 0 {
			fdDelta = -fdDelta
		}
		if fdDelta > leakFdMaxDelta {
			leakFailed = true
			t.Errorf("❌ FD/handle leak: before=%d after=%d (delta=%d, max=%d)",
				before.fds, after.fds, fdDelta, leakFdMaxDelta)
		} else {
			t.Logf("✅ FDs/handles: before=%d after=%d (delta=%d, max=%d)",
				before.fds, after.fds, fdDelta, leakFdMaxDelta)
		}
	} else {
		t.Logf("⏭️ FDs/handles: skipped (measurement unavailable on this platform)")
	}

	// ── Machine-readable metrics for compare.ts ──
	// These lines are parsed by test/benchmark/compare.ts to integrate leak
	// detection into the overall PASS/FAIL verdict. A value of -1 indicates
	// the metric was unavailable (skipped) on this platform/environment.
	verdict := "PASS"
	if leakFailed {
		verdict = "FAIL"
	}
	fmt.Printf("goroutine_delta: %d\n", goroDelta)
	fmt.Printf("rss_growth_percent: %.2f\n", rssGrowth)
	fmt.Printf("fd_handle_delta: %d\n", fdDelta)
	fmt.Printf("leak_test: %s\n", verdict)

	// Cleanup instances
	leakRequest(t, be, http.MethodDelete, fmt.Sprintf("/api/instances/%d", proxyID), nil)
	leakRequest(t, be, http.MethodDelete, fmt.Sprintf("/api/instances/%d", runtimeID), nil)
}
