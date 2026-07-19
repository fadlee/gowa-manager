package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// Rollback test helpers
// ---------------------------------------------------------------------------

// spawnSleeperProcess starts a long-running process (used as a fake "Go
// PID" for rollback tests).  Returns the PID and a cleanup function.
func spawnSleeperProcess(t *testing.T) (int, func()) {
	t.Helper()
	dir := t.TempDir()
	name := "sleeper"
	if runtime.GOOS == "windows" {
		name = "sleeper.exe"
	}
	binPath := filepath.Join(dir, name)
	src := []byte("package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(10 * time.Minute) }\n")
	srcFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcFile, src, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build sleeper: %v\n%s", err, out)
	}
	proc := exec.Command(binPath)
	if err := proc.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	pid := proc.Process.Pid
	cleanup := func() {
		if proc.Process != nil {
			_ = proc.Process.Kill()
			_, _ = proc.Process.Wait()
		}
	}
	return pid, cleanup
}

// buildFakeBunForRollback builds a tiny Go binary that acts as the "Bun
// binary" for rollback tests.  It listens on the port specified by the
// FAKE_BUN_PORT env var and responds to smoke test endpoints.  Returns
// the binary path.  The binary is built into a manual temp dir (not
// t.TempDir) to avoid cleanup issues when the process is still running.
func buildFakeBunForRollback(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gowa-ops-fakebun-")
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

// killFakeBun kills any running fake-bun processes to ensure test
// isolation and allow temp dir cleanup.  Best-effort; failures ignored.
func killFakeBun(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/IM", "fake-bun.exe", "/F").Run()
	} else {
		_ = exec.Command("pkill", "-f", "fake-bun").Run()
	}
}

// computeFileSHA256 computes the SHA-256 hex digest of a file.
func computeFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Rollback tests
// ---------------------------------------------------------------------------

// TestRollback_DryRunExitsZero verifies that dry-run mode (default) exits 0
// and makes no changes.
func TestRollback_DryRunExitsZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	r := runScript(t, "rollback", []string{})

	assertExitCode(t, r, 0)
	if r.JSON["tool"] != "rollback" {
		t.Fatalf("tool = %v, want rollback", r.JSON["tool"])
	}
	if r.JSON["mode"] != "dry_run" {
		t.Fatalf("mode = %v, want dry_run", r.JSON["mode"])
	}
	if r.JSON["execute"] != false {
		t.Fatalf("execute = %v, want false", r.JSON["execute"])
	}
}

// TestRollback_DryRunNoChanges verifies that dry-run mode does not stop
// any processes or make filesystem changes.
func TestRollback_DryRunNoChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	// Spawn a sleeper process; dry-run must NOT kill it.
	pid, cleanup := spawnSleeperProcess(t)
	defer cleanup()

	r := runScript(t, "rollback", []string{
		"--go-pid", fmt.Sprint(pid),
	})

	assertExitCode(t, r, 0)

	// Verify the sleeper is still alive.
	if !processAlive(pid) {
		t.Fatal("dry-run mode killed the Go process — should not have")
	}
}

// processAlive checks whether a process with the given PID is running.
func processAlive(pid int) bool {
	if runtime.GOOS == "windows" {
		// tasklist returns non-zero if the process doesn't exist.
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), fmt.Sprint(pid))
	}
	// On Unix, kill -0 checks existence.
	cmd := exec.Command("kill", "-0", fmt.Sprint(pid))
	return cmd.Run() == nil
}

// TestRollback_ExecuteRequired verifies that without --execute, the script
// runs in dry-run mode.
func TestRollback_ExecuteRequired(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	// Run without --execute — should be dry-run.
	r := runScript(t, "rollback", []string{
		"--go-pid", "99999",
		"--bun-binary", "/nonexistent",
	})

	assertExitCode(t, r, 0)
	if r.JSON["mode"] != "dry_run" {
		t.Fatalf("mode = %v, want dry_run (no --execute)", r.JSON["mode"])
	}
}

// TestRollback_MissingRequiredArgs verifies that --execute with missing
// required args fails.
func TestRollback_MissingRequiredArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	// --execute with no other args — should fail.
	r := runScript(t, "rollback", []string{
		"--execute",
	})

	assertExitCode(t, r, 1)
	errs, ok := r.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors for missing required args, got %v\nstdout: %s",
			r.JSON["errors"], r.RawStdout)
	}
}

// TestRollback_MissingSomeRequiredArgs verifies that --execute with only
// some required args fails.
func TestRollback_MissingSomeRequiredArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	// --execute with only --go-pid — should fail for missing args.
	r := runScript(t, "rollback", []string{
		"--execute",
		"--go-pid", "99999",
	})

	assertExitCode(t, r, 1)
	errs, ok := r.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors for missing required args, got %v\nstdout: %s",
			r.JSON["errors"], r.RawStdout)
	}
}

