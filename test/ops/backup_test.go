package ops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fadlee/gowa-manager/internal/database"
)

// ---------------------------------------------------------------------------
// Backup tests
// ---------------------------------------------------------------------------

func TestBackup_Success(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-out")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 0)
	if r.JSON["tool"] != "backup" {
		t.Fatalf("tool = %v, want backup", r.JSON["tool"])
	}

	// Manifest must be verified.
	manifest, ok := r.JSON["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("manifest not an object: %v", r.JSON["manifest"])
	}
	if manifest["verified"] != true {
		t.Fatalf("manifest verified = %v, want true", manifest["verified"])
	}
	fileCount, ok := manifest["file_count"].(float64)
	if !ok || fileCount < 3 {
		t.Fatalf("manifest file_count = %v, expected >= 3 (db, instances, versions, manifest)", manifest["file_count"])
	}

	// Method should be set (online_backup for WAL or file_copy).
	method := r.JSON["method"]
	if method != "online_backup" && method != "file_copy" {
		t.Fatalf("method = %v, want online_backup or file_copy", method)
	}

	// Verify backup files exist on disk.
	dbBackup := filepath.Join(backupDir, "gowa.db")
	if _, err := os.Stat(dbBackup); err != nil {
		t.Fatalf("backup database not created: %v", err)
	}
	manifestPath := filepath.Join(backupDir, "manifest.sha256")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest not created: %v", err)
	}
	instancesMeta := filepath.Join(backupDir, "instances.json")
	if _, err := os.Stat(instancesMeta); err != nil {
		t.Fatalf("instances metadata not created: %v", err)
	}
	versionsMeta := filepath.Join(backupDir, "versions.json")
	if _, err := os.Stat(versionsMeta); err != nil {
		t.Fatalf("versions metadata not created: %v", err)
	}
}

func TestBackup_ManifestContent(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-manifest")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	// Read the manifest and verify it lists all expected files.
	manifestPath := filepath.Join(backupDir, "manifest.sha256")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestStr := string(data)
	for _, expected := range []string{"gowa.db", "instances.json", "versions.json"} {
		if !strings.Contains(manifestStr, expected) {
			t.Fatalf("manifest missing entry for %q\nmanifest:\n%s", expected, manifestStr)
		}
	}
}

func TestBackup_InstancesMetadataExcludesConfig(t *testing.T) {
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

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-secret")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	// The instances.json must NOT contain the secret token or webhook URL.
	instancesMeta := filepath.Join(backupDir, "instances.json")
	data, err := os.ReadFile(instancesMeta)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "super-secret-token-12345") {
		t.Fatal("instances.json contains secret token")
	}
	if strings.Contains(content, "https://example.com/webhook") {
		t.Fatal("instances.json contains webhook URL")
	}
	// It should contain the instance key and name.
	if !strings.Contains(content, "secret-inst") {
		t.Fatal("instances.json missing instance key")
	}
	if !strings.Contains(content, "Secret Instance") {
		t.Fatal("instances.json missing instance name")
	}

	// Also verify the JSON output from the script doesn't contain secrets.
	if strings.Contains(r.RawStdout, "super-secret-token-12345") {
		t.Fatal("backup JSON output contains secret token")
	}
}

