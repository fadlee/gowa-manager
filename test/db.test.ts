import { describe, it, expect } from 'bun:test';
import { testDb } from './test-db';

describe('Database', () => {
  it('should initialize with the correct tables', () => {
    const tables = testDb
      .query("SELECT name FROM sqlite_master WHERE type='table'")
      .all()
      .map((t: any) => t.name);

    expect(tables).toContain('instances');
  });

  // No ports table in the updated schema

  it('should insert and retrieve an instance', () => {
    const instance = {
      name: 'test-instance',
      binary_path: '/path/to/binary',
      port: 3000,
      status: 'stopped',
      config: JSON.stringify({ test: true })
    };

    // Insert the instance
    const insertStmt = testDb.prepare(
      'INSERT INTO instances (name, binary_path, port, status, config) VALUES (?, ?, ?, ?, ?)'
    );

    // Execute the insert statement
    const insertResult = insertStmt.run(
      instance.name,
      instance.binary_path,
      instance.port,
      instance.status,
      instance.config
    ) as { lastInsertRowid: number };

    // Retrieve and verify
    const selectStmt = testDb.prepare('SELECT * FROM instances WHERE id = ?');
    const row = selectStmt.get(insertResult.lastInsertRowid) as any;

    expect(row).toBeDefined();
    expect(row.name).toBe(instance.name);
    expect(row.binary_path).toBe(instance.binary_path);
    expect(row.port).toBe(instance.port);
    expect(row.status).toBe(instance.status);
    expect(row.config).toBe(instance.config);
  });
});

describe('Instance', () => {
  it('should insert and retrieve an instance', () => {
    const instance = {
      name: 'test-instance',
      binary_path: '/path/to/binary',
      port: 3000,
      status: 'stopped',
      config: JSON.stringify({ test: true })
    };

    // Insert the instance
    const insertStmt = testDb.prepare(
      'INSERT INTO instances (name, binary_path, port, status, config) VALUES (?, ?, ?, ?, ?)'
    );

    // Execute the insert statement
    const insertResult = insertStmt.run(
      instance.name,
      instance.binary_path,
      instance.port,
      instance.status,
      instance.config
    ) as { lastInsertRowid: number };

    // Retrieve and verify
    const selectStmt = testDb.prepare('SELECT * FROM instances WHERE id = ?');
    const row = selectStmt.get(insertResult.lastInsertRowid) as any;

    expect(row).toBeDefined();
    expect(row.name).toBe(instance.name);
    expect(row.binary_path).toBe(instance.binary_path);
    expect(row.port).toBe(instance.port);
    expect(row.status).toBe(instance.status);
    expect(row.config).toBe(instance.config);
  });
});
