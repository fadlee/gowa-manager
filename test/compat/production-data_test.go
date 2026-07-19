// Package compat contains integration tests that validate the Go backend
// against sanitized production data shapes.
//
// TestProductionData is the entry point. It is skipped automatically when the
// GOWA_COMPAT_SAMPLES environment variable is not set. When set, it expects a
// comma-separated list of paths to sanitized SQLite database files (produced
// by scripts/ops/sanitize-db.ts). For each sample it:
//
//  1. Copies the sample DB into a fresh temp data directory.
//  2. Runs preflight checks (integrity + required columns).
//  3. Starts the Go manager binary against the temp data dir.
//  4. Exercises major read endpoints (instances, system status, versions).
//  5. Exercises major write endpoints (create, update, delete an instance).
//  6. Stops the manager gracefully (SIGTERM) and verifies DB integrity.
//  7. Repeats with a forced stop (SIGKILL) and verifies DB integrity.
//  8. Tests migration idempotency by starting the manager 3 times and
//     comparing schema + row counts across restarts.
//
// No real production data is committed. The harness only provides the
// workflow; sanitized samples are supplied externally via the environment.
package compat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// compatEnvVar is the environment variable supplying comma-separated paths to
// sanitized SQLite database files.
const compatEnvVar = "GOWA_COMPAT_SAMPLES"

// requiredColumns lists every column the instances table must have, matching
// internal/database/schema.go.
var requiredColumns = []string{
	"id", "key", "name", "port", "status", "config",
	"gowa_version", "created_at", "updated_at", "error_message",
}

// manifestFile is the sample manifest describing what sanitized samples should
// contain. It is loaded but not strictly enforced — it documents the contract.
const manifestFile = "testdata/manifest.json"

// compatManifest is the decoded manifest. It is loaded once in TestProductionData.
type compatManifest struct {
	SchemaVersion     int                 `json:"schemaVersion"`
	Note              string              `json:"note"`
	InstancesTable    compatTableSpec     `json:"instancesTable"`
	ExpectedStatuses  []string            `json:"expectedStatuses"`
	ConfigFeatureFlags compatFeatureFlags `json:"configFeatureFlags"`
	VersionLayouts    compatVersionLayouts `json:"versionLayouts"`
	FilesystemCategories compatFS         `json:"filesystemCategories"`
	RowCountRanges    compatRowCount      `json:"rowCountRanges"`
	Samples           []compatSampleSpec  `json:"samples"`
}

type compatTableSpec struct {
	Name             string   `json:"name"`
	Columns          []string `json:"columns"`
	AdditiveColumns  []string `json:"additiveColumns"`
}

type compatFeatureFlags struct {
	Description string   `json:"description"`
	Flags       []string `json:"flags"`
}

type compatVersionLayouts struct {
	Description string   `json:"description"`
	Allowed     []string `json:"allowed"`
}

type compatFS struct {
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
}

type compatRowCount struct {
	Description string `json:"description"`
	Min         int    `json:"min"`
	Max         int    `json:"max"`
}

type compatSampleSpec struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	ExpectedRowCount compatRowCount `json:"expectedRowCount"`
	StatusesPresent []string        `json:"statusesPresent"`
	VersionsPresent []string        `json:"versionsPresent"`
}

// TestProductionData validates the Go backend against sanitized production
// data samples supplied via GOWA_COMPAT_SAMPLES. It is skipped when no samples
// are provided.
func TestProductionData(t *testing.T) {
	samplesEnv := os.Getenv(compatEnvVar)
	if samplesEnv == "" {
		t.Skipf("set %s to a comma-separated list of sanitized DB paths to enable production-data compatibility tests", compatEnvVar)
	}

	samples := splitAndTrim(samplesEnv)
	if len(samples) == 0 {
		t.Skipf("%s set but contained no paths", compatEnvVar)
	}

	// Load the manifest so the test can log the contract it validates against.
	manifest := loadCompatManifest(t)

	// Build the Go manager binary once for all samples.
	binary := buildGoManager(t)
	t.Cleanup(func() { _ = os.Remove(binary) })

	for _, samplePath := range samples {
		samplePath := samplePath
		t.Run(filepath.Base(samplePath), func(t *testing.T) {
			if _, err := os.Stat(samplePath); err != nil {
				t.Fatalf("sample DB not found at %q: %v", samplePath, err)
			}
			runCompatSample(t, binary, samplePath, manifest)
		})
	}
}

