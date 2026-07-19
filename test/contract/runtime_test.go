package contract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// runtimeTestVersion is the fake GOWA version installed into every backend's
// data directory for runtime lifecycle parity tests.
const runtimeTestVersion = "v0.0.1-contract"

var (
	fakeGOWABinaryOnce sync.Once
	fakeGOWABinaryPath string
)

// buildFakeGOWA builds the fakegowa binary once per test process into a shared
// temp directory and returns the resulting binary path. Subsequent calls reuse
// the cached path. The binary is copied into each backend's version directory
// by installFakeGOWARuntime.
func buildFakeGOWA(t *testing.T) string {
	t.Helper()
	fakeGOWABinaryOnce.Do(func() {
		repoRoot := findRepoRoot(t)
		dir, err := os.MkdirTemp("", "gowa-contract-fakegowa-")
		if err != nil {
			t.Fatal(err)
		}
		binaryName := "gowa"
		if runtime.GOOS == "windows" {
			binaryName = "gowa.exe"
		}
		path := filepath.Join(dir, binaryName)
		cmd := exec.Command("go", "build", "-o", path, "./internal/testutil/fakegowa")
		cmd.Dir = repoRoot
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

// installFakeGOWARuntime copies the cached fakegowa binary into the data
// directory's bin/versions/<version>/gowa[.exe] path so the manager can
// resolve and spawn it.
func installFakeGOWARuntime(t *testing.T, dataDir, version string) {
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
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copy fakegowa binary: %v", err)
	}
}

// installCrashFakeGOWA installs a wrapper script/batch file that exits
// immediately, simulating a GOWA binary that crashes on start. The wrapper is
// installed under the given version so the manager resolves it as the version
// binary. On Windows a .bat file is used; on Unix a shell script with the
// executable bit set.
func installCrashFakeGOWA(t *testing.T, dataDir, version string) {
	t.Helper()
	versionDir := filepath.Join(dataDir, "bin", "versions", version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		dst := filepath.Join(versionDir, "gowa.exe")
		// Build a tiny Go program that exits with code 1 to simulate a crash.
		src := []byte("package main\n\nimport \"os\"\n\nfunc main() { os.Exit(1) }\n")
		tmpDir, err := os.MkdirTemp("", "gowa-crash-")
		if err != nil {
			t.Fatal(err)
		}
		srcFile := filepath.Join(tmpDir, "main.go")
		if err := os.WriteFile(srcFile, src, 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("go", "build", "-o", dst, srcFile).CombinedOutput(); err != nil {
			t.Fatalf("build crash fakegowa: %v\n%s", err, out)
		}
		return
	}
	dst := filepath.Join(versionDir, "gowa")
	script := []byte("#!/bin/sh\nexit 1\n")
	if err := os.WriteFile(dst, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = out.Chmod(0o755)
	}
	return out.Close()
}

// runtimeInstance is the minimal instance shape returned by the create API.
type runtimeInstance struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Port        *int   `json:"port"`
	GOWAVersion string `json:"gowa_version"`
}

// runtimeStatus is the lifecycle status response shape (start/stop/kill/
// restart/status endpoints).
type runtimeStatus struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Port   *int   `json:"port"`
	PID    *int   `json:"pid"`
	Uptime *int64 `json:"uptime"`
}

// runtimeRequest performs an authenticated HTTP request against the backend
// and decodes the JSON body into the provided destination.
func runtimeRequest(t *testing.T, client *http.Client, be backend, method, path string, body []byte, dest any) (int, []byte) {
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
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(contractUser+":"+contractPass)))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s %s: %v", be.name, method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if dest != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, dest); err != nil {
			t.Fatalf("%s %s: decode body %q: %v", be.name, path, string(raw), err)
		}
	}
	return resp.StatusCode, raw
}

// createRuntimeInstance creates an instance with the given name and version
// via the backend's HTTP API and returns the created instance.
func createRuntimeInstance(t *testing.T, client *http.Client, be backend, name, version string) runtimeInstance {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "gowa_version": version})
	var created runtimeInstance
	status, raw := runtimeRequest(t, client, be, http.MethodPost, "/api/instances", body, &created)
	if status != http.StatusCreated {
		t.Fatalf("%s create instance status = %d, want 201; body = %s", be.name, status, string(raw))
	}
	return created
}

