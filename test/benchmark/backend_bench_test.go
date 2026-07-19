// Package benchmark contains performance baseline tests and Go-native
// micro-benchmarks for the GOWA Manager backends.
//
// The harness script (scripts/benchmark-backends.ts) captures full baseline
// measurements across nine scenarios.  This file validates the harness output
// against baseline.schema.json and provides Go-native benchmarks using
// testing.B for scenarios that benefit from the Go benchmark framework.
//
// Run the harness:
//
//	bun run scripts/benchmark-backends.ts --backend bun --output test/benchmark/bun-baseline.json
//
// Run the Go tests:
//
//	go test ./test/benchmark/...
//
// Run the Go benchmarks:
//
//	go test ./test/benchmark/... -bench=. -benchmem
package benchmark

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	// modernc.org/sqlite is a pure-Go driver already in go.mod.
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Schema validation
// ---------------------------------------------------------------------------

const (
	schemaFile     = "baseline.schema.json"
	bunBaselineFile = "bun-baseline.json"
)

// findRepoRoot walks up from the test working directory to find go.mod.
func findRepoRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// loadJSON loads and parses a JSON file from the benchmark test directory.
func loadJSON(t *testing.T, name string) map[string]any {
	t.Helper()
	root := findRepoRoot(t)
	path := filepath.Join(root, "test", "benchmark", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

// TestSchemaIsValidJSON verifies the baseline schema file is valid JSON
// with the expected top-level structure.
func TestSchemaIsValidJSON(t *testing.T) {
	schema := loadJSON(t, schemaFile)
	if schema["title"] == nil {
		t.Fatal("schema missing 'title'")
	}
	if _, ok := schema["properties"].(map[string]any); !ok {
		t.Fatal("schema missing 'properties'")
	}
	if _, ok := schema["definitions"].(map[string]any); !ok {
		t.Fatal("schema missing 'definitions'")
	}
}

// validateRequired checks that all required fields are present in the object.
func validateRequired(t *testing.T, obj map[string]any, required []string, prefix string) {
	t.Helper()
	for _, field := range required {
		if _, ok := obj[field]; !ok {
			t.Errorf("%s: missing required field %q", prefix, field)
		}
	}
}

// validateDistribution validates a distribution object (raw, median, p95, unit).
func validateDistribution(t *testing.T, d map[string]any, prefix string) {
	t.Helper()
	validateRequired(t, d, []string{"raw", "median", "p95", "unit"}, prefix)
	if raw, ok := d["raw"].([]any); ok {
		if len(raw) == 0 {
			t.Errorf("%s.raw is empty", prefix)
		}
	} else {
		t.Errorf("%s.raw is not an array", prefix)
	}
}

// validateDurationScenario validates a duration scenario.
func validateDurationScenario(t *testing.T, s map[string]any, prefix string) {
	t.Helper()
	validateRequired(t, s, []string{"raw", "median", "p95", "unit", "sampleCount"}, prefix)
	if raw, ok := s["raw"].([]any); ok {
		if len(raw) == 0 {
			t.Errorf("%s.raw is empty", prefix)
		}
	} else {
		t.Errorf("%s.raw is not an array", prefix)
	}
}

// validateSizeScenario validates a size scenario.
func validateSizeScenario(t *testing.T, s map[string]any, prefix string) {
	t.Helper()
	validateDistribution(t, s, prefix)
}

// validateMetadata validates the metadata section of a baseline.
func validateMetadata(t *testing.T, meta map[string]any) {
	t.Helper()
	validateRequired(t, meta, []string{
		"backend", "os", "arch", "cpu", "cpuCount",
		"runtimeVersion", "goVersion", "sampleCount",
		"fixtureCommit", "capturedAt",
	}, "metadata")
	if backend, ok := meta["backend"].(string); ok {
		if backend != "bun" && backend != "go" {
			t.Errorf("metadata.backend = %q, want bun or go", backend)
		}
	}
}

// validateScenarios validates all nine scenario sections.
func validateScenarios(t *testing.T, scenarios map[string]any) {
	t.Helper()
	validateRequired(t, scenarios, []string{
		"coldStartup", "idleRss", "crudLatency", "proxyHttp",
		"webSocket", "monitoringCost", "gracefulShutdown",
		"executableSize", "dockerImageSize",
	}, "scenarios")

	if s, ok := scenarios["coldStartup"].(map[string]any); ok {
		validateDurationScenario(t, s, "scenarios.coldStartup")
	}
	if s, ok := scenarios["idleRss"].(map[string]any); ok {
		validateSizeScenario(t, s, "scenarios.idleRss")
	}
	if s, ok := scenarios["crudLatency"].(map[string]any); ok {
		validateRequired(t, s, []string{"operations", "median", "p95", "sampleCount"}, "scenarios.crudLatency")
		if ops, ok := s["operations"].(map[string]any); ok {
			for _, op := range []string{"create", "read", "update", "delete"} {
				if d, ok := ops[op].(map[string]any); ok {
					validateDistribution(t, d, "scenarios.crudLatency.operations."+op)
				} else {
					t.Errorf("scenarios.crudLatency.operations.%s missing or not object", op)
				}
			}
		} else {
			t.Error("scenarios.crudLatency.operations missing or not object")
		}
	}
	if s, ok := scenarios["proxyHttp"].(map[string]any); ok {
		validateRequired(t, s, []string{"small", "large"}, "scenarios.proxyHttp")
		for _, size := range []string{"small", "large"} {
			if sub, ok := s[size].(map[string]any); ok {
				validateRequired(t, sub, []string{"bodySizeBytes", "requestsPerSec", "latency"}, "scenarios.proxyHttp."+size)
				if rps, ok := sub["requestsPerSec"].(map[string]any); ok {
					validateDistribution(t, rps, "scenarios.proxyHttp."+size+".requestsPerSec")
				}
				if lat, ok := sub["latency"].(map[string]any); ok {
					validateDistribution(t, lat, "scenarios.proxyHttp."+size+".latency")
				}
			}
		}
	}
	if s, ok := scenarios["webSocket"].(map[string]any); ok {
		validateRequired(t, s, []string{"clientCount", "messagesPerClient", "messagesPerSec", "latency"}, "scenarios.webSocket")
		if rps, ok := s["messagesPerSec"].(map[string]any); ok {
			validateDistribution(t, rps, "scenarios.webSocket.messagesPerSec")
		}
	}
	if s, ok := scenarios["monitoringCost"].(map[string]any); ok {
		validateRequired(t, s, []string{"instanceCount", "windowSeconds", "cpuPercent", "rssBytes"}, "scenarios.monitoringCost")
	}
	if s, ok := scenarios["gracefulShutdown"].(map[string]any); ok {
		validateDurationScenario(t, s, "scenarios.gracefulShutdown")
	}
	if s, ok := scenarios["executableSize"].(map[string]any); ok {
		validateSizeScenario(t, s, "scenarios.executableSize")
	}
	if s, ok := scenarios["dockerImageSize"].(map[string]any); ok {
		validateRequired(t, s, []string{"available", "sizeBytes"}, "scenarios.dockerImageSize")
	}
}

// TestBunBaselineMatchesSchema validates the committed bun-baseline.json
// against the structural requirements of baseline.schema.json.
func TestBunBaselineMatchesSchema(t *testing.T) {
	root := findRepoRoot(t)
	baselinePath := filepath.Join(root, "test", "benchmark", bunBaselineFile)
	if _, err := os.Stat(baselinePath); os.IsNotExist(err) {
		t.Skipf("baseline file not found: %s (run harness first)", baselinePath)
	}

	baseline := loadJSON(t, bunBaselineFile)
	validateRequired(t, baseline, []string{"metadata", "scenarios"}, "root")

	if meta, ok := baseline["metadata"].(map[string]any); ok {
		validateMetadata(t, meta)
	} else {
		t.Fatal("root.metadata missing or not object")
	}

	if scenarios, ok := baseline["scenarios"].(map[string]any); ok {
		validateScenarios(t, scenarios)
	} else {
		t.Fatal("root.scenarios missing or not object")
	}
}

// TestHarnessScriptExists verifies the benchmark harness script is present.
func TestHarnessScriptExists(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "scripts", "benchmark-backends.ts")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("harness script not found: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Go-native micro-benchmarks
// ---------------------------------------------------------------------------

const (
	benchAdminUser = "admin"
	benchAdminPass = "password"
)

// benchBackend wraps a running Go backend subprocess for benchmarking.
type benchBackend struct {
	cmd       *exec.Cmd
	port      int
	dataDir   string
	baseURL   string
	adminAuth string
}

// findFreePort returns a free TCP port on localhost.
func findFreePort(tb testing.TB) int {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// startBenchBackend starts a Go backend on a free port with a temp data dir.
// It pre-creates a dummy GOWA binary to skip the download step.
func startBenchBackend(tb testing.TB) *benchBackend {
	tb.Helper()
	root := findRepoRoot(tb)
	port := findFreePort(tb)

	dataDir, err := os.MkdirTemp("", "gowa-bench-go-")
	if err != nil {
		tb.Fatalf("mkdtemp: %v", err)
	}

	// Pre-create dummy GOWA binary so the backend skips download.
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		tb.Fatal(err)
	}
	dummyName := "gowa"
	if runtime.GOOS == "windows" {
		dummyName = "gowa.exe"
	}
	dummyPath := filepath.Join(binDir, dummyName)
	if err := os.WriteFile(dummyPath, []byte("#!/bin/sh\necho dummy\n"), 0o755); err != nil {
		tb.Fatal(err)
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
			tb.Fatalf("go build: %v\n%s", err, out)
		}
	}

	cmd := exec.Command(binaryPath,
		"-p", fmt.Sprintf("%d", port),
		"-u", benchAdminUser,
		"-P", benchAdminPass,
		"-d", dataDir,
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NODE_ENV=production")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		tb.Fatalf("start backend: %v", err)
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
		tb.Fatalf("backend not healthy: %v\nstderr: %s", err, stderr.String())
	}
	resp.Body.Close()

	tb.Cleanup(func() {
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

// benchRequest performs an authenticated request against the backend.
func benchRequest(b *testing.B, be *benchBackend, method, path string, body []byte) (int, []byte) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, be.baseURL+path, reader)
	if err != nil {
		b.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Basic "+be.adminAuth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// updateInstanceDB sets an instance's status to 'running' and port to the
// given value in the SQLite database using the modernc.org/sqlite driver.
func updateInstanceDB(tb testing.TB, dbPath string, id int64, port int) {
	tb.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		tb.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_, err = db.Exec("UPDATE instances SET status = ?, port = ? WHERE id = ?", "running", port, id)
	if err != nil {
		tb.Fatalf("update instance: %v", err)
	}
}

// BenchmarkHealthEndpoint measures GET /api/health throughput.
func BenchmarkHealthEndpoint(b *testing.B) {
	be := startBenchBackend(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(be.baseURL + "/api/health")
		if err != nil {
			b.Fatal(err)
		}
		if resp.StatusCode != 200 {
			b.Fatalf("health status = %d, want 200", resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// BenchmarkCrudCreate measures POST /api/instances throughput.
func BenchmarkCrudCreate(b *testing.B) {
	be := startBenchBackend(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := []byte(fmt.Sprintf(`{"name":"bench-create-%d-%d"}`, i, time.Now().UnixNano()))
		status, _ := benchRequest(b, be, http.MethodPost, "/api/instances", body)
		if status != 201 {
			b.Fatalf("create status = %d, want 201", status)
		}
	}
}

// BenchmarkCrudRead measures GET /api/instances/{id} throughput.
func BenchmarkCrudRead(b *testing.B) {
	be := startBenchBackend(b)

	// Create one instance to read.
	body := []byte(fmt.Sprintf(`{"name":"bench-read-%d"}`, time.Now().UnixNano()))
	status, raw := benchRequest(b, be, http.MethodPost, "/api/instances", body)
	if status != 201 {
		b.Fatalf("create status = %d, want 201", status)
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &inst); err != nil {
		b.Fatal(err)
	}
	path := fmt.Sprintf("/api/instances/%d", inst.ID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status, _ := benchRequest(b, be, http.MethodGet, path, nil)
		if status != 200 {
			b.Fatalf("read status = %d, want 200", status)
		}
	}
}

// BenchmarkProxyForward measures HTTP proxy forwarding throughput with a
// small body against an in-process httptest upstream.
func BenchmarkProxyForward(b *testing.B) {
	be := startBenchBackend(b)

	// Start an httptest echo upstream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(body)
	}))
	defer upstream.Close()

	// Parse upstream port from URL (e.g. http://127.0.0.1:12345).
	upstreamPort := 0
	if _, err := fmt.Sscanf(strings.TrimPrefix(upstream.URL, "http://127.0.0.1:"), "%d", &upstreamPort); err != nil {
		b.Fatalf("parse upstream port: %v", err)
	}

	// Create an instance via API.
	body := []byte(fmt.Sprintf(`{"name":"bench-proxy-%d"}`, time.Now().UnixNano()))
	status, raw := benchRequest(b, be, http.MethodPost, "/api/instances", body)
	if status != 201 {
		b.Fatalf("create status = %d, want 201", status)
	}
	var inst struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &inst); err != nil {
		b.Fatal(err)
	}

	// Update the DB to set the instance as running with the upstream port.
	dbPath := filepath.Join(be.dataDir, "gowa.db")
	updateInstanceDB(b, dbPath, inst.ID, upstreamPort)

	proxyURL := be.baseURL + "/app/" + inst.Key + "/echo"
	reqBody := bytes.Repeat([]byte("x"), 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		if resp.StatusCode != 200 {
			b.Fatalf("proxy status = %d, want 200", resp.StatusCode)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
