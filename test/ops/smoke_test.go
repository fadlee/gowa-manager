package ops

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Smoke test helpers
// ---------------------------------------------------------------------------

// startManagerBinary builds (if needed) and starts the Go manager binary on
// a free port with the given data dir.  Returns the base URL and a cleanup
// function that stops the process.
func startManagerBinary(t *testing.T, dataDir string, metrics bool) (string, func()) {
	t.Helper()
	bin := managerBinary(t)
	port := freePort(t)

	args := []string{
		"--port", fmt.Sprint(port),
		"--data-dir", dataDir,
	}
	if metrics {
		args = append(args, "--admin-username", "admin", "--admin-password", "password")
	}

	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start manager binary: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for the manager to become ready.
	if !waitForReady(t, baseURL, 15*time.Second) {
		_ = cmd.Process.Kill()
		t.Fatalf("manager did not become ready on %s", baseURL)
	}

	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
	return baseURL, cleanup
}

// waitForReady polls /api/ready until it returns 200 or timeout.
func waitForReady(t *testing.T, baseURL string, timeout time.Duration) bool {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/ready")
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return true
			}
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// buildFakeBunBinary builds a tiny Go binary that listens on a port and
// responds to /api/health and /api/ready with 200.  Used as a stand-in for
// the Bun manager in smoke tests that need a live HTTP server.
func buildFakeBunBinary(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gowa-ops-fakebun-smoke-")
	if err != nil {
		t.Fatal(err)
	}
	name := "fake-bun"
	if runtime.GOOS == "windows" {
		name = "fake-bun.exe"
	}
	binPath := filepath.Join(dir, name)
	src := `package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("FAKE_BUN_PORT")
	if port == "" {
		port = "0"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, ` + "`" + `{"status":"ok"}` + "`" + `)
	})
	mux.HandleFunc("/api/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, ` + "`" + `{"status":"ready"}` + "`" + `)
	})
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, ` + "`" + `{"success":true}` + "`" + `)
	})
	mux.HandleFunc("/api/instances", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "[]")
	})
	mux.HandleFunc("/api/system/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, ` + "`" + `{"status":"ok"}` + "`" + `)
	})
	mux.HandleFunc("/api/system/versions/installed", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "[]")
	})
	mux.HandleFunc("/api/system/auto-update/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, ` + "`" + `{"enabled":false}` + "`" + `)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "# HELP test")
	})
	http.ListenAndServe(":"+port, mux)
}
`
	srcFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcFile, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake bun: %v\n%s", err, out)
	}
	return binPath
}

// startFakeBun starts the fake Bun binary on a free port and returns the
// base URL and a cleanup function.
func startFakeBun(t *testing.T) (string, func()) {
	t.Helper()
	bin := buildFakeBunBinary(t)
	port := freePort(t)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "FAKE_BUN_PORT="+fmt.Sprint(port))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake bun: %v", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitForReady(t, baseURL, 10*time.Second) {
		_ = cmd.Process.Kill()
		t.Fatalf("fake bun did not become ready on %s", baseURL)
	}
	return baseURL, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
}

// killFakeBunProcesses kills any running fake-bun processes.  Best-effort.
func killFakeBunProcesses() {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/IM", "fake-bun.exe", "/F").Run()
	} else {
		_ = exec.Command("pkill", "-f", "fake-bun").Run()
	}
}

// ---------------------------------------------------------------------------
// Smoke tests against a real Go manager
// ---------------------------------------------------------------------------

// TestSmoke_AllChecksPass runs the smoke script against a real Go manager
// and verifies all non-destructive checks pass.
func TestSmoke_AllChecksPass(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		// On Windows we need pwsh; skip if not available.
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	baseURL, cleanup := startManagerBinary(t, dataDir, false)
	defer cleanup()

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
	})

	assertExitCode(t, r, 0)
	if r.JSON["tool"] != "smoke" {
		t.Fatalf("tool = %v, want smoke", r.JSON["tool"])
	}
	if r.JSON["mode"] != "non_destructive" {
		t.Fatalf("mode = %v, want non_destructive", r.JSON["mode"])
	}
	passCount, ok := r.JSON["pass_count"].(float64)
	if !ok {
		t.Fatalf("pass_count not a number: %v", r.JSON["pass_count"])
	}
	if passCount < 7 {
		t.Fatalf("pass_count = %v, expected >= 7 (health, ready, auth, instances, system_status, versions, autoupdate)", passCount)
	}
	failCount, ok := r.JSON["fail_count"].(float64)
	if !ok {
		t.Fatalf("fail_count not a number: %v", r.JSON["fail_count"])
	}
	if failCount != 0 {
		t.Fatalf("fail_count = %v, want 0\nstdout: %s", failCount, r.RawStdout)
	}
}

// TestSmoke_NonResponsiveManager verifies the smoke script fails when the
// manager is not running.
func TestSmoke_NonResponsiveManager(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	// Find a free port but don't start anything on it.
	port := freePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
	})

	if r.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for non-responsive manager, got 0\nstdout: %s", r.RawStdout)
	}
	failCount, ok := r.JSON["fail_count"].(float64)
	if !ok || failCount == 0 {
		t.Fatalf("expected fail_count > 0, got %v\nstdout: %s", r.JSON["fail_count"], r.RawStdout)
	}
}