// TestRollback_ChecksumVerification verifies that the Bun binary checksum
// is verified.  A wrong checksum should cause failure.
func TestRollback_ChecksumVerification(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bunBinary := buildFakeBunForRollback(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "rollback-backup")

	// Spawn a sleeper as the "Go PID".
	pid, cleanup := spawnSleeperProcess(t)
	defer cleanup()

	// Use a deliberately wrong checksum.
	r := runScript(t, "rollback", []string{
		"--execute",
		"--backup-dir", backupDir,
		"--go-pid", fmt.Sprint(pid),
		"--go-version", "test-v1.0.0",
		"--bun-binary", bunBinary,
		"--bun-checksum", "0000000000000000000000000000000000000000000000000000000000000000",
		"--data-dir", dataDir,
		"--bun-url", "http://127.0.0.1:1", // unreachable so smoke won't pass
	})

	// Should fail due to checksum mismatch (or earlier step).
	if r.ExitCode == 0 {
		t.Fatalf("expected non-zero exit for wrong checksum, got 0\nstdout: %s", r.RawStdout)
	}
	// Look for a checksum-related error or step failure.
	steps, ok := r.JSON["steps"].([]any)
	if !ok {
		t.Fatalf("steps not an array: %v", r.JSON["steps"])
	}
	foundChecksum := false
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := m["name"].(string); strings.Contains(name, "checksum") {
			if status, _ := m["status"].(string); status == "fail" {
				foundChecksum = true
			}
		}
	}
	if !foundChecksum {
		t.Fatalf("expected checksum step to fail, steps: %v\nstdout: %s", steps, r.RawStdout)
	}
}

// TestRollback_ChecksumMatchProceeds verifies that a correct checksum
// allows the rollback to proceed past the checksum step.
func TestRollback_ChecksumMatchProceeds(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bunBinary := buildFakeBunForRollback(t)
	correctChecksum := computeFileSHA256(t, bunBinary)
	backupDir := filepath.Join(filepath.Dir(dataDir), "rollback-backup-ok")

	// Spawn a sleeper as the "Go PID".
	pid, cleanup := spawnSleeperProcess(t)
	defer cleanup()
	defer killFakeBun(t) // kill any fake-bun started by the rollback script

	r := runScript(t, "rollback", []string{
		"--execute",
		"--backup-dir", backupDir,
		"--go-pid", fmt.Sprint(pid),
		"--go-version", "test-v1.0.0",
		"--bun-binary", bunBinary,
		"--bun-checksum", correctChecksum,
		"--data-dir", dataDir,
		"--bun-url", "http://127.0.0.1:1", // unreachable so smoke will fail
	})

	// The rollback may still fail at the smoke test step (Bun URL unreachable),
	// but the checksum step should pass.
	steps, ok := r.JSON["steps"].([]any)
	if !ok {
		t.Fatalf("steps not an array: %v", r.JSON["steps"])
	}
	checksumPassed := false
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := m["name"].(string); name == "verify_bun_checksum" {
			if status, _ := m["status"].(string); status == "pass" {
				checksumPassed = true
			}
		}
	}
	if !checksumPassed {
		t.Fatalf("expected verify_bun_checksum step to pass, steps: %v\nstdout: %s", steps, r.RawStdout)
	}
}

// TestRollback_AmbiguousGoPidNotRunning verifies that when the Go PID is
// not running and --override-ambiguous-state is NOT set, the rollback
// fails.
func TestRollback_AmbiguousGoPidNotRunning(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bunBinary := buildFakeBunForRollback(t)
	correctChecksum := computeFileSHA256(t, bunBinary)
	backupDir := filepath.Join(filepath.Dir(dataDir), "rollback-ambig")

	// Use a PID that is very unlikely to exist.
	r := runScript(t, "rollback", []string{
		"--execute",
		"--backup-dir", backupDir,
		"--go-pid", "999999",
		"--go-version", "test-v1.0.0",
		"--bun-binary", bunBinary,
		"--bun-checksum", correctChecksum,
		"--data-dir", dataDir,
	})

	assertExitCode(t, r, 1)
	errs, ok := r.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors for ambiguous Go PID, got %v\nstdout: %s",
			r.JSON["errors"], r.RawStdout)
	}
	found := false
	for _, e := range errs {
		if s, ok := e.(string); ok && strings.Contains(s, "not running") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about Go PID not running, got %v", errs)
	}
}

