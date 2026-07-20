import { defineConfig } from '@playwright/test';

/**
 * Playwright E2E configuration for the Go backend.
 *
 * Tests run sequentially with a single worker because they share a Go
 * backend instance started by the helpers in test/e2e/helpers.ts. The
 * webServer is NOT started here — the helpers manage the Go binary
 * lifecycle (build, seed fake GOWA, start, wait for /api/ready, stop).
 */
export default defineConfig({
  testDir: './test/e2e',
  fullyParallel: false,
  // Retry in CI so a transient timing flake (e.g. a freshly-started
  // instance reporting "running" a beat before its port accepts
  // connections through the proxy) self-heals; a real regression still
  // fails every attempt. Locally keep 0 for fast, deterministic feedback.
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'list',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:3000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
});
