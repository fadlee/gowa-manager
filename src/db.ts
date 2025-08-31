import { Database } from 'bun:sqlite'
import { join } from 'path'

// Initialize database schema first
function initializeDatabase() {
  // Create data directory if it doesn't exist
  const fs = require('fs')
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
export { db }

// Simple CRUD helpers
export const queries = {
  // Instances
  getAllInstances: db.prepare('SELECT * FROM instances ORDER BY created_at DESC'),
  getInstanceById: db.prepare('SELECT * FROM instances WHERE id = ?'),
  createInstance: db.prepare(`
    INSERT INTO instances (name, port, config) 
    VALUES (?, ?, ?) 
    RETURNING *
  `),
  updateInstance: db.prepare(`
    UPDATE instances 
    SET name = ?, port = ?, config = ?, updated_at = CURRENT_TIMESTAMP 
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
