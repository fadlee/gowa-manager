package main

import (
	"context"
	"fmt"
	"io"
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
)

func TestServeWritesPIDFileAndRespondsToHealth(t *testing.T) {
	port := freePort(t)
	pidFile := filepath.Join(t.TempDir(), "fakegowa.pid")

	cmd := startFakeGOWA(t, []string{"rest", "--port=" + strconv.Itoa(port)}, map[string]string{
		"FAKE_GOWA_MODE":     "serve",
		"FAKE_GOWA_PID_FILE": pidFile,
	})

	waitForHealth(t, port, 2*time.Second)
	assertPIDFile(t, pidFile, cmd.Process.Pid)
}

func TestCrashExitsWithSelectedExitCode(t *testing.T) {
	cmd := newFakeGOWACmd(t, nil, map[string]string{
		"FAKE_GOWA_MODE":      "crash",
		"FAKE_GOWA_EXIT_CODE": "7",
	})

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected process to exit with failure")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected exec.ExitError, got %T: %v", err, err)
	}
	if got := exitErr.ExitCode(); got != 7 {
		t.Fatalf("exit code = %d, want 7", got)
	}
}

func TestDelayedReadyWaitsBeforeListening(t *testing.T) {
	port := freePort(t)
	cmd := startFakeGOWA(t, []string{"rest", "--port", strconv.Itoa(port)}, map[string]string{
		"FAKE_GOWA_MODE":           "delayed-ready",
		"FAKE_GOWA_READY_DELAY_MS": "350",
	})

	if healthOK(port) == nil {
		t.Fatal("health endpoint responded before ready delay elapsed")
	}

	start := time.Now()
	waitForHealth(t, port, 2*time.Second)
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("health endpoint became ready too early after %s", elapsed)
	}
	_ = cmd
}

func TestServeSupportsShortPortFlag(t *testing.T) {
	port := freePort(t)
	startFakeGOWA(t, []string{"rest", "-p", strconv.Itoa(port)}, map[string]string{
		"FAKE_GOWA_MODE": "serve",
	})

	waitForHealth(t, port, 2*time.Second)
}

func TestServePortZeroWritesActualPortFile(t *testing.T) {
	portFile := filepath.Join(t.TempDir(), "port")
	startFakeGOWA(t, []string{"rest", "--port=0"}, map[string]string{
		"FAKE_GOWA_MODE":      "serve",
		"FAKE_GOWA_PORT_FILE": portFile,
	})

	port := waitForPortFile(t, portFile, 2*time.Second)
	waitForHealth(t, port, 2*time.Second)
}

func TestSpawnChildReportsChildPID(t *testing.T) {
	port := freePort(t)
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")

	startFakeGOWA(t, []string{"--port", strconv.Itoa(port)}, map[string]string{
		"FAKE_GOWA_MODE":           "spawn-child",
		"FAKE_GOWA_CHILD_PID_FILE": childPIDFile,
	})

	waitForHealth(t, port, 2*time.Second)
	childPID := waitForPIDFile(t, childPIDFile, 2*time.Second)
	t.Cleanup(func() { killPID(childPID) })
	if childPID <= 0 {
		t.Fatalf("child pid = %d, want positive pid", childPID)
	}
	if !processExists(childPID) {
		t.Fatalf("reported child process %d is not running", childPID)
	}
}

func TestProcessExistsRejectsExitedProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessExitsImmediately")
	cmd.Env = append(os.Environ(), "FAKE_GOWA_HELPER_PROCESS=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for helper process: %v", err)
	}

	if processExists(pid) {
		t.Fatalf("processExists(%d) = true for exited process", pid)
	}
}

func TestHelperProcessExitsImmediately(t *testing.T) {
	if os.Getenv("FAKE_GOWA_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(0)
}

func TestFakeGOWABuildIncludesRaceFlagUnderRace(t *testing.T) {
	args := fakeGOWABuildArgs(filepath.Join(t.TempDir(), executableName("fakegowa")))
	hasRace := false
	for _, arg := range args {
		if arg == "-race" {
			hasRace = true
		}
	}
	if raceDetectorEnabled() != hasRace {
		t.Fatalf("build args %v include -race = %v, want %v", args, hasRace, raceDetectorEnabled())
	}
}

func TestIgnoreTermSurvivesInterruptAndCanBeKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt is not a meaningful graceful child-process signal on Windows")
	}

	port := freePort(t)
	cmd := startFakeGOWA(t, []string{"rest", "--port=" + strconv.Itoa(port)}, map[string]string{
		"FAKE_GOWA_MODE": "ignore-term",
	})
	waitForHealth(t, port, 2*time.Second)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send interrupt: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := cmd.Process.Signal(os.Signal(nil)); err != nil {
		t.Fatalf("process exited after interrupt, want still running: %v", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill process: %v", err)
	}
}

func TestLoadModeServesHealth(t *testing.T) {
	port := freePort(t)
	startFakeGOWA(t, []string{"rest", "--port=" + strconv.Itoa(port)}, map[string]string{
		"FAKE_GOWA_MODE":       "load",
		"FAKE_GOWA_LOAD_BYTES": "1048576",
	})

	waitForHealth(t, port, 2*time.Second)
}

func startFakeGOWA(t *testing.T, args []string, env map[string]string) *exec.Cmd {
	t.Helper()
	cmd := newFakeGOWACmd(t, args, env)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fakegowa: %v", err)
	}
	t.Cleanup(func() { cleanupProcess(cmd) })
	return cmd
}

func newFakeGOWACmd(t *testing.T, args []string, env map[string]string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, fakeGOWABinary(t), args...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	t.Cleanup(func() {
		if t.Failed() && output.Len() > 0 {
			t.Logf("fakegowa output:\n%s", output.String())
		}
	})
	return cmd
}

func fakeGOWABinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), executableName("fakegowa"))
	cmd := exec.Command("go", fakeGOWABuildArgs(path)...)
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fakegowa with args %v: %v\n%s", cmd.Args, err, string(output))
	}
	return path
}

func fakeGOWABuildArgs(outputPath string) []string {
	args := []string{"build"}
	if raceDetectorEnabled() {
		args = append(args, "-race")
	}
	return append(args, "-o", outputPath, ".")
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func cleanupProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForHealth(t *testing.T, port int, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	var lastErr error
	for time.Now().Before(end) {
		if err := healthOK(port); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("health endpoint did not become ready on port %d within %s; last error: %v", port, deadline, lastErr)
}

func healthOK(port int) error {
	client := http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok") {
		return fmt.Errorf("status %d body %q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func assertPIDFile(t *testing.T, path string, want int) {
	t.Helper()
	got := waitForPIDFile(t, path, 2*time.Second)
	if got != want {
		t.Fatalf("pid file = %d, want %d", got, want)
	}
}

func waitForPIDFile(t *testing.T, path string, deadline time.Duration) int {
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

func waitForPortFile(t *testing.T, path string, deadline time.Duration) int {
	t.Helper()
	end := time.Now().Add(deadline)
	var lastErr error
	for time.Now().Before(end) {
		data, err := os.ReadFile(path)
		if err == nil {
			port, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse port file %s: %v", path, parseErr)
			}
			if port <= 0 {
				t.Fatalf("port file %s = %d, want positive port", path, port)
			}
			return port
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("port file %s was not written within %s; last error: %v", path, deadline, lastErr)
	return 0
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH")
		output, err := cmd.Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid))
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(os.Signal(nil)) == nil
}

func killPID(pid int) {
	if pid <= 0 {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
		return
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		_ = proc.Kill()
	}
}
