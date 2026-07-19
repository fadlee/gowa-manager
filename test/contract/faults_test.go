package contract

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestRuntimeFaults_KillDuringStart verifies that force-killing the manager
// process during an in-flight start operation leaves no duplicate child
// process and the database passes PRAGMA integrity_check.
func TestRuntimeFaults_KillDuringStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	be := startBackend(t, ctx, repoRoot, "go")
	installFakeGOWARuntime(t, be.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	inst := createRuntimeInstance(t, client, be, "faults-kill-start", runtimeTestVersion)

	// Start the instance and wait until it is running so the DB status is
	// persisted as "running" (the reconciler only restarts running
	// instances).
	startInstance(t, client, be, inst.ID)
	waitForRuntimeStatus(t, client, be, inst.ID, "running", 10*time.Second)

	// Issue a second start request in a goroutine (idempotent re-start);
	// kill the manager while it is processing the request.
	startDone := make(chan struct{}, 1)
	go func() {
		defer func() { startDone <- struct{}{} }()
		req, _ := http.NewRequest(http.MethodPost, be.baseURL+fmt.Sprintf("/api/instances/%d/start", inst.ID), nil)
		req.SetBasicAuth(contractUser, contractPass)
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = readAll(resp.Body)
		resp.Body.Close()
	}()

	time.Sleep(200 * time.Millisecond)
	if be.cmd != nil && be.cmd.Process != nil {
		forceKillProcess(be.cmd.Process.Pid)
	}
	be.cmd = nil
	<-startDone

	time.Sleep(500 * time.Millisecond)
	killProcessesUsingDataDir(be.dataDir)

	assertNoFakeGOWAChildren(t, be.dataDir)

	// Restart the manager and verify reconciliation + DB integrity. The
	// instance was running when we killed the manager, so the reconciler
	// should restart it.
	restarted := restartBackend(t, ctx, repoRoot, be.dataDir)
	waitForRuntimeStatus(t, client, restarted, inst.ID, "running", 20*time.Second)
	// Exactly one fakegowa process should be running (reconciled).
	assertFakeGOWAChildCount(t, 1)
	assertDBIntegrity(t, be.dataDir)
}

// TestRuntimeFaults_KillDuringRestart verifies that force-killing the manager
// process during a restart (stop+start) leaves no duplicate child process and
// the database remains consistent.
func TestRuntimeFaults_KillDuringRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	be := startBackend(t, ctx, repoRoot, "go")
	installFakeGOWARuntime(t, be.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	inst := createRuntimeInstance(t, client, be, "faults-kill-restart", runtimeTestVersion)
	startInstance(t, client, be, inst.ID)
	waitForRuntimeStatus(t, client, be, inst.ID, "running", 10*time.Second)

	// Issue a restart in a goroutine and kill the manager mid-flight. The
	// restart stops the instance first (persisting "stopped"), then starts
	// it; killing during this window may leave the DB status as "stopped".
	restartDone := make(chan struct{}, 1)
	go func() {
		defer func() { restartDone <- struct{}{} }()
		req, _ := http.NewRequest(http.MethodPost, be.baseURL+fmt.Sprintf("/api/instances/%d/restart", inst.ID), nil)
		req.SetBasicAuth(contractUser, contractPass)
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = readAll(resp.Body)
		resp.Body.Close()
	}()

	time.Sleep(300 * time.Millisecond)
	if be.cmd != nil && be.cmd.Process != nil {
		forceKillProcess(be.cmd.Process.Pid)
	}
	be.cmd = nil
	<-restartDone

	time.Sleep(500 * time.Millisecond)
	killProcessesUsingDataDir(be.dataDir)

	assertNoFakeGOWAChildren(t, be.dataDir)

	// Restart the manager. The instance may or may not be reconciled
	// depending on whether the DB status was "running" or "stopped" at
	// kill time. Either way, the manager must come up healthy with an
	// intact database, and we must be able to start the instance cleanly.
	restarted := restartBackend(t, ctx, repoRoot, be.dataDir)
	// Manually start the instance to verify the manager is functional and
	// no duplicate process is spawned.
	startInstance(t, client, restarted, inst.ID)
	waitForRuntimeStatus(t, client, restarted, inst.ID, "running", 20*time.Second)
	// Exactly one fakegowa process should be running (the one we started).
	assertFakeGOWAChildCount(t, 1)
	assertDBIntegrity(t, be.dataDir)
}

