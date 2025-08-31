import { beforeAll, afterEach, afterAll } from 'bun:test';
import { testDb } from './test-db';

// Make the test database available to all tests
// @ts-ignore
global.testDb = testDb;

beforeAll(() => {
  // Any global setup before all tests
});

afterEach(async () => {
  // Clear data after each test
  testDb.exec('DELETE FROM instances');
});

afterAll(() => {
  // Close the database connection after all tests
  testDb.close();
});

// Extend the global type to include our test database
declare global {
  // eslint-disable-next-line no-var
  var testDb: import('bun:sqlite').Database;
}