// TestSmoke_MissingAuthFailsProtectedEndpoints verifies that protected
// endpoints fail when auth credentials are wrong.
func TestSmoke_MissingAuthFailsProtectedEndpoints(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	baseURL, cleanup := startManagerBinary(t, dataDir, false)
	defer cleanup()

	// Use wrong password — protected endpoints should fail.
	r := runScript(t, "smoke", []string{
		"--url", baseURL,
		"--admin-password", "wrong-password",
	})

	if r.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code with wrong auth, got 0\nstdout: %s", r.RawStdout)
	}
	// The health and ready checks (no auth) should still pass, but
	// protected endpoints (instances, system/*) should fail.
	checks, ok := r.JSON["checks"].([]any)
	if !ok {
		t.Fatalf("checks not an array: %v", r.JSON["checks"])
	}
	protectedFailed := false
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		status, _ := m["status"].(string)
		// Protected endpoints that require auth.
		if strings.Contains(name, "instances") || strings.Contains(name, "system") || name == "auth_login" {
			if status == "fail" {
				protectedFailed = true
			}
		}
	}
	if !protectedFailed {
		t.Fatalf("expected at least one protected endpoint to fail with wrong auth\nstdout: %s", r.RawStdout)
	}
}

// TestSmoke_DestructiveRequiresTestKey verifies that --destructive without
// --test-key fails.
func TestSmoke_DestructiveRequiresTestKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	port := freePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
		"--destructive",
	})

	assertExitCode(t, r, 1)
	errs, ok := r.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors for destructive without --test-key, got %v\nstdout: %s",
			r.JSON["errors"], r.RawStdout)
	}
	found := false
	for _, e := range errs {
		if s, ok := e.(string); ok && strings.Contains(s, "test-key") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about --test-key, got %v", errs)
	}
}

// TestSmoke_MetricsFlag verifies that --metrics enables the metrics check.
func TestSmoke_MetricsFlag(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// Start manager with metrics enabled via env var.
	bin := managerBinary(t)
	port := freePort(t)
	cmd := exec.Command(bin, "--port", fmt.Sprint(port), "--data-dir", dataDir)
	cmd.Env = append(os.Environ(), "GOWA_METRICS_ENABLED=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitForReady(t, baseURL, 15*time.Second) {
		t.Fatalf("manager did not become ready")
	}

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
		"--metrics",
	})

	// The metrics check should be present.  It may pass or fail depending
	// on whether the metrics endpoint is reachable, but the check must
	// exist in the output.
	checks, ok := r.JSON["checks"].([]any)
	if !ok {
		t.Fatalf("checks not an array: %v", r.JSON["checks"])
	}
	found := false
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == "metrics" {
			found = true
		}
	}
	if !found {
		t.Fatalf("metrics check not found in checks array\nstdout: %s", r.RawStdout)
	}
}

// TestSmoke_NoPasswordsPrinted verifies the JSON output never contains
// the admin password.
func TestSmoke_NoPasswordsPrinted(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	baseURL, cleanup := startManagerBinary(t, dataDir, false)
	defer cleanup()

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
		"--admin-password", "super-secret-pw-xyz",
	})

	if strings.Contains(r.RawStdout, "super-secret-pw-xyz") {
		t.Fatal("smoke JSON output contains the admin password")
	}
}

// TestSmoke_FakeBunAllChecksPass runs the smoke script against a fake Bun
// HTTP server (stand-in for the real Bun manager) to verify the script
// logic without requiring a real Bun install.  This is used by rollback
// tests as well.
func TestSmoke_FakeBunAllChecksPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	baseURL, cleanup := startFakeBun(t)
	defer cleanup()

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
	})

	assertExitCode(t, r, 0)
	passCount, ok := r.JSON["pass_count"].(float64)
	if !ok {
		t.Fatalf("pass_count not a number: %v", r.JSON["pass_count"])
	}
	if passCount < 7 {
		t.Fatalf("pass_count = %v, expected >= 7", passCount)
	}
}

// TestSmoke_JSONSchema verifies the JSON output has the expected fields.
func TestSmoke_JSONSchema(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("curl"); err != nil {
			t.Skip("curl not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	baseURL, cleanup := startManagerBinary(t, dataDir, false)
	defer cleanup()

	r := runScript(t, "smoke", []string{
		"--url", baseURL,
	})
	assertExitCode(t, r, 0)

	for _, key := range []string{"tool", "schema_version", "mode", "start_timestamp", "end_timestamp", "url", "metrics_enabled", "destructive", "checks", "pass_count", "fail_count", "errors", "exit_code"} {
		if _, ok := r.JSON[key]; !ok {
			t.Fatalf("JSON missing key %q\nstdout: %s", key, r.RawStdout)
		}
	}
}

// Ensure the net package is referenced (used by freePort in helpers).
var _ = net.Listen
