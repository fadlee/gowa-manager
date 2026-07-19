package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/gofrs/flock"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

const fakeGOWAVersion = "v1.0.0-test"

// findRepoRoot walks up from the test working directory to find go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root containing go.mod")
		}
		dir = parent
	}
}

// managerBinary returns the path to the pre-built Go manager binary.
// Tests skip if the binary does not exist.
func managerBinary(t *testing.T) string {
	t.Helper()
	root := findRepoRoot(t)
	name := "gowa-manager-go"
	if runtime.GOOS == "windows" {
		name = "gowa-manager-go.exe"
	}
	path := filepath.Join(root, "dist-go", name)
	if _, err := os.Stat(path); err != nil {
		// Fall back to building it.
		path = buildManager(t, root)
	}
	return path
}

func buildManager(t *testing.T, root string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gowa-ops-mgr-")
	if err != nil {
		t.Fatal(err)
	}
	name := "gowa-manager-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", path, "./cmd/gowa-manager-go")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build manager: %v\n%s", err, out)
	}
	return path
}

// sqlite3Available reports whether the sqlite3 CLI is on PATH.
func sqlite3Available() bool {
	_, err := exec.LookPath("sqlite3")
	return err == nil
}

// skipIfNoSqlite3 skips the test when the sqlite3 CLI is not installed.
func skipIfNoSqlite3(t *testing.T) {
	t.Helper()
	if !sqlite3Available() {
		t.Skip("sqlite3 CLI not available — required by preflight/backup scripts")
	}
}

// scriptResult holds the parsed JSON and exit code from a script invocation.
type scriptResult struct {
	JSON     map[string]any
	ExitCode int
	RawStdout string
}

// runScript runs the platform-appropriate script variant and returns its
// JSON output and exit code.  On Windows the .ps1 variant is used; on
// Unix the .sh variant is used.  If both interpreters are available the
// native variant is preferred.
func runScript(t *testing.T, scriptName string, args []string) scriptResult {
	t.Helper()
	root := findRepoRoot(t)
	opsDir := filepath.Join(root, "scripts", "ops")

	if runtime.GOOS == "windows" {
		return runPowerShell(t, filepath.Join(opsDir, scriptName+".ps1"), args)
	}
	return runSh(t, filepath.Join(opsDir, scriptName+".sh"), args)
}

func runPowerShell(t *testing.T, script string, args []string) scriptResult {
	t.Helper()
	// Build -File argument list: script path followed by named params.
	// The callers pass already-formatted args like ["-Binary", path, ...].
	cmdArgs := []string{"-NoProfile", "-NonInteractive", "-File", script}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("pwsh", cmdArgs...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("invoke pwsh %s: %v\nstderr: %s", script, err, stderr.String())
		}
	}
	return parseScriptResult(t, stdout.String(), exitCode)
}

func runSh(t *testing.T, script string, args []string) scriptResult {
	t.Helper()
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("invoke sh %s: %v\nstderr: %s", script, err, stderr.String())
		}
	}
	return parseScriptResult(t, stdout.String(), exitCode)
}

func parseScriptResult(t *testing.T, stdout string, exitCode int) scriptResult {
	t.Helper()
	var j map[string]any
	if err := json.Unmarshal([]byte(stdout), &j); err != nil {
		t.Fatalf("parse script JSON: %v\nstdout: %s", err, stdout)
	}
	return scriptResult{JSON: j, ExitCode: exitCode, RawStdout: stdout}
}

// createValidDataDir creates a data directory with a valid SQLite database
// containing the instances table and optional running instances.
func createValidDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gowa-ops-preflight-")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	// Insert a running and a stopped instance.
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO instances (key, name, port, status, gowa_version) VALUES (?, ?, ?, ?, ?)`,
		"inst1", "Instance 1", 51234, "running", fakeGOWAVersion)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO instances (key, name, port, status) VALUES (?, ?, ?, ?)`,
		"inst2", "Instance 2", 51235, "stopped")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Install a fake GOWA binary.
	installFakeGOWA(t, dataDir, fakeGOWAVersion)
	return dataDir
}