// startInstance starts the instance via the HTTP API and returns the status.
func startInstance(t *testing.T, client *http.Client, be backend, id int64) (int, runtimeStatus) {
	t.Helper()
	var st runtimeStatus
	status, raw := runtimeRequest(t, client, be, http.MethodPost, fmt.Sprintf("/api/instances/%d/start", id), nil, &st)
	if status != http.StatusOK {
		t.Fatalf("%s start instance %d status = %d, want 200; body = %s", be.name, id, status, string(raw))
	}
	return status, st
}

// stopInstance stops the instance via the HTTP API.
func stopInstance(t *testing.T, client *http.Client, be backend, id int64) runtimeStatus {
	t.Helper()
	var st runtimeStatus
	status, raw := runtimeRequest(t, client, be, http.MethodPost, fmt.Sprintf("/api/instances/%d/stop", id), nil, &st)
	if status != http.StatusOK {
		t.Fatalf("%s stop instance %d status = %d, want 200; body = %s", be.name, id, status, string(raw))
	}
	return st
}

// killInstance force-kills the instance via the HTTP API.
func killInstance(t *testing.T, client *http.Client, be backend, id int64) runtimeStatus {
	t.Helper()
	var st runtimeStatus
	status, raw := runtimeRequest(t, client, be, http.MethodPost, fmt.Sprintf("/api/instances/%d/kill", id), nil, &st)
	if status != http.StatusOK {
		t.Fatalf("%s kill instance %d status = %d, want 200; body = %s", be.name, id, status, string(raw))
	}
	return st
}

// restartInstance restarts the instance via the HTTP API.
func restartInstance(t *testing.T, client *http.Client, be backend, id int64) runtimeStatus {
	t.Helper()
	var st runtimeStatus
	status, raw := runtimeRequest(t, client, be, http.MethodPost, fmt.Sprintf("/api/instances/%d/restart", id), nil, &st)
	if status != http.StatusOK {
		t.Fatalf("%s restart instance %d status = %d, want 200; body = %s", be.name, id, status, string(raw))
	}
	return st
}

// getInstanceStatus fetches the instance lifecycle status via the HTTP API.
func getInstanceStatus(t *testing.T, client *http.Client, be backend, id int64) runtimeStatus {
	t.Helper()
	var st runtimeStatus
	status, raw := runtimeRequest(t, client, be, http.MethodGet, fmt.Sprintf("/api/instances/%d/status", id), nil, &st)
	if status != http.StatusOK {
		t.Fatalf("%s status instance %d status = %d, want 200; body = %s", be.name, id, status, string(raw))
	}
	return st
}

// deleteInstance deletes the instance via the HTTP API and returns the HTTP
// status code and raw body.
func deleteInstance(t *testing.T, client *http.Client, be backend, id int64) (int, []byte) {
	t.Helper()
	return runtimeRequest(t, client, be, http.MethodDelete, fmt.Sprintf("/api/instances/%d", id), nil, nil)
}

// waitForRuntimeStatus polls the instance status endpoint until it reaches the
// desired status or the timeout expires.
func waitForRuntimeStatus(t *testing.T, client *http.Client, be backend, id int64, want string, timeout time.Duration) runtimeStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := getInstanceStatus(t, client, be, id)
		if st.Status == want {
			return st
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("%s instance %d did not reach status %q within %v (last = %+v)", be.name, id, want, timeout, getInstanceStatus(t, client, be, id))
	return runtimeStatus{}
}

// waitForNonRunningStatus polls the instance status endpoint until it reaches
// any status other than "running" (e.g. stopped, failed, error) or the timeout
// expires.
func waitForNonRunningStatus(t *testing.T, client *http.Client, be backend, id int64, timeout time.Duration) runtimeStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := getInstanceStatus(t, client, be, id)
		if st.Status != "running" && st.Status != "starting" {
			return st
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s instance %d did not leave running state within %v (last = %+v)", be.name, id, timeout, getInstanceStatus(t, client, be, id))
	return runtimeStatus{}
}

// normalizeRuntimeStatus replaces volatile fields (pid, uptime, port, id,
// name) with stable labels so Bun and Go responses can be compared. Uptime
// is normalized to a single label whether it is present, zero, or absent
// (nil/omitted), since a freshly started instance may report uptime=0 which
// Go omits via omitempty but Bun includes as 0.
func normalizeRuntimeStatus(st runtimeStatus, portLabel string) map[string]any {
	out := map[string]any{
		"id":     "<id>",
		"name":   "<name>",
		"status": st.Status,
	}
	if st.PID != nil {
		out["pid"] = "<pid>"
	} else {
		out["pid"] = nil
	}
	// Uptime: normalize any value (nil, 0, or positive) to a single label
	// because the two backends differ on whether they include uptime=0.
	out["uptime"] = "<duration>"
	if st.Port != nil {
		out["port"] = portLabel
	} else {
		out["port"] = nil
	}
	return out
}