func TestBackup_MissingDataDir(t *testing.T) {
	skipIfNoSqlite3(t)
	dir, err := os.MkdirTemp("", "gowa-ops-backup-nodir-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	missingDataDir := filepath.Join(dir, "does-not-exist")
	backupDir := filepath.Join(dir, "backup-out")

	r := runScript(t, "backup", []string{
		"-DataDir", missingDataDir,
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	errs, ok := r.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors, got %v", r.JSON["errors"])
	}
}

func TestBackup_MissingDB(t *testing.T) {
	skipIfNoSqlite3(t)
	dir, err := os.MkdirTemp("", "gowa-ops-backup-nodb-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create a data dir with no database file.
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(dir, "backup-out")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 1)
	errs, ok := r.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors for missing DB, got %v", r.JSON["errors"])
	}
	found := false
	for _, e := range errs {
		if s, ok := e.(string); ok && strings.Contains(s, "database") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about missing database, got %v", errs)
	}
}

func TestBackup_PathsWithSpaces(t *testing.T) {
	skipIfNoSqlite3(t)
	base, err := os.MkdirTemp("", "gowa ops backup spaces-")
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
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO instances (key, name, port, status) VALUES (?, ?, ?, ?)`,
		"spaced", "Spaced Instance", 51238, "stopped")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	installFakeGOWA(t, dataDir, fakeGOWAVersion)

	backupDir := filepath.Join(base, "backup output dir")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})

	assertExitCode(t, r, 0)
	manifest, ok := r.JSON["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("manifest not an object: %v", r.JSON["manifest"])
	}
	if manifest["verified"] != true {
		t.Fatalf("manifest verified = %v, want true (spaced path)", manifest["verified"])
	}
	// Verify backup files exist in the spaced backup dir.
	dbBackup := filepath.Join(backupDir, "gowa.db")
	if _, err := os.Stat(dbBackup); err != nil {
		t.Fatalf("backup database not created in spaced path: %v", err)
	}
}

func TestBackup_MetadataCounts(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-counts")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	meta, ok := r.JSON["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata not an object: %v", r.JSON["metadata"])
	}
	// createValidDataDir inserts 2 instances.
	instances, ok := meta["instances_copied"].(float64)
	if !ok {
		t.Fatalf("instances_copied not a number: %v", meta["instances_copied"])
	}
	if instances != 2 {
		t.Fatalf("instances_copied = %v, want 2", instances)
	}
	// createValidDataDir installs 1 GOWA version.
	versions, ok := meta["versions_copied"].(float64)
	if !ok {
		t.Fatalf("versions_copied not a number: %v", meta["versions_copied"])
	}
	if versions != 1 {
		t.Fatalf("versions_copied = %v, want 1", versions)
	}
}

func TestBackup_TimestampsRecorded(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-ts")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	startTs, ok := r.JSON["start_timestamp"].(string)
	if !ok || startTs == "" {
		t.Fatalf("start_timestamp missing or empty: %v", r.JSON["start_timestamp"])
	}
	endTs, ok := r.JSON["end_timestamp"].(string)
	if !ok || endTs == "" {
		t.Fatalf("end_timestamp missing or empty: %v", r.JSON["end_timestamp"])
	}
	if startTs > endTs {
		t.Fatalf("start_timestamp %q > end_timestamp %q", startTs, endTs)
	}
}

func TestBackup_ManagerDowntimeState(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-downtime")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	downtime, ok := r.JSON["manager_downtime"].(map[string]any)
	if !ok {
		t.Fatalf("manager_downtime not an object: %v", r.JSON["manager_downtime"])
	}
	state, ok := downtime["state"].(string)
	if !ok || state == "" {
		t.Fatalf("manager_downtime.state missing: %v", downtime["state"])
	}
	note, ok := downtime["note"].(string)
	if !ok || note == "" {
		t.Fatalf("manager_downtime.note missing: %v", downtime["note"])
	}
}

func TestBackup_NonWalFileCopy(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// Set journal mode to DELETE (non-WAL) to exercise the file_copy path.
	// createValidDataDir already closed the DB connection.
	ctx := context.Background()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQL.ExecContext(ctx, `PRAGMA journal_mode=DELETE`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-delete")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	if r.JSON["method"] != "file_copy" {
		t.Fatalf("method = %v, want file_copy for non-WAL journal", r.JSON["method"])
	}
	if r.JSON["journal_mode"] != "delete" {
		t.Fatalf("journal_mode = %v, want delete", r.JSON["journal_mode"])
	}
}

func TestBackup_WalOnlineBackup(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	// Ensure WAL mode (createValidDataDir uses database.Open which defaults
	// to whatever the driver sets; explicitly set WAL).
	ctx := context.Background()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQL.ExecContext(ctx, `PRAGMA journal_mode=WAL`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-wal")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	if r.JSON["method"] != "online_backup" {
		t.Fatalf("method = %v, want online_backup for WAL journal", r.JSON["method"])
	}
}

func TestBackup_FilesListContainsSHA256(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-sha")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	files, ok := r.JSON["files"].([]any)
	if !ok {
		t.Fatalf("files not an array: %v", r.JSON["files"])
	}
	if len(files) < 3 {
		t.Fatalf("expected >= 3 files, got %d", len(files))
	}
	for _, f := range files {
		m, ok := f.(map[string]any)
		if !ok {
			t.Fatalf("file entry not an object: %v", f)
		}
		sha, ok := m["sha256"].(string)
		if !ok || len(sha) != 64 {
			t.Fatalf("file sha256 invalid: %v (len %d)", sha, len(sha))
		}
		path, ok := m["path"].(string)
		if !ok || path == "" {
			t.Fatalf("file path missing: %v", m["path"])
		}
		size, ok := m["size"].(float64)
		if !ok || size < 0 {
			t.Fatalf("file size invalid: %v", m["size"])
		}
	}
}

func TestBackup_VersionsMetadataContent(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-vermeta")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	versionsMeta := filepath.Join(backupDir, "versions.json")
	data, err := os.ReadFile(versionsMeta)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, fakeGOWAVersion) {
		t.Fatalf("versions.json missing version %q\ncontent: %s", fakeGOWAVersion, content)
	}
}

func TestBackup_NoAtomicityClaim(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-atomic")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	// The script must not claim atomicity across files.  The note in
	// manager_downtime should mention that the script does not stop the
	// manager, and there should be no "atomic" claim in the output.
	lower := strings.ToLower(r.RawStdout)
	if strings.Contains(lower, "atomic backup") || strings.Contains(lower, "atomic across") {
		t.Fatal("backup output claims atomicity across files")
	}
}

// Ensure backup works on Windows with .exe GOWA binaries.
func TestBackup_WindowsExeBinary(t *testing.T) {
	skipIfNoSqlite3(t)
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-win")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	// Verify versions.json lists the .exe binary.
	versionsMeta := filepath.Join(backupDir, "versions.json")
	data, err := os.ReadFile(versionsMeta)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "gowa.exe") {
		t.Fatalf("versions.json should reference gowa.exe on Windows\ncontent: %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Checksum mismatch test (Step 4 failure case)
// ---------------------------------------------------------------------------

func TestBackup_ChecksumMismatch(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-verify")

	// 1. Run a normal backup — should succeed with a verified manifest.
	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)
	manifest, _ := r.JSON["manifest"].(map[string]any)
	if manifest["verified"] != true {
		t.Fatalf("initial backup manifest not verified: %v\nstdout: %s",
			manifest["verified"], r.RawStdout)
	}

	// 2. Corrupt one of the backed-up files (the database copy).
	dbBackup := filepath.Join(backupDir, "gowa.db")
	corrupt := []byte("CORRUPTED-DATA-FOR-VERIFY-TEST-1234567890")
	if err := os.WriteFile(dbBackup, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Run backup with --verify — should detect the checksum mismatch and
	//    exit non-zero.
	rv := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
		"-Verify",
	})
	assertExitCode(t, rv, 1)
	// The manifest.verified field must be false.
	vm, ok := rv.JSON["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("manifest not an object in verify output: %v\nstdout: %s",
			rv.JSON["manifest"], rv.RawStdout)
	}
	if vm["verified"] != false {
		t.Fatalf("verify manifest.verified = %v, want false", vm["verified"])
	}
	// An error about checksum mismatch must be present.
	errs, ok := rv.JSON["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors in verify output, got none\nstdout: %s", rv.RawStdout)
	}
	found := false
	for _, e := range errs {
		if s, ok := e.(string); ok && strings.Contains(s, "checksum mismatch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no checksum mismatch error in verify output; errors: %v", errs)
	}
}

// TestBackup_VerifyPassesOnCleanBackup ensures --verify succeeds when the
// backup is intact (sanity check for the verify mode itself).
func TestBackup_VerifyPassesOnCleanBackup(t *testing.T) {
	skipIfNoSqlite3(t)
	dataDir := createValidDataDir(t)
	defer os.RemoveAll(filepath.Dir(dataDir))

	backupDir := filepath.Join(filepath.Dir(dataDir), "backup-verify-clean")

	r := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
	})
	assertExitCode(t, r, 0)

	rv := runScript(t, "backup", []string{
		"-DataDir", dataDir,
		"-BackupDir", backupDir,
		"-Verify",
	})
	assertExitCode(t, rv, 0)
	vm, _ := rv.JSON["manifest"].(map[string]any)
	if vm["verified"] != true {
		t.Fatalf("verify on clean backup: verified = %v, want true\nstdout: %s",
			vm["verified"], rv.RawStdout)
	}
}
