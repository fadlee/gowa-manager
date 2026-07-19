/**
 * E2E test environment helpers for the Go backend.
 *
 * These helpers build and start the Go manager with a deterministic
 * environment: a temporary data directory, a seeded fake GOWA binary,
 * known admin credentials, and a random port. They also provide an
 * authenticated API client and console/network health assertions.
 */
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtemp, mkdir, rm, copyFile, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

/** Default admin credentials used by the E2E environment. */
export const ADMIN_USERNAME = 'admin';
export const ADMIN_PASSWORD = 'test123';

/** The fake GOWA version seeded into the temp data directory. */
export const FAKE_GOWA_VERSION = 'v1.0.0';

const IS_WINDOWS = process.platform === 'win32';
const GOWA_BINARY_NAME = IS_WINDOWS ? 'gowa.exe' : 'gowa';
const MANAGER_BINARY = IS_WINDOWS
  ? join('dist-go', 'gowa-manager-go.exe')
  : join('dist-go', 'gowa-manager-go');

export interface GoBackendEnv {
  baseURL: string;
  stop: () => Promise<void>;
  dataDir: string;
  port: number;
}

/** Allocate a random free TCP port by binding to port 0. */
async function randomPort(): Promise<number> {
  const { createServer } = await import('node:net');
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (addr && typeof addr === 'object') {
        const port = addr.port;
        srv.close(() => resolve(port));
      } else {
        srv.close();
        reject(new Error('failed to allocate port'));
      }
    });
  });
}

/** Run a command and stream output to the parent. Resolves on exit 0. */
function runCommand(command: string, args: string[], opts: { cwd?: string } = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    const proc = spawn(command, args, { cwd: opts.cwd, stdio: 'inherit', shell: IS_WINDOWS });
    proc.on('error', reject);
    proc.on('exit', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} ${args.join(' ')} exited with code ${code}`));
    });
  });
}

/**
 * Build the fake GOWA binary and install it into the temp data dir's
 * bin/versions/{version}/ directory so the version resolver finds it.
 */
async function seedFakeGowa(dataDir: string, version: string): Promise<string> {
  const versionDir = join(dataDir, 'bin', 'versions', version);
  await mkdir(versionDir, { recursive: true });
  const binaryPath = join(versionDir, GOWA_BINARY_NAME);
  await runCommand('go', [
    'build',
    '-o',
    binaryPath,
    'github.com/fadlee/gowa-manager/internal/testutil/fakegowa',
  ]);
  return binaryPath;
}

/** Poll /api/ready until it returns 200 or time out. */
async function waitForReady(baseURL: string, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown;
  while (Date.now() < deadline) {
    try {
      const resp = await fetch(`${baseURL}/api/ready`);
      if (resp.ok) return;
    } catch (err) {
      lastErr = err;
    }
    await delay(200);
  }
  throw new Error(`/api/ready did not return 200 within ${timeoutMs}ms (last error: ${lastErr})`);
}

/**
 * Start the Go manager for E2E testing.
 *
 * 1. Creates a temp data directory.
 * 2. Builds and seeds a fake GOWA version.
 * 3. Starts the Go manager with known credentials and a random port.
 * 4. Waits for /api/ready to return 200.
 * 5. Returns the base URL, a stop function, and the data dir.
 */
export async function startGoBackend(_options: {
  /** Skip building the Go binary (assume it's already built). */
  skipBuild?: boolean;
} = {}): Promise<GoBackendEnv> {
  // Ensure the Go binary exists.
  try {
    await stat(MANAGER_BINARY);
  } catch {
    await runCommand('bun', ['run', 'build:go']);
  }

  const dataDir = await mkdtemp(join(tmpdir(), 'gowa-e2e-'));
  await seedFakeGowa(dataDir, FAKE_GOWA_VERSION);

  const port = await randomPort();
  const baseURL = `http://localhost:${port}`;

  const proc = spawn(MANAGER_BINARY, [], {
    stdio: ['ignore', 'pipe', 'pipe'],
    shell: IS_WINDOWS,
    env: {
      ...process.env,
      PORT: String(port),
      ADMIN_USERNAME,
      ADMIN_PASSWORD,
      DATA_DIR: dataDir,
    },
  });

  const stdoutBuf: string[] = [];
  const stderrBuf: string[] = [];
  proc.stdout?.on('data', (d) => stdoutBuf.push(d.toString()));
  proc.stderr?.on('data', (d) => stderrBuf.push(d.toString()));

  let stopped = false;
  const stop = async () => {
    if (stopped) return;
    stopped = true;
    if (proc.pid && !proc.killed) {
      try {
        proc.kill('SIGTERM');
      } catch {
        // ignore
      }
      // Wait up to 10s for graceful exit.
      await Promise.race([
        new Promise<void>((resolve) => proc.on('exit', () => resolve())),
        delay(10_000),
      ]);
      if (proc.pid && !proc.killed) {
        try {
          proc.kill('SIGKILL');
        } catch {
          // ignore
        }
      }
    }
    // On Windows the lock file may still be held briefly after the
    // process exits. Retry cleanup a few times with a short delay.
    for (let attempt = 0; attempt < 5; attempt++) {
      try {
        await rm(dataDir, { recursive: true, force: true });
        return;
      } catch {
        await delay(500);
      }
    }
    // Final best-effort attempt (ignore errors).
    try {
      await rm(dataDir, { recursive: true, force: true });
    } catch {
      // Leave temp dir for OS cleanup if still locked.
    }
  };

  // Ensure cleanup on unexpected exit.
  proc.on('exit', (code) => {
    if (!stopped) {
      stopped = true;
      if (code !== 0 && code !== null) {
        console.error(`Go manager exited unexpectedly with code ${code}`);
        console.error('stdout:', stdoutBuf.join(''));
        console.error('stderr:', stderrBuf.join(''));
      }
    }
  });

  try {
    await waitForReady(baseURL);
  } catch (err) {
    await stop();
    throw err;
  }

  return { baseURL, stop, dataDir, port };
}

