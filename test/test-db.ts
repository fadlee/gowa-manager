import { Database } from 'bun:sqlite';

// Initialize an in-memory database for testing
export function createTestDatabase(): Database {
  const db = new Database(':memory:');

  // Create tables
  db.exec(`
    CREATE TABLE IF NOT EXISTS instances (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL UNIQUE,
      binary_path TEXT NOT NULL,
      port INTEGER,
      status TEXT DEFAULT 'stopped',
      config TEXT DEFAULT '{}',
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
  `);

  return db;
}

// Export a test database instance
export const testDb = createTestDatabase();