// runCompatSample exercises the full compatibility workflow for a single
// sanitized sample DB.
func runCompatSample(t *testing.T, binary, samplePath string, manifest *compatManifest) {
	t.Helper()

	// --- Step 1: Copy the sample DB into a fresh temp data dir. ---
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "gowa.db")
	if err := copyFile(samplePath, dbPath); err != nil {
		t.Fatalf("copy sample DB: %v", err)
	}
	t.Logf("copied %s -> %s", samplePath, dbPath)

	// Capture the initial schema and row count for idempotency comparison.
	initialSchema := dumpSchema(t, dbPath)
	initialRowCount := rowCount(t, dbPath)
	t.Logf("initial: %d rows, schema=%s", initialRowCount, initialSchema)

	// --- Step 2: Preflight — integrity + required columns. ---
	preflightCompat(t, dbPath)

	// --- Step 3: Start Go manager (graceful lifecycle). ---
	t.Run("GracefulLifecycle", func(t *testing.T) {
		dataDir := dataDir // capture for subtest clarity
		_ = dataDir
		proc := startGoManager(t, binary, dbPath)
		defer stopProcess(proc)

		baseURL := waitForManagerReady(t, proc)

		// Step 4: Exercise major reads.
		exerciseReads(t, baseURL)

		// Step 5: Exercise major writes.
		exerciseWrites(t, baseURL)

		// Step 6: Stop normally (SIGTERM).
		stopGracefully(t, proc)

		// Step 7: PRAGMA integrity_check after graceful stop.
		checkIntegrity(t, dbPath)
	})

	// --- Step 8: Repeat with forced stop (SIGKILL). ---
	t.Run("ForcedStop", func(t *testing.T) {
		// Use a fresh copy so the forced-stop test is independent of the
		// graceful lifecycle mutations.
		forcedDir := t.TempDir()
		forcedDB := filepath.Join(forcedDir, "gowa.db")
		if err := copyFile(samplePath, forcedDB); err != nil {
			t.Fatalf("copy sample DB for forced stop: %v", err)
		}
		proc := startGoManager(t, binary, forcedDB)
		defer stopProcess(proc)
		baseURL := waitForManagerReady(t, proc)
		exerciseReads(t, baseURL)

		// Force stop (SIGKILL / taskkill /F).
		stopForced(t, proc)

		// Step 9: PRAGMA integrity_check after forced stop.
		checkIntegrity(t, forcedDB)
	})

	// --- Step 10: Migration idempotency — start Go 3 times, verify schema +
	// data unchanged (no destructive or repeated mutation). ---
	t.Run("MigrationIdempotency", func(t *testing.T) {
		idemDir := t.TempDir()
		idemDB := filepath.Join(idemDir, "gowa.db")
		if err := copyFile(samplePath, idemDB); err != nil {
			t.Fatalf("copy sample DB for idempotency: %v", err)
		}

		// Snapshot schema + row count before any Go startup.
		beforeSchema := dumpSchema(t, idemDB)
		beforeRows := rowCount(t, idemDB)

		for i := 1; i <= 3; i++ {
			proc := startGoManager(t, binary, idemDB)
			baseURL := waitForManagerReady(t, proc)
			// A read to ensure the DB is touched.
			exerciseReads(t, baseURL)
			stopGracefully(t, proc)
			stopProcess(proc)

			afterSchema := dumpSchema(t, idemDB)
			afterRows := rowCount(t, idemDB)
			if afterSchema != beforeSchema {
				t.Fatalf("restart %d: schema changed by migration:\nbefore=%s\nafter=%s", i, beforeSchema, afterSchema)
			}
			if afterRows != beforeRows {
				t.Fatalf("restart %d: row count changed: before=%d after=%d (no destructive/repeated mutation expected on read-only exercise)", i, beforeRows, afterRows)
			}
			t.Logf("restart %d: schema and row count (%d) unchanged", i, afterRows)
		}
	})
}

// ---------------------------------------------------------------------------
// Preflight
// ---------------------------------------------------------------------------

// preflightCompat runs the same checks the ops preflight script performs:
// PRAGMA integrity_check and required-column presence.
func preflightCompat(t *testing.T, dbPath string) {
	t.Helper()
	db := openSQLDB(t, dbPath)
	defer db.Close()

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check query: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("preflight integrity_check = %q, want ok", integrity)
	}
	t.Logf("preflight: integrity_check = ok")

	cols, err := instanceColumnsCompat(db)
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}
	missing := []string{}
	for _, c := range requiredColumns {
		if !cols[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("preflight: missing required columns: %v", missing)
	}
	t.Logf("preflight: all %d required columns present", len(requiredColumns))
}

// ---------------------------------------------------------------------------
// HTTP exercise helpers
// ---------------------------------------------------------------------------

