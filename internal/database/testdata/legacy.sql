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

INSERT INTO instances (key, name, port, config)
VALUES ('LEGACY01', 'legacy', 5001, '{"hello":"world"}');
