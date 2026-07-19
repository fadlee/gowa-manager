//go:build windows

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
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsProcessAssignsProcessToJobObject(t *testing.T) {
	proc := startWindowsProcess(t, "serve", nil)
	defer cleanupWindowsProcess(t, proc)

	if proc.PID() <= 0 {
		t.Fatalf("PID() = %d, want positive PID", proc.PID())
	}
	if !processInJob(t, proc.PID()) {
		t.Fatalf("process %d is not assigned to a job object", proc.PID())
	}
}

func TestWindowsProcessGracefulStopTerminatesProcess(t *testing.T) {
	proc := startWindowsProcess(t, "serve", nil)

	if err := proc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForWindowsProcessExit(t, proc, 3*time.Second)
	if processExists(proc.PID()) {
		t.Fatalf("process %d still exists after Stop", proc.PID())
	}
	if err := proc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestWindowsProcessForceKillTerminatesJobObject(t *testing.T) {
	proc := startWindowsProcess(t, "ignore-term", nil)

	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	waitForWindowsProcessExit(t, proc, 3*time.Second)
	if processExists(proc.PID()) {
		t.Fatalf("process %d still exists after Kill", proc.PID())
	}
	if err := proc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestWindowsProcessForceKillCleansDescendants(t *testing.T) {
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	proc := startWindowsProcess(t, "spawn-child", map[string]string{
		"FAKE_GOWA_CHILD_PID_FILE": childPIDFile,
	})
	childPID := waitForWindowsPIDFile(t, childPIDFile, 3*time.Second)
	if !processExists(childPID) {
		t.Fatalf("child process %d was not running before Kill", childPID)
	}

	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	waitForWindowsPIDExit(t, proc.PID(), 3*time.Second)
	waitForWindowsPIDExit(t, childPID, 3*time.Second)
	if err := proc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestWindowsProcessRepeatedStopKillCloseAreIdempotent(t *testing.T) {
	proc := startWindowsProcess(t, "serve", nil)

	for i := 0; i < 3; i++ {
		if err := proc.Stop(context.Background()); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("Stop() attempt %d error = %v", i+1, err)
		}
	}
	waitForWindowsProcessExit(t, proc, 3*time.Second)
	for i := 0; i < 3; i++ {
		if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("Kill() attempt %d error = %v", i+1, err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := proc.Close(); err != nil {
			t.Fatalf("Close() attempt %d error = %v", i+1, err)
		}
	}
}

func startWindowsProcess(t *testing.T, mode string, env map[string]string) *windowsProcess {
	t.Helper()
	port := freeWindowsPort(t)
	mergedEnv := map[string]string{"FAKE_GOWA_MODE": mode}
	for key, value := range env {
		mergedEnv[key] = value
	}
	proc, err := startPlatformProcess(context.Background(), platformProcessConfig{
		Path: fakeGOWABinary(t),
		Args: []string{"rest", "--port=" + strconv.Itoa(port)},
		Env:  mergedEnv,
	})
	if err != nil {
		t.Fatalf("startPlatformProcess() error = %v", err)
	}
	t.Cleanup(func() { cleanupWindowsProcess(t, proc) })
	waitForWindowsHealth(t, port, 3*time.Second)
	return proc
}

func cleanupWindowsProcess(t *testing.T, proc *windowsProcess) {
	t.Helper()
	if proc == nil {
		return
	}
	_ = proc.Kill()
	_ = proc.Close()
}

func fakeGOWABinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), executableName("fakegowa"))
	cmd := exec.Command("go", fakeGOWABuildArgs(path)...)
	cmd.Dir = filepath.Join("..", "testutil", "fakegowa")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fakegowa: %v\n%s", err, string(output))
	}
	return path
}

func fakeGOWABuildArgs(outputPath string) []string {
	return []string{"build", "-o", outputPath, "."}
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func freeWindowsPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForWindowsHealth(t *testing.T, port int, deadline time.Duration) {
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

func waitForWindowsProcessExit(t *testing.T, proc *windowsProcess, deadline time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	if err := proc.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func waitForWindowsPIDExit(t *testing.T, pid int, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if !processExists(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after %s", pid, deadline)
}

func waitForWindowsPIDFile(t *testing.T, path string, deadline time.Duration) int {
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

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid))
}

func processInJob(t *testing.T, pid int) bool {
	t.Helper()
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatalf("OpenProcess(%d): %v", pid, err)
	}
	defer windows.CloseHandle(handle)
	var inJob uint32
	r1, _, err := procIsProcessInJob.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&inJob)))
	if r1 == 0 {
		t.Fatalf("IsProcessInJob(%d): %v", pid, err)
	}
	return inJob != 0
}