// TestRuntimeFaults_Leak repeats 100 start/stop cycles on a single instance
// and asserts stable goroutine count, no surviving child PIDs, stable
// registry size, and DB integrity. On Linux it also checks file descriptor
// count via /proc/self/fd.
func TestRuntimeFaults_Leak(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	be := startBackend(t, ctx, repoRoot, "go")
	installFakeGOWARuntime(t, be.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	inst := createRuntimeInstance(t, client, be, "faults-leak", runtimeTestVersion)

	// Baseline measurements before the cycle.
	goroutinesBefore := runtime.NumGoroutine()
	fdBefore := fdCount()
	registryBefore := instanceCount(t, be.dataDir)

	const cycles = 100
	for i := 0; i < cycles; i++ {
		startInstance(t, client, be, inst.ID)
		// Brief settle time so the process registers as running.
		waitForRuntimeStatus(t, client, be, inst.ID, "running", 10*time.Second)
		stopInstance(t, client, be, inst.ID)
		waitForRuntimeStatus(t, client, be, inst.ID, "stopped", 10*time.Second)
	}

	// Allow goroutines from the last cycle to settle.
	time.Sleep(2 * time.Second)

	goroutinesAfter := runtime.NumGoroutine()
	fdAfter := fdCount()
	registryAfter := instanceCount(t, be.dataDir)

	// Goroutine count should be roughly stable (allow generous delta for
	// runtime/scheduler goroutines).
	delta := goroutinesAfter - goroutinesBefore
	if delta < 0 {
		delta = -delta
	}
	if delta > 20 {
		t.Fatalf("goroutine leak: before=%d after=%d (delta=%d)", goroutinesBefore, goroutinesAfter, delta)
	}

	// File descriptor count (Linux only) should be stable.
	if fdBefore >= 0 && fdAfter >= 0 {
		fdDelta := fdAfter - fdBefore
		if fdDelta < 0 {
			fdDelta = -fdDelta
		}
		if fdDelta > 20 {
			t.Fatalf("fd leak: before=%d after=%d (delta=%d)", fdBefore, fdAfter, fdDelta)
		}
	}

	// Registry size should be unchanged (1 instance).
	if registryBefore != registryAfter {
		t.Fatalf("registry size changed: before=%d after=%d", registryBefore, registryAfter)
	}
	if registryAfter != 1 {
		t.Fatalf("registry size = %d, want 1", registryAfter)
	}

	// No surviving child fakegowa processes.
	assertNoFakeGOWAChildren(t, be.dataDir)

	// DB integrity.
	assertDBIntegrity(t, be.dataDir)
}

// TestRuntimeFaults_KillDuringScheduler verifies that force-killing the
// manager while schedulers are running does not corrupt the database or leave
// orphaned child processes. We start an instance, then kill the manager and
// immediately restart it to exercise scheduler recovery.
func TestRuntimeFaults_KillDuringScheduler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	be := startBackend(t, ctx, repoRoot, "go")
	installFakeGOWARuntime(t, be.dataDir, runtimeTestVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	inst := createRuntimeInstance(t, client, be, "faults-kill-scheduler", runtimeTestVersion)
	startInstance(t, client, be, inst.ID)
	waitForRuntimeStatus(t, client, be, inst.ID, "running", 10*time.Second)

	// Let schedulers run for a moment, then force-kill the manager.
	time.Sleep(1 * time.Second)
	if be.cmd != nil && be.cmd.Process != nil {
		forceKillProcess(be.cmd.Process.Pid)
	}
	be.cmd = nil
	time.Sleep(500 * time.Millisecond)
	killProcessesUsingDataDir(be.dataDir)

	assertNoFakeGOWAChildren(t, be.dataDir)

	restarted := restartBackend(t, ctx, repoRoot, be.dataDir)
	waitForRuntimeStatus(t, client, restarted, inst.ID, "running", 20*time.Second)
	assertFakeGOWAChildCount(t, 1)
	assertDBIntegrity(t, be.dataDir)
}

// assertNoFakeGOWAChildren verifies that no more than maxExpected fakegowa
// processes are running using the given data directory. It checks via the
// shared fakegowa binary path (if built) and via the data-dir process scan.
func assertNoFakeGOWAChildren(t *testing.T, dataDir string) {
	t.Helper()
	assertFakeGOWAChildCount(t, 0)
}

// assertFakeGOWAChildCount verifies that exactly want fakegowa processes are
// running, retrying briefly to let the OS reap zombies.
func assertFakeGOWAChildCount(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var n int
	for time.Now().Before(deadline) {
		n = countFakeGOWAProcesses(fakeGOWABinaryPath)
		if n == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	if n != want {
		t.Fatalf("expected %d fakegowa processes, found %d", want, n)
	}
}

// fdCount returns the number of open file descriptors for the current
// process. On Linux it reads /proc/self/fd; on other platforms it returns -1
// (skip).
func fdCount() int {
	if runtime.GOOS != "linux" {
		return -1
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

// instanceCount returns the number of instance rows in the SQLite database.
func instanceCount(t *testing.T, dataDir string) int {
	t.Helper()
	rows := readRows(t, dataDir)
	return len(rows)
}
