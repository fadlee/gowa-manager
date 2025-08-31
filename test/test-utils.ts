import { Database } from 'bun:sqlite';
import { faker } from '@faker-js/faker';

interface TestInstance {
  id?: string;
  name: string;
  binary_path: string;
  port: number;
  status: string;
  config: string;
}

export const createTestInstance = (db: Database, overrides: Partial<TestInstance> = {}): TestInstance => {
  const instance: TestInstance = {
    name: faker.lorem.word(),
    binary_path: faker.system.filePath(),
    port: faker.number.int({ min: 3000, max: 9000 }),
    status: 'stopped',
    config: JSON.stringify({ test: true }),
    ...overrides,
  };

  const { lastInsertRowid } = db.query(
    'INSERT INTO instances (name, binary_path, port, status, config) VALUES (?, ?, ?, ?, ?)'
  ).get(
    instance.name,
    instance.binary_path,
    instance.port,
    instance.status,
    instance.config
  ) as { lastInsertRowid: number };

  return { ...instance, id: lastInsertRowid.toString() };
};

export const clearDatabase = (db: Database) => {
  db.exec('PRAGMA foreign_keys = OFF');
  const tables = db.query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'");
  for (const { name } of tables.all()) {
    db.exec(`DELETE FROM ${name}`);
  }
  db.exec('PRAGMA foreign_keys = ON');
};