// ---------------------------------------------------------------------------
// Authenticated API client
// ---------------------------------------------------------------------------

export interface ApiClientOptions {
  baseURL: string;
  username?: string;
  password?: string;
}

export interface Instance {
  id: number;
  key: string;
  name: string;
  port: number | null;
  status: string;
  config: string;
  gowa_version: string;
  error_message: string | null;
  created_at: string;
  updated_at: string;
}

export interface InstanceStatus {
  id: number;
  name: string;
  status: string;
  port: number | null;
  pid: number | null;
  uptime?: number;
  resources?: { cpuPercent: number; memoryMB: number };
  devices?: { count: number };
  error_message?: string;
}

/** Create an authenticated API helper wrapping fetch with Basic Auth. */
export function apiClient(opts: ApiClientOptions) {
  const username = opts.username ?? ADMIN_USERNAME;
  const password = opts.password ?? ADMIN_PASSWORD;
  const authHeader =
    'Basic ' + Buffer.from(`${username}:${password}`).toString('base64');

  async function request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    const resp = await fetch(`${opts.baseURL}${path}`, {
      method,
      headers: {
        Authorization: authHeader,
        'Content-Type': 'application/json',
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!resp.ok) {
      const text = await resp.text().catch(() => '');
      throw new Error(`${method} ${path} -> ${resp.status}: ${text}`);
    }
    if (resp.status === 204) return undefined as T;
    return resp.json() as Promise<T>;
  }

  return {
    getInstances: () => request<Instance[]>('GET', '/api/instances'),
    getInstance: (id: number) => request<Instance>('GET', `/api/instances/${id}`),
    createInstance: (data: {
      name?: string;
      config?: string;
      gowa_version?: string;
    }) => request<Instance>('POST', '/api/instances', data),
    updateInstance: (id: number, data: {
      name?: string;
      config?: string;
      gowa_version?: string;
    }) => request<Instance>('PUT', `/api/instances/${id}`, data),
    deleteInstance: (id: number) =>
      request<{ success: boolean }>('DELETE', `/api/instances/${id}`),
    startInstance: (id: number) =>
      request<InstanceStatus>('POST', `/api/instances/${id}/start`),
    stopInstance: (id: number) =>
      request<InstanceStatus>('POST', `/api/instances/${id}/stop`),
    restartInstance: (id: number) =>
      request<InstanceStatus>('POST', `/api/instances/${id}/restart`),
    killInstance: (id: number) =>
      request<InstanceStatus>('POST', `/api/instances/${id}/kill`),
    getInstanceStatus: (id: number) =>
      request<InstanceStatus>('GET', `/api/instances/${id}/status`),
    getInstanceDevices: (id: number) =>
      request<unknown>('GET', `/api/instances/${id}/devices`),
    resetInstanceData: (id: number) =>
      request<{ success: boolean }>('POST', `/api/instances/${id}/reset-data`),
    createAdminLink: (id: number) =>
      request<{ url: string; expiresAt?: string }>(
        'POST',
        `/api/instances/${id}/admin-link`
      ),
    testConnection: (id: number) =>
      request<{ ok: boolean; status?: number; message: string }>(
        'POST',
        `/api/instances/${id}/test-connection`
      ),
    getInstalledVersions: () =>
      request<unknown[]>('GET', '/api/system/versions/installed'),
    getSystemStatus: () =>
      request<unknown>('GET', '/api/system/status'),
    login: () =>
      request<{ success: boolean; user: string }>('POST', '/api/auth/login'),
  };
}

/**
 * Seed a test instance via the API and optionally wait for it to reach
 * a target status. Returns the created instance.
 */
export async function seedInstance(
  client: ReturnType<typeof apiClient>,
  options: { name?: string; gowaVersion?: string } = {}
): Promise<Instance> {
  const defaultConfig = {
    args: ['rest', '--port=PORT'],
    flags: { accountValidation: true, os: 'GowaManager' },
  };
  const instance = await client.createInstance({
    name: options.name ?? `e2e-${Date.now()}`,
    gowa_version: options.gowaVersion ?? FAKE_GOWA_VERSION,
    config: JSON.stringify(defaultConfig),
  });
  return instance;
}

/** Poll instance status until it matches the target or time out. */
export async function waitForInstanceStatus(
  client: ReturnType<typeof apiClient>,
  id: number,
  target: string,
  timeoutMs = 20_000
): Promise<InstanceStatus> {
  const deadline = Date.now() + timeoutMs;
  let last: InstanceStatus | undefined;
  while (Date.now() < deadline) {
    try {
      last = await client.getInstanceStatus(id);
      if (last.status?.toLowerCase() === target.toLowerCase()) return last;
    } catch {
      // retry
    }
    await delay(300);
  }
  throw new Error(
    `instance ${id} did not reach status "${target}" within ${timeoutMs}ms (last: ${JSON.stringify(last)})`
  );
}

// ---------------------------------------------------------------------------
// Console / network health tracking
// ---------------------------------------------------------------------------

export interface HealthTracker {
  errors: string[];
  attach: (page: import('@playwright/test').Page) => void;
  assert: (expect: import('@playwright/test').Expect) => void;
}

/**
 * Create a health tracker that records console errors, uncaught page
 * errors, API 5xx responses, and failed static asset loads. Call
 * `attach(page)` in each test and `assert(expect)` at the end.
 */
export function createHealthTracker(): HealthTracker {
  const errors: string[] = [];

  const attach = (page: import('@playwright/test').Page) => {
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text();
        // Suppress 404 console errors for proxied content — the fake
        // GOWA only serves /api/health and /health, so navigating to
        // other paths legitimately returns 404 which the browser logs.
        if (/404.*\(Not Found\)/.test(text) && page.url().includes('/app/')) {
          return;
        }
        errors.push(`console.error: ${text}`);
      }
    });
    page.on('pageerror', (err) => {
      errors.push(`pageerror: ${err.message}`);
    });
    page.on('response', (resp) => {
      const status = resp.status();
      const url = resp.url();
      // API 5xx responses are always errors.
      if (status >= 500 && url.includes('/api/')) {
        errors.push(`API ${status}: ${url}`);
      }
      // Failed static asset loads (CSS/JS) are errors — but only for
      // the manager's own assets, not proxied upstream content.
      if (
        status >= 400 &&
        /\.(js|css|ico|png|svg|woff2?)(\?|$)/.test(url) &&
        !url.includes('/app/')
      ) {
        errors.push(`asset ${status}: ${url}`);
      }
    });
  };

  const assert = (expect: import('@playwright/test').Expect) => {
    expect(errors).toEqual([]);
  };

  return { errors, attach, assert };
}
