//go:build linux

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var linuxFakeGOWAOnce struct {
	path string
	err  error
}

func TestLinuxProcessLaunchesInOwnProcessGroup(t *testing.T) {
	proc := startLinuxProcess(t, "serve", nil)

	pgid, err := syscall.Getpgid(proc.PID())
	if err != nil {
		t.Fatalf("Getpgid(%d) error = %v", proc.PID(), err)
	}
	if pgid != proc.PID() {
		t.Fatalf("process group = %d, want pid %d", pgid, proc.PID())
	}
}

func TestLinuxProcessGracefulStopSignalsProcessGroup(t *testing.T) {
	proc := startLinuxProcess(t, "serve", nil)

	if err := proc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForLinuxProcessExit(t, proc, 3*time.Second)
	if linuxProcessExists(proc.PID()) {
		t.Fatalf("process %d still exists after Stop", proc.PID())
	}
}

func TestLinuxProcessForcedKillTerminatesIgnoredSignal(t *testing.T) {
	proc := startLinuxProcess(t, "ignore-term", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := proc.Stop(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForLinuxProcessExit(t, proc, 3*time.Second)
	if linuxProcessExists(proc.PID()) {
		t.Fatalf("process %d still exists after forced Stop", proc.PID())
	}
}

func TestLinuxProcessTerminatesSpawnedDescendants(t *testing.T) {
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	proc := startLinuxProcess(t, "spawn-child", map[string]string{
		"FAKE_GOWA_CHILD_PID_FILE": childPIDFile,
	})
	childPID := waitForLinuxPIDFile(t, childPIDFile, 3*time.Second)
	if !linuxProcessExists(childPID) {
		t.Fatalf("child process %d was not running before Stop", childPID)
	}

	if err := proc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForLinuxPIDExit(t, proc.PID(), 3*time.Second)
	waitForLinuxPIDExit(t, childPID, 3*time.Second)
}

func TestLinuxProcessWaitReapsChild(t *testing.T) {
	proc := startLinuxProcess(t, "serve", nil)

	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	waitForLinuxProcessExit(t, proc, 3*time.Second)
	if linuxProcessState(t, proc.PID()) == "Z" {
		t.Fatalf("process %d was not reaped", proc.PID())
	}
}

func TestLinuxProcessCancellationBeforeStartLeavesNoProcess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proc, err := startPlatformProcess(ctx, platformProcessConfig{
		Path: linuxFakeGOWABinary(t),
		Args: []string{"rest", "--port=0"},
		Env:  map[string]string{"FAKE_GOWA_MODE": "serve", "FAKE_GOWA_PID_FILE": pidFile},
	})
	if err == nil {
		_ = proc.Close()
		t.Fatalf("startPlatformProcess() error = nil, want cancellation")
	}
	if proc != nil {
		t.Fatalf("startPlatformProcess() proc = %#v, want nil", proc)
	}
	if data, readErr := os.ReadFile(pidFile); readErr == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr != nil {
			t.Fatalf("parse pid file %s: %v", pidFile, parseErr)
		}
		if linuxProcessExists(pid) {
			t.Fatalf("cancelled start left process %d running", pid)
		}
	}
}

func startLinuxProcess(t *testing.T, mode string, env map[string]string) *linuxProcess {
	t.Helper()
	port := freeLinuxPort(t)
	mergedEnv := map[string]string{"FAKE_GOWA_MODE": mode}
	for key, value := range env {
		mergedEnv[key] = value
	}
	proc, err := startPlatformProcess(context.Background(), platformProcessConfig{
		Path: linuxFakeGOWABinary(t),
		Args: []string{"rest", "--port=" + strconv.Itoa(port)},
		Env:  mergedEnv,
	})
	if err != nil {
		t.Fatalf("startPlatformProcess() error = %v", err)
	}
	t.Cleanup(func() { cleanupLinuxProcess(t, proc) })
	waitForLinuxHealth(t, port, 3*time.Second)
	return proc
}

func cleanupLinuxProcess(t *testing.T, proc *linuxProcess) {
	t.Helper()
	if proc == nil {
		return
	}
	_ = proc.Kill()
	_ = proc.Close()
}

func linuxFakeGOWABinary(t *testing.T) string {
	t.Helper()
	if linuxFakeGOWAOnce.path != "" || linuxFakeGOWAOnce.err != nil {
		if linuxFakeGOWAOnce.err != nil {
			t.Fatalf("build fakegowa: %v", linuxFakeGOWAOnce.err)
		}
		return linuxFakeGOWAOnce.path
	}
	path := filepath.Join(os.TempDir(), "gowa-manager-linux-test-fakegowa")
	cmd := exec.Command("go", "build", "-o", path, ".")
	cmd.Dir = filepath.Join("..", "testutil", "fakegowa")
	output, err := cmd.CombinedOutput()
	if err != nil {
		linuxFakeGOWAOnce.err = fmt.Errorf("%v\n%s", err, string(output))
		t.Fatalf("build fakegowa: %v\n%s", err, string(output))
	}
	linuxFakeGOWAOnce.path = path
	return path
}

func freeLinuxPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForLinuxHealth(t *testing.T, port int, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	var lastErr error
	for time.Now().Before(end) {
		client := http.Client{Timeout: 200 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("health endpoint did not become ready on port %d within %s; last error: %v", port, deadline, lastErr)
}

func waitForLinuxProcessExit(t *testing.T, proc *linuxProcess, deadline time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	if err := proc.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func waitForLinuxPIDExit(t *testing.T, pid int, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if !linuxProcessExists(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after %s", pid, deadline)
}

func waitForLinuxPIDFile(t *testing.T, path string, deadline time.Duration) int {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse pid file %s: %v", path, parseErr)
			}
			return pid
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}

func linuxProcessExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func linuxProcessState(t *testing.T, pid int) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read process stat for %d: %v", pid, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		t.Fatalf("unexpected process stat for %d: %q", pid, string(data))
	}
	return fields[2]
}