// exerciseReads hits the major read endpoints and verifies they return 200.
func exerciseReads(t *testing.T, baseURL string) {
	t.Helper()
	endpoints := []string{
		"/api/instances",
		"/api/system/status",
		"/api/system/versions/installed",
	}
	for _, ep := range endpoints {
		code, body := compatHTTPGet(t, baseURL, ep, true)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200; body: %s", ep, code, body)
		}
		t.Logf("read %s -> %d (ok)", ep, code)
	}
}

// exerciseWrites creates a test instance, updates it, then deletes it,
// verifying the major write paths work against the production-shaped DB.
func exerciseWrites(t *testing.T, baseURL string) {
	t.Helper()

	// Create.
	name := "compat-test-" + fmt.Sprint(time.Now().UnixNano())
	createBody := fmt.Sprintf(`{"name":%q}`, name)
	code, body := compatHTTPPost(t, baseURL, "/api/instances", createBody, true)
	if code != http.StatusCreated {
		t.Fatalf("create instance = %d, want 201; body: %s", code, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decode created instance: %v; body: %s", err, body)
	}
	t.Logf("write: created instance id=%d name=%s", created.ID, name)

	// Update.
	updateBody := fmt.Sprintf(`{"name":%q}`, name+"-updated")
	code, body = compatHTTPPut(t, baseURL, fmt.Sprintf("/api/instances/%d", created.ID), updateBody, true)
	if code != http.StatusOK {
		t.Fatalf("update instance %d = %d, want 200; body: %s", created.ID, code, body)
	}
	t.Logf("write: updated instance id=%d", created.ID)

	// Delete.
	code, body = compatHTTPDelete(t, baseURL, fmt.Sprintf("/api/instances/%d", created.ID), true)
	if code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("delete instance %d = %d, want 200/204; body: %s", created.ID, code, body)
	}
	t.Logf("write: deleted instance id=%d", created.ID)
}

// ---------------------------------------------------------------------------
// Go manager process lifecycle
// ---------------------------------------------------------------------------

// startGoManager starts the Go manager binary with the given data dir and a
// random free port. It returns the running process. On Windows the process is
// created in a new process group so that GenerateConsoleCtrlEvent can deliver
// a Ctrl+Break (mapped to os.Interrupt by the Go runtime) for graceful
// shutdown.
func startGoManager(t *testing.T, binary, dbPath string) *exec.Cmd {
	t.Helper()
	dataDir := filepath.Dir(dbPath)
	port := freePort(t)

	cmd := exec.Command(binary,
		"--port", fmt.Sprint(port),
		"--host", "127.0.0.1",
		"--data-dir", dataDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	setNewProcessGroup(cmd) // platform-specific SysProcAttr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start go manager: %v", err)
	}
	return cmd
}

// waitForManagerReady polls /api/ready until 200 or timeout. It returns the
// base URL.
func waitForManagerReady(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	// We cannot easily read the bound port back from the binary, so we rely on
	// the port we passed via --port. The binary is started with a known free
	// port above.
	port := extractPortFromCmd(cmd)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// Check the process is still alive.
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("go manager exited early with code %d", cmd.ProcessState.ExitCode())
		}
		resp, err := client.Get(baseURL + "/api/ready")
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return baseURL
			}
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("go manager did not become ready on %s within 30s", baseURL)
	return baseURL
}

// extractPortFromCmd retrieves the --port flag value from the command args.
func extractPortFromCmd(cmd *exec.Cmd) int {
	args := cmd.Args
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			var p int
			fmt.Sscanf(args[i+1], "%d", &p)
			return p
		}
	}
	return 3000
}

// stopGracefully sends a graceful-shutdown signal and waits for the process
// to exit within a timeout. On Unix this is SIGTERM; on Windows it uses
// GenerateConsoleCtrlEvent (Ctrl+Break) which the Go runtime maps to
// os.Interrupt.
func stopGracefully(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		sendWindowsCtrlBreak(t, cmd.Process.Pid)
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	waitWithTimeout(t, cmd, 15*time.Second)
}

// stopForced sends SIGKILL (or taskkill /F on Windows) and waits.
func stopForced(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
	} else {
		_ = cmd.Process.Kill()
	}
	waitWithTimeout(t, cmd, 10*time.Second)
}

// stopProcess is a best-effort cleanup that ensures no lingering process is
// left behind. It is safe to call after stopGracefully/stopForced.
func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

// waitWithTimeout waits for the process to exit within the timeout, failing
// the test if it does not.
func waitWithTimeout(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil && cmd.ProcessState != nil && !cmd.ProcessState.Success() {
			// A non-zero exit from a signal is expected on forced stop; only
			// log, do not fail.
			t.Logf("go manager exited: %v (code=%d)", err, cmd.ProcessState.ExitCode())
		}
	case <-time.After(timeout):
		t.Fatalf("go manager did not exit within %v", timeout)
	}
}