// installFakeGOWA copies a small executable into the data dir's
// bin/versions/<version>/ directory so preflight can verify it.
func installFakeGOWA(t *testing.T, dataDir, version string) {
	t.Helper()
	binaryName := "gowa"
	if runtime.GOOS == "windows" {
		binaryName = "gowa.exe"
	}
	versionDir := filepath.Join(dataDir, "bin", "versions", version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(versionDir, binaryName)
	// Build a tiny Go program that exits 0.
	src := []byte("package main\n\nimport \"os\"\n\nfunc main() { os.Exit(0) }\n")
	tmpDir, err := os.MkdirTemp("", "gowa-fake-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	srcFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcFile, src, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", dst, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake gowa: %v\n%s", err, out)
	}
	if runtime.GOOS != "windows" {
		os.Chmod(dst, 0o755)
	}
}

// createCorruptDB creates a data directory with a corrupt SQLite database.
func createCorruptDB(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gowa-ops-corrupt-")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "gowa.db")
	// Write garbage that looks like a SQLite header but is corrupt.
	garbage := []byte("SQLite format 3\x00" + strings.Repeat("\xff", 512))
	if err := os.WriteFile(dbPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

// holdManagerLock acquires the flock on the data dir and returns a release
// function.  The lock is held until release is called.
func holdManagerLock(t *testing.T, dataDir string) func() {
	t.Helper()
	lockPath := filepath.Join(dataDir, ".gowa-manager.lock")
	f := flock.New(lockPath)
	locked, err := f.TryLock()
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	if !locked {
		t.Fatal("could not acquire lock — already held")
	}
	return func() { _ = f.Unlock() }
}

// freePort returns a TCP port that is currently free.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// occupyPort listens on the given port until the returned function is called.
func occupyPort(t *testing.T, port int) func() {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("occupy port %d: %v", port, err)
	}
	return func() { ln.Close() }
}

// assertExitCode checks that the script result has the expected exit code.
func assertExitCode(t *testing.T, r scriptResult, want int) {
	t.Helper()
	if r.ExitCode != want {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", r.ExitCode, want, r.RawStdout)
	}
}

// assertBlockerContains checks that at least one blocker contains substr.
func assertBlockerContains(t *testing.T, r scriptResult, substr string) {
	t.Helper()
	blockers, ok := r.JSON["blockers"].([]any)
	if !ok || len(blockers) == 0 {
		t.Fatalf("expected blockers containing %q, got none\nstdout: %s", substr, r.RawStdout)
	}
	for _, b := range blockers {
		if s, ok := b.(string); ok && strings.Contains(s, substr) {
			return
		}
	}
	t.Fatalf("no blocker contains %q; blockers: %v", substr, blockers)
}

// checkField retrieves a nested field from the JSON result.
func checkField(t *testing.T, j map[string]any, key string) any {
	t.Helper()
	v, ok := j[key]
	if !ok {
		t.Fatalf("JSON missing key %q\nstdout: %s", key, j)
	}
	return v
}

// ---------------------------------------------------------------------------
// Preflight tests
// ---------------------------------------------------------------------------

func TestPreflight_Success(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	// The manager_active check may detect system processes, so we only
	// assert that there are no blockers from the checks we control.
	// If manager_active fires, exit code will be 1 — filter it out.
	blockers, _ := r.JSON["blockers"].([]any)
	nonManagerBlockers := []any{}
	for _, b := range blockers {
		if s, ok := b.(string); ok && !strings.Contains(s, "manager process") {
			nonManagerBlockers = append(nonManagerBlockers, s)
		}
	}
	if len(nonManagerBlockers) > 0 {
		t.Fatalf("unexpected blockers: %v\nstdout: %s", nonManagerBlockers, r.RawStdout)
	}

	// Verify key JSON fields.
	if checkField(t, r.JSON, "tool") != "preflight" {
		t.Fatal("tool field mismatch")
	}
	sqliteInfo := checkField(t, r.JSON, "sqlite").(map[string]any)
	if sqliteInfo["integrity"] != "ok" {
		t.Fatalf("sqlite integrity = %v, want ok", sqliteInfo["integrity"])
	}
	colInfo := checkField(t, r.JSON, "columns").(map[string]any)
	missing, _ := colInfo["missing"].([]any)
	if len(missing) != 0 {
		t.Fatalf("expected no missing columns, got %v", missing)
	}
	portInfo := checkField(t, r.JSON, "port").(map[string]any)
	if portInfo["available"] != true {
		t.Fatalf("expected port available, got %v", portInfo["available"])
	}
}

func TestPreflight_CorruptDB(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createCorruptDB(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "integrity")
	sqliteInfo := checkField(t, r.JSON, "sqlite").(map[string]any)
	if sqliteInfo["integrity"] == "ok" {
		t.Fatal("expected integrity != ok for corrupt DB")
	}
}

func TestPreflight_LockHeld(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	release := holdManagerLock(t, dataDir)
	defer release()

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "lock")
	lockInfo := checkField(t, r.JSON, "lock").(map[string]any)
	if lockInfo["held"] != true {
		t.Fatalf("expected lock held=true, got %v", lockInfo["held"])
	}
}

func TestPreflight_PortOccupied(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	port := freePort(t)
	release := occupyPort(t, port)
	defer release()

	bin := managerBinary(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "port")
	portInfo := checkField(t, r.JSON, "port").(map[string]any)
	if portInfo["available"] != false {
		t.Fatalf("expected port available=false, got %v", portInfo["available"])
	}
}

func TestPreflight_MissingBinary(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")
	missingBin := filepath.Join(filepath.Dir(dataDir), "nonexistent-binary")

	r := runScript(t, "preflight", []string{
		"-Binary", missingBin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "binary")
	binInfo := checkField(t, r.JSON, "binary").(map[string]any)
	if binInfo["exists"] != false {
		t.Fatalf("expected binary exists=false, got %v", binInfo["exists"])
	}
}

func TestPreflight_NoBinaryPath(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	// Run without -Binary — should report "no binary path provided".
	r := runScript(t, "preflight", []string{
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "binary")
}

func TestPreflight_PermissionFailure(t *testing.T) {
	skipIfNoSqlite3(t)
	// Permission tests are unreliable on Windows (no Unix permission model
	// for regular users) and when tests run as root (e.g. in CI containers).
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission test not meaningful on Windows or as root")
	}

	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// Make data dir read-only.
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dataDir, 0o755)

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	permInfo := checkField(t, r.JSON, "permissions").(map[string]any)
	if permInfo["write"] == true {
		t.Fatalf("expected write=false for read-only dir, got %v", permInfo["write"])
	}
}

func TestPreflight_MissingDataDir(t *testing.T) {
	skipIfNoSqlite3(t)
	dir, err := os.MkdirTemp("", "gowa-ops-nodir-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	missingDataDir := filepath.Join(dir, "does-not-exist")
	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(dir, "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", missingDataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "data")
	ddInfo := checkField(t, r.JSON, "data_dir").(map[string]any)
	if ddInfo["exists"] != false {
		t.Fatalf("expected data_dir exists=false, got %v", ddInfo["exists"])
	}
}

func TestPreflight_PathsWithSpaces(t *testing.T) {
	skipIfNoSqlite3(t)
	// Create a data directory whose path contains spaces.
	base, err := os.MkdirTemp("", "gowa ops spaces-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)

	dataDir := filepath.Join(base, "my data dir")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx,
		`INSERT INTO instances (key, name, port, status) VALUES (?, ?, ?, ?)`,
		"spaced", "Spaced Instance", 51236, "stopped"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	installFakeGOWA(t, dataDir, fakeGOWAVersion)

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(base, "backup dir")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	// Filter out manager_active blockers (system processes).
	blockers, _ := r.JSON["blockers"].([]any)
	nonManagerBlockers := []any{}
	for _, b := range blockers {
		if s, ok := b.(string); ok && !strings.Contains(s, "manager process") {
			nonManagerBlockers = append(nonManagerBlockers, s)
		}
	}
	if len(nonManagerBlockers) > 0 {
		t.Fatalf("unexpected blockers with spaced paths: %v\nstdout: %s",
			nonManagerBlockers, r.RawStdout)
	}

	// Verify the script handled the spaces correctly.
	sqliteInfo := checkField(t, r.JSON, "sqlite").(map[string]any)
	if sqliteInfo["integrity"] != "ok" {
		t.Fatalf("sqlite integrity = %v, want ok (spaced path)", sqliteInfo["integrity"])
	}
	binInfo := checkField(t, r.JSON, "binary").(map[string]any)
	if binInfo["exists"] != true {
		t.Fatalf("expected binary exists=true with spaced path, got %v", binInfo["exists"])
	}
}

func TestPreflight_ChildProcessInventory(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	children, ok := r.JSON["child_processes"].([]any)
	if !ok {
		t.Fatalf("child_processes not an array: %v", r.JSON["child_processes"])
	}
	// We inserted one running instance ("inst1").
	found := false
	for _, c := range children {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["key"] == "inst1" && m["status"] == "running" {
			found = true
			// Verify config is NOT included.
			if _, hasConfig := m["config"]; hasConfig {
				t.Fatal("child_processes entry must not include config field")
			}
		}
	}
	if !found {
		t.Fatalf("running instance 'inst1' not found in child_processes: %v", children)
	}
}

func TestPreflight_GOWABinariesVerified(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	gowaBins, ok := r.JSON["gowa_binaries"].([]any)
	if !ok {
		t.Fatalf("gowa_binaries not an array: %v", r.JSON["gowa_binaries"])
	}
	if len(gowaBins) == 0 {
		t.Fatal("expected at least one GOWA binary entry")
	}
	entry := gowaBins[0].(map[string]any)
	if entry["version"] != fakeGOWAVersion {
		t.Fatalf("GOWA binary version = %v, want %s", entry["version"], fakeGOWAVersion)
	}
	if entry["exists"] != true {
		t.Fatalf("GOWA binary exists = %v, want true", entry["exists"])
	}
	if entry["executable"] != true {
		t.Fatalf("GOWA binary executable = %v, want true", entry["executable"])
	}
}

func TestPreflight_GOWABinaryMissing(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// Remove the GOWA binary to simulate a missing binary.
	versionDir := filepath.Join(dataDir, "bin", "versions", fakeGOWAVersion)
	binaryName := "gowa"
	if runtime.GOOS == "windows" {
		binaryName = "gowa.exe"
	}
	os.Remove(filepath.Join(versionDir, binaryName))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	assertBlockerContains(t, r, "GOWA binary missing")
}

func TestPreflight_JSONNoSecrets(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// Insert an instance with a config containing a fake token.
	ctx := context.Background()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO instances (key, name, port, status, config) VALUES (?, ?, ?, ?, ?)`,
		"secret-inst", "Secret Instance", 51237, "stopped",
		`{"token":"super-secret-token-12345","webhook":"https://example.com/webhook"}`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	// The raw JSON output must never contain the secret token or webhook URL.
	if strings.Contains(r.RawStdout, "super-secret-token-12345") {
		t.Fatal("preflight JSON output contains secret token")
	}
	if strings.Contains(r.RawStdout, "https://example.com/webhook") {
		t.Fatal("preflight JSON output contains webhook URL")
	}
	// Verify child_processes does not include config.
	children, _ := r.JSON["child_processes"].([]any)
	for _, c := range children {
		if m, ok := c.(map[string]any); ok {
			if _, hasConfig := m["config"]; hasConfig {
				t.Fatal("child_processes entry must not include config field")
			}
		}
	}
}

func TestPreflight_NoPasswordsPrinted(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	// The preflight script does not accept password args, but verify the
	// JSON output never contains common password field names.
	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})

	lower := strings.ToLower(r.RawStdout)
	for _, forbidden := range []string{"password", "admin_password", "adminpassword"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("preflight JSON output contains password-related field: %q", forbidden)
		}
	}
}

// Ensure the script completes quickly to avoid hanging tests.
func TestPreflight_CompletesQuickly(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	bin := managerBinary(t)
	port := freePort(t)
	backupDir := filepath.Join(filepath.Dir(dataDir), "backup")

	start := time.Now()
	r := runScript(t, "preflight", []string{
		"-Binary", bin,
		"-DataDir", dataDir,
		"-Port", fmt.Sprint(port),
		"-BackupDir", backupDir,
	})
	elapsed := time.Since(start)
	if elapsed > 30*time.Second {
		t.Fatalf("preflight took %v, expected < 30s", elapsed)
	}
	// Just verify it produced valid JSON.
	if r.JSON["tool"] != "preflight" {
		t.Fatal("tool field mismatch")
	}
}