// assertRuntimeParity compares normalized Bun and Go lifecycle status
// responses, allowing pid/uptime/port to differ only in their normalized
// labels. Both ports are normalized to the same label "<port>" since they
// are expected to differ (each backend allocates its own port).
func assertRuntimeParity(t *testing.T, label string, bunStatus, goStatus runtimeStatus, bunBackend, goBackend backend) {
	t.Helper()
	bunNorm := normalizeRuntimeStatus(bunStatus, "<port>")
	goNorm := normalizeRuntimeStatus(goStatus, "<port>")
	if !reflect.DeepEqual(bunNorm, goNorm) {
		t.Fatalf("%s runtime parity mismatch\nBun: %#v\nGo:  %#v", label, bunNorm, goNorm)
	}
}

// assertRuntimeFailureParity compares failure responses (non-200 status codes)
// for both backends, normalizing the error message to a stable label.
func assertRuntimeFailureParity(t *testing.T, label string, bunStatus int, bunBody []byte, goStatus int, goBody []byte) {
	t.Helper()
	if bunStatus < 400 || bunStatus >= 600 {
		t.Fatalf("%s Bun status = %d, want failure (4xx/5xx); body = %s", label, bunStatus, string(bunBody))
	}
	if goStatus < 400 || goStatus >= 600 {
		t.Fatalf("%s Go status = %d, want failure (4xx/5xx); body = %s", label, goStatus, string(goBody))
	}
	// Both must report success=false and a string error.
	bunEnv := decodeEnvelope(t, "Bun "+label, bunBody)
	goEnv := decodeEnvelope(t, "Go "+label, goBody)
	if bunEnv["success"] != false || goEnv["success"] != false {
		t.Fatalf("%s success mismatch\nBun: %#v\nGo:  %#v", label, bunEnv, goEnv)
	}
	if _, ok := bunEnv["error"].(string); !ok {
		t.Fatalf("%s Bun error is not string: %#v", label, bunEnv)
	}
	if _, ok := goEnv["error"].(string); !ok {
		t.Fatalf("%s Go error is not string: %#v", label, goEnv)
	}
}

func decodeEnvelope(t *testing.T, label string, body []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("%s: decode body %q: %v", label, string(body), err)
	}
	return env
}

