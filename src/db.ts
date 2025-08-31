import { Database } from 'bun:sqlite'
import { join } from 'node:path'

// Utility function to generate 8-character random keys
function generateInstanceKey(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
  let key = ''
  for (let i = 0; i < 8; i++) {
    key += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  return key
}

// Initialize database schema first
function initializeDatabase() {
  // Create data directory if it doesn't exist
  const fs = require('node:fs')
  const dataDir = join(process.cwd(), 'data')
  if (!fs.existsSync(dataDir)) {
    fs.mkdirSync(dataDir, { recursive: true })
  }

  // Simple SQLite database connection
  const db = new Database(join(process.cwd(), 'data', 'gowa.db'))

  // Create tables
  db.exec(`
    CREATE TABLE IF NOT EXISTS instances (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      key TEXT UNIQUE NOT NULL,
      name TEXT NOT NULL UNIQUE,
      port INTEGER,
      status TEXT DEFAULT 'stopped',
      config TEXT DEFAULT '{}',
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
  `)

  console.log('Database initialized successfully')
  return db
}

// Initialize database and get connection
const db = initializeDatabase()

// Export db for direct access when needed
export { db, generateInstanceKey }
export const queries = {
  // Instances
  getAllInstances: db.prepare('SELECT * FROM instances ORDER BY created_at DESC'),
  getInstanceById: db.prepare('SELECT * FROM instances WHERE id = ?'),
  getInstanceByKey: db.prepare('SELECT * FROM instances WHERE key = ?'),
  createInstance: db.prepare(`
    INSERT INTO instances (key, name, port, config)
    VALUES (?, ?, ?, ?)
    RETURNING *
  `),
  updateInstance: db.prepare(`
    UPDATE instances
    SET key = ?, name = ?, port = ?, config = ?, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
    RETURNING *
  `),
  updateInstanceStatus: db.prepare(`
    UPDATE instances
    SET status = ?, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
    RETURNING *
  `),
  deleteInstance: db.prepare('DELETE FROM instances WHERE id = ?'),

}
