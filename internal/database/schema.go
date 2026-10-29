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

// authTablesSQL creates the user management tables. Session and API key
// tokens are stored only as SHA-256 hashes; deleting a user cascades to
// its sessions and API keys.
var authTablesSQL = []string{
	`CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  is_active INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`,
	`CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at DATETIME NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);`,
	`CREATE TABLE IF NOT EXISTS api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  last_used_at DATETIME DEFAULT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);`,
	`CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`,
}