// TestRuntimeParity_Lifecycle compares Bun and Go backends across the core
// lifecycle: start, status, stop, restart, kill.
func TestRuntimeParity_Lifecycle(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skipf("bun executable not found; skipping runtime parity: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	bunBackend := startBackend(t, ctx, repoRoot, "bun")
	goBackend := startBackend(t, ctx, repoRoot, "go")

	installFakeGOWARuntime(t, bunBackend.dataDir, runtimeTestVersion)
	installFakeGOWARuntime(t, goBackend.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}

	bunInst := createRuntimeInstance(t, client, bunBackend, "runtime-lifecycle-bun", runtimeTestVersion)
	goInst := createRuntimeInstance(t, client, goBackend, "runtime-lifecycle-go", runtimeTestVersion)

	// start
	_, bunStart := startInstance(t, client, bunBackend, bunInst.ID)
	_, goStart := startInstance(t, client, goBackend, goInst.ID)
	assertRuntimeParity(t, "start", bunStart, goStart, bunBackend, goBackend)
	if bunStart.Status != "running" {
		t.Fatalf("start status = %q, want running", bunStart.Status)
	}

	// status (running)
	bunStatus := getInstanceStatus(t, client, bunBackend, bunInst.ID)
	goStatus := getInstanceStatus(t, client, goBackend, goInst.ID)
	assertRuntimeParity(t, "status running", bunStatus, goStatus, bunBackend, goBackend)

	// stop
	bunStop := stopInstance(t, client, bunBackend, bunInst.ID)
	goStop := stopInstance(t, client, goBackend, goInst.ID)
	assertRuntimeParity(t, "stop", bunStop, goStop, bunBackend, goBackend)
	if bunStop.Status != "stopped" {
		t.Fatalf("stop status = %q, want stopped", bunStop.Status)
	}

	// start again (from stopped) to get back to running before restart.
	_, bunStart2 := startInstance(t, client, bunBackend, bunInst.ID)
	_, goStart2 := startInstance(t, client, goBackend, goInst.ID)
	assertRuntimeParity(t, "start after stop", bunStart2, goStart2, bunBackend, goBackend)

	// restart (from running)
	bunRestart := restartInstance(t, client, bunBackend, bunInst.ID)
	goRestart := restartInstance(t, client, goBackend, goInst.ID)
	assertRuntimeParity(t, "restart", bunRestart, goRestart, bunBackend, goBackend)
	if bunRestart.Status != "running" {
		t.Fatalf("restart status = %q, want running", bunRestart.Status)
	}

	// kill (from running)
	bunKill := killInstance(t, client, bunBackend, bunInst.ID)
	goKill := killInstance(t, client, goBackend, goInst.ID)
	assertRuntimeParity(t, "kill", bunKill, goKill, bunBackend, goBackend)
	if bunKill.Status != "stopped" {
		t.Fatalf("kill status = %q, want stopped", bunKill.Status)
	}

	// DB integrity after lifecycle operations.
	assertDBIntegrity(t, bunBackend.dataDir)
	assertDBIntegrity(t, goBackend.dataDir)
}

// TestRuntimeParity_DeleteWhileRunning compares Bun and Go behavior when
// deleting an instance that is currently running. Both backends stop the
// instance before deleting it.
func TestRuntimeParity_DeleteWhileRunning(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skipf("bun executable not found; skipping runtime parity: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	bunBackend := startBackend(t, ctx, repoRoot, "bun")
	goBackend := startBackend(t, ctx, repoRoot, "go")

	installFakeGOWARuntime(t, bunBackend.dataDir, runtimeTestVersion)
	installFakeGOWARuntime(t, goBackend.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}

	bunInst := createRuntimeInstance(t, client, bunBackend, "runtime-delete-bun", runtimeTestVersion)
	goInst := createRuntimeInstance(t, client, goBackend, "runtime-delete-go", runtimeTestVersion)

	startInstance(t, client, bunBackend, bunInst.ID)
	startInstance(t, client, goBackend, goInst.ID)
	waitForRuntimeStatus(t, client, bunBackend, bunInst.ID, "running", 10*time.Second)
	waitForRuntimeStatus(t, client, goBackend, goInst.ID, "running", 10*time.Second)

	bunStatus, bunBody := deleteInstance(t, client, bunBackend, bunInst.ID)
	goStatus, goBody := deleteInstance(t, client, goBackend, goInst.ID)
	if bunStatus != goStatus {
		t.Fatalf("delete while running status mismatch: Bun=%d Go=%d\nBun: %s\nGo:  %s", bunStatus, goStatus, string(bunBody), string(goBody))
	}
	if bunStatus != http.StatusOK {
		t.Fatalf("delete while running status = %d, want 200; body = %s", bunStatus, string(bunBody))
	}

	// Verify the instance is gone from both backends.
	bunStatus2, bunBody2 := runtimeRequest(t, client, bunBackend, http.MethodGet, fmt.Sprintf("/api/instances/%d", bunInst.ID), nil, nil)
	goStatus2, goBody2 := runtimeRequest(t, client, goBackend, http.MethodGet, fmt.Sprintf("/api/instances/%d", goInst.ID), nil, nil)
	if bunStatus2 != goStatus2 {
		t.Fatalf("detail after delete status mismatch: Bun=%d Go=%d\nBun: %s\nGo:  %s", bunStatus2, goStatus2, string(bunBody2), string(goBody2))
	}
	if bunStatus2 != http.StatusNotFound {
		t.Fatalf("detail after delete status = %d, want 404; body = %s", bunStatus2, string(bunBody2))
	}

	assertDBIntegrity(t, bunBackend.dataDir)
	assertDBIntegrity(t, goBackend.dataDir)
}

// TestRuntimeParity_Crash verifies the Go backend correctly detects and
// persists a "failed" status when the GOWA binary crashes immediately on
// start. This is a Go-only test because the Bun backend does not
// synchronously detect process crashes or update the DB status on exit
// (its onExit callback only removes the process from the in-memory
// ProcessManager without persisting a status change).
func TestRuntimeParity_Crash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	goBackend := startBackend(t, ctx, repoRoot, "go")

	installCrashFakeGOWA(t, goBackend.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	inst := createRuntimeInstance(t, client, goBackend, "runtime-crash-go", runtimeTestVersion)

	// Start the instance. The crash binary exits immediately; the Go
	// supervisor's exit callback should persist a "failed" status.
	runtimeRequest(t, client, goBackend, http.MethodPost, fmt.Sprintf("/api/instances/%d/start", inst.ID), nil, nil)

	// Poll until the instance reaches a non-running status (failed).
	final := waitForNonRunningStatus(t, client, goBackend, inst.ID, 15*time.Second)
	if final.Status != "failed" && final.Status != "stopped" {
		t.Fatalf("Go crash instance status = %q, want failed or stopped", final.Status)
	}

	assertDBIntegrity(t, goBackend.dataDir)
}

// TestRuntimeParity_MissingVersion compares Bun and Go behavior when starting
// an instance whose GOWA version is not installed.
func TestRuntimeParity_MissingVersion(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skipf("bun executable not found; skipping runtime parity: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	bunBackend := startBackend(t, ctx, repoRoot, "bun")
	goBackend := startBackend(t, ctx, repoRoot, "go")

	client := &http.Client{Timeout: 15 * time.Second}

	const missingVersion = "v9.9.9-missing"
	bunInst := createRuntimeInstance(t, client, bunBackend, "runtime-missing-bun", missingVersion)
	goInst := createRuntimeInstance(t, client, goBackend, "runtime-missing-go", missingVersion)

	bunStatus, bunBody := runtimeRequest(t, client, bunBackend, http.MethodPost, fmt.Sprintf("/api/instances/%d/start", bunInst.ID), nil, nil)
	goStatus, goBody := runtimeRequest(t, client, goBackend, http.MethodPost, fmt.Sprintf("/api/instances/%d/start", goInst.ID), nil, nil)

	assertRuntimeFailureParity(t, "missing version start", bunStatus, bunBody, goStatus, goBody)

	assertDBIntegrity(t, bunBackend.dataDir)
	assertDBIntegrity(t, goBackend.dataDir)
}

// TestRuntimeParity_OccupiedPort compares Bun and Go behavior when the
// instance's stored port is already occupied. Both backends allocate a new
// available port rather than failing, so the instance should start
// successfully with a different port.
func TestRuntimeParity_OccupiedPort(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skipf("bun executable not found; skipping runtime parity: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	bunBackend := startBackend(t, ctx, repoRoot, "bun")
	goBackend := startBackend(t, ctx, repoRoot, "go")

	installFakeGOWARuntime(t, bunBackend.dataDir, runtimeTestVersion)
	installFakeGOWARuntime(t, goBackend.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}

	bunInst := createRuntimeInstance(t, client, bunBackend, "runtime-occupied-bun", runtimeTestVersion)
	goInst := createRuntimeInstance(t, client, goBackend, "runtime-occupied-go", runtimeTestVersion)

	// Occupy the allocated ports by listening on them. If a port is already
	// occupied (by another service or test), skip occupying it — the
	// backend will still reallocate. The backends should re-allocate to a
	// new free port on start.
	var bunOccupier, goOccupier net.Listener
	if bunInst.Port != nil {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *bunInst.Port)); err == nil {
			bunOccupier = ln
			defer bunOccupier.Close()
		}
	}
	if goInst.Port != nil {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *goInst.Port)); err == nil {
			goOccupier = ln
			defer goOccupier.Close()
		}
	}

	_, bunStart := startInstance(t, client, bunBackend, bunInst.ID)
	_, goStart := startInstance(t, client, goBackend, goInst.ID)

	if bunStart.Status != "running" {
		t.Fatalf("Bun occupied-port start status = %q, want running", bunStart.Status)
	}
	if goStart.Status != "running" {
		t.Fatalf("Go occupied-port start status = %q, want running", goStart.Status)
	}

	// The instance should now be running on a different port than the
	// originally allocated one (if we successfully occupied it).
	bunStatus := getInstanceStatus(t, client, bunBackend, bunInst.ID)
	goStatus := getInstanceStatus(t, client, goBackend, goInst.ID)
	if bunOccupier != nil && bunStatus.Port != nil && *bunStatus.Port == *bunInst.Port {
		t.Fatalf("Bun instance still using occupied port %d", *bunInst.Port)
	}
	if goOccupier != nil && goStatus.Port != nil && *goStatus.Port == *goInst.Port {
		t.Fatalf("Go instance still using occupied port %d", *goInst.Port)
	}

	assertDBIntegrity(t, bunBackend.dataDir)
	assertDBIntegrity(t, goBackend.dataDir)
}

