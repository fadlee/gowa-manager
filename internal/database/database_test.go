package database

import (
	"context"
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesLegacyInstancesTable(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatalf("IntegrityCheck() error = %v", err)
	}
	columns := tableColumns(t, db.SQL)
	for _, name := range []string{"id", "key", "name", "port", "status", "config", "gowa_version", "created_at", "updated_at", "error_message"} {
		if !columns[name] {
			t.Fatalf("missing column %s in %#v", name, columns)
		}
	}
}

func TestOpenUpgradesLegacySchemaIdempotentlyAndPreservesRows(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	seedLegacyDB(t, filepath.Join(dataDir, "gowa.db"))

	db, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	db, err = Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer db.Close()

	var name, config, gowaVersion string
	var errorMessage sql.NullString
	if err := db.SQL.QueryRowContext(ctx, `SELECT name, config, gowa_version, error_message FROM instances WHERE key = ?`, "LEGACY01").Scan(&name, &config, &gowaVersion, &errorMessage); err != nil {
		t.Fatalf("select legacy row error = %v", err)
	}
	if name != "legacy" || config != `{"hello":"world"}` || gowaVersion != "latest" || errorMessage.Valid {
		t.Fatalf("unexpected row values name=%q config=%q gowa_version=%q error=%+v", name, config, gowaVersion, errorMessage)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatalf("IntegrityCheck() error = %v", err)
	}
}

func TestOpenConfiguresBusyTimeout(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	var timeout int
	if err := db.SQL.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatalf("busy_timeout query error = %v", err)
	}
	if timeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", timeout)
	}
}

func TestBunCanReopenGoWrittenDatabase(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skipf("bun unavailable for reopen compatibility test: %v", err)
	}
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO instances (key, name, port, config, gowa_version) VALUES (?, ?, ?, ?, ?)`, "GOBUN001", "go bun", 5010, `{"text":"compatible"}`, "latest")
	if err != nil {
		t.Fatalf("insert error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	script := `
import { Database } from 'bun:sqlite';
const db = new Database(process.argv[1]);
const row = db.query('SELECT key, name, config FROM instances WHERE key = ?').get('GOBUN001');
console.log(JSON.stringify(row));
db.close();
`
	cmd := exec.Command("bun", "--eval", script, filepath.Join(dataDir, "gowa.db"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun reopen error = %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, `"key":"GOBUN001"`) || !strings.Contains(got, `"config":"{\"text\":\"compatible\"}"`) {
		t.Fatalf("unexpected bun output: %s", got)
	}
}

func tableColumns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(instances)`)
	if err != nil {
		t.Fatalf("table_info error = %v", err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("table_info scan error = %v", err)
		}
		columns[name] = true
	}
	return columns
}

func seedLegacyDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db error = %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE instances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL UNIQUE,
  port INTEGER,
  status TEXT DEFAULT 'stopped',
  config TEXT DEFAULT '{}',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO instances (key, name, port, config) VALUES ('LEGACY01', 'legacy', 5001, '{"hello":"world"}');
`); err != nil {
		t.Fatalf("seed legacy db error = %v", err)
	}
}
