import { Database } from 'bun:sqlite'
import { join, resolve } from 'node:path'
import { getConfig } from './cli'

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
  const config = getConfig()
  // Use data directory from CLI config
  const dataDir = config.dataDir
  // Resolve relative paths to absolute paths
  const absoluteDataDir = resolve(dataDir)
  if (!fs.existsSync(absoluteDataDir)) {
    fs.mkdirSync(absoluteDataDir, { recursive: true })
  }

  // Simple SQLite database connection
  const db = new Database(join(absoluteDataDir, 'gowa.db'))

  // Create tables
  db.exec(`
    CREATE TABLE IF NOT EXISTS instances (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      key TEXT UNIQUE NOT NULL,
      name TEXT NOT NULL UNIQUE,
      port INTEGER,
      status TEXT DEFAULT 'stopped',
      config TEXT DEFAULT '{}',
      gowa_version TEXT DEFAULT 'latest',
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
  `)

  // Migration: Add gowa_version column if it doesn't exist
  try {
    db.exec(`ALTER TABLE instances ADD COLUMN gowa_version TEXT DEFAULT 'latest'`)
    console.log('Added gowa_version column to existing instances table')
  } catch (error) {
    // Column already exists, which is fine
    if (!error.message.includes('duplicate column name')) {
      console.warn('Migration warning:', error.message)
    }
  }

  // Migration: Add error_message column if it doesn't exist
  try {
    db.exec(`ALTER TABLE instances ADD COLUMN error_message TEXT DEFAULT NULL`)
    console.log('Added error_message column to existing instances table')
  } catch (error) {
    // Column already exists, which is fine
    if (!error.message.includes('duplicate column name')) {
      console.warn('Migration warning:', error.message)
    }
  }

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
    INSERT INTO instances (key, name, port, config, gowa_version)
    VALUES (?, ?, ?, ?, ?)
    RETURNING *
  `),
  updateInstance: db.prepare(`
    UPDATE instances
    SET key = ?, name = ?, port = ?, config = ?, gowa_version = ?, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
    RETURNING *
  `),
  updateInstanceStatus: db.prepare(`
    UPDATE instances
    SET status = ?, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
    RETURNING *
  `),
  updateInstanceStatusWithError: db.prepare(`
    UPDATE instances
    SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
    RETURNING *
  `),
  clearInstanceError: db.prepare(`
    UPDATE instances
    SET error_message = NULL, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
    RETURNING *
  `),
  updateInstancePort: db.prepare(`
    UPDATE instances
    SET port = ?, updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
  `),
  deleteInstance: db.prepare('DELETE FROM instances WHERE id = ?'),

}