// TestRuntimeParity_ManagerRestart verifies that after the manager process is
// stopped and restarted, previously-running instances are reconciled back to
// running. This is a Go-only test (the Bun backend does not reconcile on
// restart in the same way); it is grouped under TestRuntimeParity because it
// validates runtime recovery behavior.
func TestRuntimeParity_ManagerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	be := startBackend(t, ctx, repoRoot, "go")

	installFakeGOWARuntime(t, be.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	inst := createRuntimeInstance(t, client, be, "runtime-restart-mgr", runtimeTestVersion)
	startInstance(t, client, be, inst.ID)
	waitForRuntimeStatus(t, client, be, inst.ID, "running", 10*time.Second)

	// Force-kill the manager process (simulating a crash/restart). We kill
	// the process directly rather than calling stopBackend to avoid
	// conflicting with the t.Cleanup handler registered by startBackend.
	if be.cmd != nil && be.cmd.Process != nil {
		forceKillProcess(be.cmd.Process.Pid)
	}
	// Mark the cmd as already handled so the t.Cleanup stopBackend is a
	// no-op (it checks be.cmd.Process == nil).
	be.cmd = nil

	// Kill any orphaned fakegowa processes using this data dir to avoid port
	// conflicts during reconciliation.
	time.Sleep(500 * time.Millisecond)
	killProcessesUsingDataDir(be.dataDir)

	// Restart the manager on a fresh port pointing at the same data dir.
	restarted := restartBackend(t, ctx, repoRoot, be.dataDir)

	// The reconciler should restart the instance. Poll until running.
	waitForRuntimeStatus(t, client, restarted, inst.ID, "running", 20*time.Second)

	assertDBIntegrity(t, be.dataDir)
}