// TestRollback_OverrideAmbiguousState verifies that --override-ambiguous-state
// allows the rollback to proceed when the Go PID is not running.
func TestRollback_OverrideAmbiguousState(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bunBinary := buildFakeBunForRollback(t)
	correctChecksum := computeFileSHA256(t, bunBinary)
	backupDir := filepath.Join(filepath.Dir(dataDir), "rollback-override")

	defer killFakeBun(t) // kill any fake-bun started by the rollback script

	r := runScript(t, "rollback", []string{
		"--execute",
		"--backup-dir", backupDir,
		"--go-pid", "999999",
		"--go-version", "test-v1.0.0",
		"--bun-binary", bunBinary,
		"--bun-checksum", correctChecksum,
		"--data-dir", dataDir,
		"--bun-url", "http://127.0.0.1:1", // unreachable so smoke will fail
		"--override-ambiguous-state",
	})

	// The stop_go step should pass (with override), even though PID 999999
	// doesn't exist.  The overall rollback may still fail at a later step
	// (e.g. smoke), but stop_go should not be the failure.
	steps, ok := r.JSON["steps"].([]any)
	if !ok {
		t.Fatalf("steps not an array: %v", r.JSON["steps"])
	}
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := m["name"].(string); name == "stop_go" {
			if status, _ := m["status"].(string); status == "fail" {
				t.Fatalf("stop_go should pass with --override-ambiguous-state, got fail\nstdout: %s",
					r.RawStdout)
			}
		}
	}
}

// TestRollback_JSONSchema verifies the JSON output has expected fields.
func TestRollback_JSONSchema(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	r := runScript(t, "rollback", []string{})
	assertExitCode(t, r, 0)

	for _, key := range []string{"tool", "schema_version", "mode", "start_timestamp", "end_timestamp", "execute", "steps", "errors", "warnings", "exit_code"} {
		if _, ok := r.JSON[key]; !ok {
			t.Fatalf("JSON missing key %q\nstdout: %s", key, r.RawStdout)
		}
	}
}

// TestRollback_NoPasswordsPrinted verifies the JSON output never contains
// passwords.
func TestRollback_NoPasswordsPrinted(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	r := runScript(t, "rollback", []string{})
	assertExitCode(t, r, 0)

	lower := strings.ToLower(r.RawStdout)
	for _, forbidden := range []string{"password", "admin_password", "adminpassword"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("rollback JSON output contains password-related field: %q", forbidden)
		}
	}
}

// ---------------------------------------------------------------------------
// Staging rehearsal integration test
// ---------------------------------------------------------------------------