// ---------------------------------------------------------------------------
// DB helpers
// ---------------------------------------------------------------------------

// checkIntegrity runs PRAGMA integrity_check on the DB file.
func checkIntegrity(t *testing.T, dbPath string) {
	t.Helper()
	db := openSQLDB(t, dbPath)
	defer db.Close()
	var result string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if result != "ok" {
		t.Fatalf("integrity_check = %q, want ok", result)
	}
	t.Logf("integrity_check = ok for %s", dbPath)
}

// dumpSchema returns a canonical string representation of the instances table
// schema (sorted column list) for idempotency comparison.
func dumpSchema(t *testing.T, dbPath string) string {
	t.Helper()
	db := openSQLDB(t, dbPath)
	defer db.Close()
	cols, err := instanceColumnsCompat(db)
	if err != nil {
		t.Fatalf("dump schema: %v", err)
	}
	names := make([]string, 0, len(cols))
	for c := range cols {
		names = append(names, c)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// rowCount returns the number of rows in the instances table.
func rowCount(t *testing.T, dbPath string) int {
	t.Helper()
	db := openSQLDB(t, dbPath)
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&n); err != nil {
		t.Fatalf("row count: %v", err)
	}
	return n
}

// instanceColumnsCompat returns a set of column names for the instances table.
func instanceColumnsCompat(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(instances)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// openSQLDB opens the SQLite file read-only-ish via the modernc driver.
func openSQLDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	dsn := dbPath + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite %q: %v", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Fatalf("ping sqlite %q: %v", dbPath, err)
	}
	return db
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// compatHTTPGet performs an (optionally authenticated) GET and returns the
// status code and body.
func compatHTTPGet(t *testing.T, baseURL, path string, auth bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatalf("new get request %s: %v", path, err)
	}
	if auth {
		req.SetBasicAuth("admin", "password")
	}
	return compatDo(t, req)
}

// compatHTTPPost performs an authenticated POST with the given JSON body.
func compatHTTPPost(t *testing.T, baseURL, path, body string, auth bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new post request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.SetBasicAuth("admin", "password")
	}
	return compatDo(t, req)
}

// compatHTTPPut performs an authenticated PUT with the given JSON body.
func compatHTTPPut(t *testing.T, baseURL, path, body string, auth bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new put request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.SetBasicAuth("admin", "password")
	}
	return compatDo(t, req)
}

// compatHTTPDelete performs an authenticated DELETE.
func compatHTTPDelete(t *testing.T, baseURL, path string, auth bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, baseURL+path, nil)
	if err != nil {
		t.Fatalf("new delete request %s: %v", path, err)
	}
	if auth {
		req.SetBasicAuth("admin", "password")
	}
	return compatDo(t, req)
}

func compatDo(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("http %s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", req.URL.Path, err)
	}
	return resp.StatusCode, string(body)
}

// ---------------------------------------------------------------------------
// Build + utility helpers
// ---------------------------------------------------------------------------

// buildGoManager builds the Go manager binary into a temp file and returns its
// path. The caller is responsible for removing it.
func buildGoManager(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "gowa-manager-go")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/gowa-manager-go")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build manager: %v", err)
	}
	t.Logf("built go manager: %s", out)
	return out
}

// freePort returns a free TCP port on 127.0.0.1. It does not hold the port
// open, so there is a small race window; acceptable for tests.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr()
	type portAddr interface{ Port() int }
	if a, ok := addr.(portAddr); ok {
		return a.Port()
	}
	// Fallback parse from string.
	s := addr.String()
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		var p int
		fmt.Sscanf(s[idx+1:], "%d", &p)
		return p
	}
	t.Fatalf("could not determine free port from %v", addr)
	return 0
}

// copyFile copies src to dst, creating dst's parent directory if needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
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
	_, err = io.Copy(out, in)
	return err
}

// splitAndTrim splits a comma-separated string and trims whitespace from each
// element, dropping empties.
func splitAndTrim(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// loadCompatManifest reads and decodes the testdata manifest. It is non-fatal
// if the manifest is missing (the harness still runs); a parse error is fatal.
func loadCompatManifest(t *testing.T) *compatManifest {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	path := filepath.Join(wd, manifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("manifest not found at %s (continuing without it): %v", path, err)
		return &compatManifest{}
	}
	var m compatManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode manifest %s: %v", path, err)
	}
	t.Logf("loaded manifest: schemaVersion=%d, %d sample specs, columns=%v", m.SchemaVersion, len(m.Samples), m.InstancesTable.Columns)
	return &m
}