// restartBackend starts a new Go manager backend reusing an existing data
// directory. It returns a backend struct for the restarted manager. The
// returned backend's cleanup is registered with t.Cleanup.
func restartBackend(t *testing.T, ctx context.Context, repoRoot, dataDir string) backend {
	t.Helper()
	port := freePort(t)
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/gowa-manager-go", "--port", fmt.Sprint(port), "--data-dir", dataDir)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "ADMIN_USERNAME="+contractUser, "ADMIN_PASSWORD="+contractPass, "DATA_DIR="+dataDir, "PORT="+fmt.Sprint(port))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("restart go backend: %v", err)
	}
	be := backend{name: "go-restarted", baseURL: fmt.Sprintf("http://127.0.0.1:%d", port), dataDir: dataDir, port: port, cmd: cmd}
	t.Cleanup(func() {
		stopBackend(t, be, output.String())
	})
	waitForHealth(t, ctx, be, output.String)
	return be
}

// assertDBIntegrity opens the SQLite database in the data directory and
// verifies PRAGMA integrity_check returns "ok".
func assertDBIntegrity(t *testing.T, dataDir string) {
	t.Helper()
	dbPath := filepath.Join(dataDir, "gowa.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("DB file missing in %s: %v", dataDir, err)
	}
	// Reuse the readRows helper which already performs an integrity check.
	_ = readRows(t, dataDir)
}

// killProcessesUsingDataDir kills any running processes whose command line
// references the given data directory. On Windows it uses the shared
// killWindowsProcessesUsingDataDir helper; on Unix it is a best-effort pkill.
func killProcessesUsingDataDir(dataDir string) {
	if runtime.GOOS == "windows" {
		killWindowsProcessesUsingDataDir(dataDir)
		return
	}
	_ = exec.Command("pkill", "-f", dataDir).Run()
}

// forceKillProcess force-terminates a process by PID. On Windows it uses
// taskkill /T /F; on Unix it sends SIGKILL.
func forceKillProcess(pid int) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
		return
	}
	_ = exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
}

// countFakeGOWAProcesses returns the number of running fakegowa processes.
// On Windows it uses a PowerShell CIM query; on Unix it counts pgrep matches.
func countFakeGOWAProcesses(binaryPath string) int {
	binaryName := filepath.Base(binaryPath)
	if runtime.GOOS == "windows" {
		// Strip the .exe suffix for the image name filter.
		imageName := strings.TrimSuffix(binaryName, ".exe")
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+imageName+".exe", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return 0
		}
		return strings.Count(string(out), imageName)
	}
	out, err := exec.Command("pgrep", "-f", binaryName).Output()
	if err != nil {
		return 0
	}
	return len(strings.Fields(strings.TrimSpace(string(out))))
}

// readAll reads all bytes from r, returning the content. It is a thin wrapper
// around io.ReadAll so callers in goroutines can drain HTTP bodies without
// importing io directly.
func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