// TestRollback_StagingRehearsal simulates the full staging rehearsal flow:
//  1. Start a Go manager on a temp port with a temp data dir
//  2. Create a test instance (mutation)
//  3. Run a backup
//  4. Run rollback in dry-run mode (verify plan is correct)
//  5. Stop the Go manager
//  6. Run rollback with --execute using the current DB (compatible path)
//  7. Verify integrity check passes
//  8. Run a second rehearsal: corrupt the DB, then run rollback with --execute
//     using the backup (restore path)
//  9. Verify backup restore succeeds and integrity passes
//
// This is an integration test — it's OK if it takes longer.  It skips if the
// Go binary can't be built or sqlite3 is not available.
func TestRollback_StagingRehearsal(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh not available")
		}
	} else {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not available")
		}
	}

	// Build the Go manager binary.
	bin := managerBinary(t)

	// Create a valid data dir with instances and a fake GOWA binary.
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// 1. Start the Go manager on a free port.
	port := freePort(t)
	cmd := exec.Command(bin,
		"--port", fmt.Sprint(port),
		"--data-dir", dataDir,
		"--admin-username", "admin",
		"--admin-password", "password",
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Go manager: %v", err)
	}
	goPid := cmd.Process.Pid

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitForReady(t, baseURL, 15*time.Second) {
		_ = cmd.Process.Kill()
		t.Fatalf("Go manager did not become ready on %s", baseURL)
	}

	// 2. Create a test instance via the API (mutation).
	createBody := `{"name":"rehearsal-test","gowa_version":"` + fakeGOWAVersion + `"}`
	createReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/instances", strings.NewReader(createBody))
	createReq.SetBasicAuth("admin", "password")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("create instance: %v", err)
	}
	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		_ = cmd.Process.Kill()
		t.Fatalf("create instance: unexpected status %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	// 3. Run a backup (pre-cutover backup).
	backupDir := filepath.Join(filepath.Dir(dataDir), "rehearsal-backup")
	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	// 4. Run rollback in dry-run mode (verify plan is correct).
	dryRun := runScript(t, "rollback", []string{
		"--go-pid", fmt.Sprint(goPid),
		"--go-version", "test-v1.0.0",
		"--backup-dir", backupDir,
		"--data-dir", dataDir,
	})
	assertExitCode(t, dryRun, 0)
	if dryRun.JSON["mode"] != "dry_run" {
		t.Fatalf("dry-run mode = %v, want dry_run", dryRun.JSON["mode"])
	}

	// 5. Stop the Go manager.
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}

	// Build a fake Bun binary for the rollback.
	bunBinary := buildFakeBunForRollback(t)
	correctChecksum := computeFileSHA256(t, bunBinary)
	defer killFakeBun(t) // kill any fake-bun started by the rollback script

	// 6. Run rollback with --execute using the current DB (compatible path).
	//    The Go manager is stopped, so we use --override-ambiguous-state.
	//    The bun-url is unreachable so smoke will fail, but we only care
	//    about the integrity_check and db_strategy steps.
	execResult := runScript(t, "rollback", []string{
		"--execute",
		"--backup-dir", backupDir,
		"--go-pid", fmt.Sprint(goPid),
		"--go-version", "test-v1.0.0",
		"--bun-binary", bunBinary,
		"--bun-checksum", correctChecksum,
		"--data-dir", dataDir,
		"--bun-url", "http://127.0.0.1:1", // unreachable so smoke will fail
		"--override-ambiguous-state",
	})

	// 7. Verify integrity_check step passes and db_strategy uses current DB.
	steps, ok := execResult.JSON["steps"].([]any)
	if !ok {
		t.Fatalf("steps not an array: %v\nstdout: %s", execResult.JSON["steps"], execResult.RawStdout)
	}
	integrityPassed := false
	dbStrategyPassed := false
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		status, _ := m["status"].(string)
		if name == "integrity_check" && status == "pass" {
			integrityPassed = true
		}
		if name == "db_strategy" && status == "pass" {
			dbStrategyPassed = true
		}
	}
	if !integrityPassed {
		t.Fatalf("expected integrity_check step to pass (compatible path), steps: %v\nstdout: %s",
			steps, execResult.RawStdout)
	}
	if !dbStrategyPassed {
		t.Fatalf("expected db_strategy step to pass (compatible path), steps: %v\nstdout: %s",
			steps, execResult.RawStdout)
	}
	if execResult.JSON["db_strategy"] != "use_current" {
		t.Fatalf("db_strategy = %v, want use_current\nstdout: %s",
			execResult.JSON["db_strategy"], execResult.RawStdout)
	}

	// 8. Second rehearsal: corrupt the DB, then run rollback with --execute
	//    using the backup (restore path).
	dbPath := filepath.Join(dataDir, "gowa.db")
	garbage := []byte("SQLite format 3\x00" + strings.Repeat("\xff", 512))
	if err := os.WriteFile(dbPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}

	// Kill any fake-bun processes from the first run before the second.
	killFakeBun(t)

	restoreResult := runScript(t, "rollback", []string{
		"--execute",
		"--backup-dir", backupDir,
		"--go-pid", fmt.Sprint(goPid),
		"--go-version", "test-v1.0.0",
		"--bun-binary", bunBinary,
		"--bun-checksum", correctChecksum,
		"--data-dir", dataDir,
		"--bun-url", "http://127.0.0.1:1", // unreachable so smoke will fail
		"--override-ambiguous-state",
	})

	// 9. Verify backup restore succeeds and integrity passes.
	restoreSteps, ok := restoreResult.JSON["steps"].([]any)
	if !ok {
		t.Fatalf("restore steps not an array: %v\nstdout: %s",
			restoreResult.JSON["steps"], restoreResult.RawStdout)
	}
	restoreDbStrategyPassed := false
	for _, s := range restoreSteps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		status, _ := m["status"].(string)
		if name == "db_strategy" && status == "pass" {
			restoreDbStrategyPassed = true
		}
	}
	if !restoreDbStrategyPassed {
		t.Fatalf("expected db_strategy step to pass (restore path), steps: %v\nstdout: %s",
			restoreSteps, restoreResult.RawStdout)
	}
	if restoreResult.JSON["db_strategy"] != "restore_backup" {
		t.Fatalf("db_strategy = %v, want restore_backup\nstdout: %s",
			restoreResult.JSON["db_strategy"], restoreResult.RawStdout)
	}

	// Verify the DB was actually restored (integrity check passes on the
	// restored DB file).
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("restored DB file not found: %v", err)
	}
	// Use sqlite3 to verify the restored DB.
	sqlite3Path, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not available for post-restore verification")
	}
	integrityCmd := exec.Command(sqlite3Path, dbPath, "PRAGMA integrity_check;")
	integrityOut, err := integrityCmd.Output()
	if err != nil {
		t.Fatalf("sqlite3 integrity check on restored DB failed: %v\noutput: %s", err, integrityOut)
	}
	integrityStr := strings.TrimSpace(string(integrityOut))
	if integrityStr != "ok" {
		t.Fatalf("restored DB integrity check = %q, want ok", integrityStr)
	}
}
