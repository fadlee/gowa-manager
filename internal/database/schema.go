package database

const createInstancesTableSQL = `
CREATE TABLE IF NOT EXISTS instances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL UNIQUE,
  port INTEGER,
  status TEXT DEFAULT 'stopped',
  config TEXT DEFAULT '{}',
  gowa_version TEXT DEFAULT 'latest',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  error_message TEXT DEFAULT NULL
);`

var additiveInstanceColumns = []struct {
	name string
	sql  string
}{
	{"gowa_version", `ALTER TABLE instances ADD COLUMN gowa_version TEXT DEFAULT 'latest'`},
	{"error_message", `ALTER TABLE instances ADD COLUMN error_message TEXT DEFAULT NULL`},
}
