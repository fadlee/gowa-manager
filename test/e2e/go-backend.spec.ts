import { test, expect, type Page, type Expect } from '@playwright/test';
import {
  startGoBackend,
  stopGoBackend,
  apiClient,
  seedInstance,
  waitForInstanceStatus,
  createHealthTracker,
  ADMIN_USERNAME,
  ADMIN_PASSWORD,
  FAKE_GOWA_VERSION,
  type Instance,
} from './helpers';

let baseURL: string;
let stop: () => Promise<void>;
let client: ReturnType<typeof apiClient>;

test.beforeAll(async () => {
  const env = await startGoBackend({});
  baseURL = env.baseURL;
  stop = env.stop;
  client = apiClient({ baseURL });
});

test.afterAll(async () => {
  if (stop) await stop();
});

/** The workspace landing view: the summary, or the empty state when there are no instances. */
function workspaceHome(page: Page) {
  return page.getByRole('heading', { name: /^(Summary|Create your first instance)$/ });
}

/** Log in via the frontend login form. */
async function login(page: Page) {
  await page.goto(baseURL);
  await page.getByLabel('Username').fill(ADMIN_USERNAME);
  await page.getByLabel('Password').fill(ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(workspaceHome(page)).toBeVisible();
}

/** Log out through the account menu. */
async function logout(page: Page) {
  await page.locator('[aria-haspopup="menu"]').click();
  await page.getByRole('menuitem', { name: 'Log out' }).click();
  await expect(page.getByText('Sign in to GOWA Manager')).toBeVisible();
}

/** The sidebar row of an instance. */
function instanceRow(page: Page, name: string) {
  return page.locator('[data-instance-row]').filter({ hasText: name });
}

/** Open an instance from the sidebar and wait for its panel. */
async function openInstance(page: Page, name: string) {
  await instanceRow(page, name).click();
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
}

/** Open a tab of the selected instance. */
async function openTab(page: Page, label: string) {
  await page.getByRole('navigation', { name: 'Instance sections' }).getByRole('button', { name: label }).click();
}

/** Create an instance via the UI and return its name. */
async function createInstanceViaUI(page: Page, name: string): Promise<string> {
  // "New" in the sidebar, or "New instance" on the empty state.
  await page.getByRole('button', { name: /^New( instance)?$/ }).first().click();
  await page.getByLabel('Name').fill(name);
  await page.getByRole('button', { name: 'Create' }).click();
  // The new instance opens in the detail panel.
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  return name;
}

test.describe('Go backend E2E', () => {
  test.describe.configure({ mode: 'serial' });

  test('login and logout', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    await login(page);

    await logout(page);

    health.assert(expect);
  });

  test('workspace shows instance list', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    // Seed an instance via the API so the list isn't empty.
    const instance = await seedInstance(client, { name: 'dash-list-instance' });
    await login(page);

    await expect(instanceRow(page, 'dash-list-instance')).toBeVisible();

    // Cleanup.
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('create instance via UI', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    await login(page);
    const name = `ui-created-${Date.now()}`;
    await createInstanceViaUI(page, name);

    // Verify via the API that the instance exists.
    const instances = await client.getInstances();
    const created = instances.find((i) => i.name === name);
    expect(created).toBeTruthy();
    await client.deleteInstance(created!.id);

    health.assert(expect);
  });

  test('start instance via UI', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'start-ui-instance' });
    await login(page);

    // Rows show Start/Stop/Restart on hover.
    await instanceRow(page, 'start-ui-instance').hover();
    await page.getByRole('button', { name: 'Start start-ui-instance', exact: true }).click();

    // Wait for the status to become running via the API.
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    // The summary counts it as running.
    await expect(page.getByText('Running').first()).toBeVisible();

    // Cleanup: stop and delete.
    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('stop instance via UI', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'stop-ui-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    await login(page);

    await instanceRow(page, 'stop-ui-instance').hover();
    await page.getByRole('button', { name: 'Stop stop-ui-instance', exact: true }).click();

    // A confirmation dialog appears for stop.
    await expect(page.getByRole('heading', { name: 'Stop stop-ui-instance?' })).toBeVisible();
    await page.getByRole('button', { name: 'Stop instance' }).click();

    // Wait for stopped status.
    await waitForInstanceStatus(client, instance.id, 'stopped', 20_000);

    // Cleanup.
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('restart instance via detail page', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'restart-detail-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    await login(page);
    await openInstance(page, 'restart-detail-instance');

    // Click the Restart button in the detail header.
    await page.getByRole('button', { name: 'Restart', exact: true }).click();

    // The instance should remain running after restart.
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    // Cleanup.
    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('kill instance via detail danger zone', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'kill-detail-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    await login(page);
    await openInstance(page, 'kill-detail-instance');

    // The danger zone sits at the bottom of the Settings tab.
    await openTab(page, 'Settings');

    // Click "Force Kill" to reveal confirmation.
    await page.getByRole('button', { name: 'Force Kill' }).click();
    // Confirm kill.
    await page.getByRole('button', { name: 'Confirm Kill' }).click();

    // Wait for the instance to be stopped (killed).
    await waitForInstanceStatus(client, instance.id, 'stopped', 20_000);

    // Cleanup.
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('delete instance via detail danger zone', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'delete-detail-instance' });
    await login(page);

    await openInstance(page, 'delete-detail-instance');
    await openTab(page, 'Settings');

    // Click Delete to reveal confirmation.
    await page.getByRole('button', { name: 'Delete', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm Delete' }).click();

    // Should go back to the workspace summary.
    await expect(workspaceHome(page)).toBeVisible();

    // Verify via the API that the instance is gone.
    const instances = await client.getInstances();
    expect(instances.find((i) => i.id === instance.id)).toBeUndefined();

    health.assert(expect);
  });

  test('reset instance data via detail danger zone', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'reset-detail-instance' });
    await login(page);

    await openInstance(page, 'reset-detail-instance');
    await openTab(page, 'Settings');

    // Click "Reset Data" to reveal confirmation.
    await page.getByRole('button', { name: 'Reset Data' }).click();
    await page.getByRole('button', { name: 'Confirm Reset' }).click();

    // The instance should still exist after reset.
    const after = await client.getInstance(instance.id);
    expect(after.id).toBe(instance.id);

    // Cleanup.
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('devices endpoint returns a response', async () => {
    const instance = await seedInstance(client, { name: 'devices-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    // The devices endpoint should respond (even if no devices connected).
    const devices = await client.getInstanceDevices(instance.id);
    expect(devices).toBeTruthy();

    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);
  });

  test('test connection endpoint returns a response', async () => {
    const instance = await seedInstance(client, { name: 'conn-test-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    const result = await client.testConnection(instance.id);
    expect(result).toBeTruthy();
    expect(typeof result.ok).toBe('boolean');

    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);
  });

  test('admin link via API opens proxy', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'admin-link-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    // Get the admin link via the API.
    const link = await client.createAdminLink(instance.id);
    expect(link.url).toBeTruthy();

    // Navigate to the admin link URL (relative path with autologin token).
    const adminUrl = baseURL + link.url;
    await page.goto(adminUrl);

    // The proxy should forward to the fake GOWA instance. The fake GOWA
    // serves /api/health -> {"status":"ok"}. The proxy strips the
    // /app/{key}/ prefix, so navigating to the root should return the
    // fake GOWA root response (or a redirect after autologin).
    // We just verify the page loads without a 401/502 error.
    await page.waitForLoadState('networkidle');

    // Cleanup.
    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('proxy navigation to running instance', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'proxy-nav-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    // A "running" status can precede the instance's port accepting
    // proxied connections. Poll the forwarded health endpoint until it
    // responds ok BEFORE navigating the page — an early navigation would
    // otherwise surface a transient 502 in the browser console that the
    // health tracker rightly flags. page.request shares the browser
    // context but does not drive page navigation, so it warms the
    // upstream connection without recording console errors.
    const healthUrl = `${baseURL}/app/${instance.key}/api/health`;
    await expect(async () => {
      const resp = await page.request.get(healthUrl);
      expect(resp.ok()).toBeTruthy();
      const body = await resp.json();
      expect(body.status).toBe('ok');
    }).toPass({ timeout: 15_000 });

    // Now navigate to the proxy root (without autologin — the proxy
    // injects instance-level Basic Auth). The fake GOWA serves its
    // routes under the /app/{key} base path and returns 404 for the bare
    // root; the health tracker suppresses proxied 404s. We just verify
    // the proxy didn't return a 502/503 page error.
    const proxyUrl = `${baseURL}/app/${instance.key}/`;
    await page.goto(proxyUrl);
    await page.waitForLoadState('networkidle');

    // Cleanup.
    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('proxy status endpoint', async ({ page }) => {
    const instance = await seedInstance(client, { name: 'proxy-status-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    const statusUrl = `${baseURL}/app/${instance.key}/status`;
    const resp = await page.request.get(statusUrl);
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body.instanceKey).toBe(instance.key);
    expect(body.status).toBe('running');

    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);
  });

  test('proxy health endpoint', async ({ page }) => {
    const instance = await seedInstance(client, { name: 'proxy-health-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    const healthUrl = `${baseURL}/app/${instance.key}/health`;
    const resp = await page.request.get(healthUrl);
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body.instanceKey).toBe(instance.key);
    // The fake GOWA only serves /api/health and /health, so the root
    // health probe (GET /) returns 404 → healthy=false. The important
    // assertion is that the endpoint responds with the correct shape.
    expect(typeof body.healthy).toBe('boolean');

    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);
  });

  test('WebSocket-backed fixture action through proxy', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'ws-proxy-instance' });
    await client.startInstance(instance.id);
    await waitForInstanceStatus(client, instance.id, 'running', 20_000);

    // The fake GOWA echoes WebSocket messages at {basePath}/ws, like the
    // live-updates socket of real GOWA. Round-trip a message through the
    // manager's WebSocket proxy.
    const wsUrl = `${baseURL.replace('http', 'ws')}/app/${instance.key}/ws`;
    const result = await page.evaluate(async (url) => {
      return new Promise<{ echoed?: string; error?: string }>((resolve) => {
        const ws = new WebSocket(url);
        const timer = setTimeout(() => resolve({ error: 'timeout' }), 5000);
        ws.onopen = () => ws.send('ping-through-proxy');
        ws.onmessage = (event) => {
          clearTimeout(timer);
          ws.close();
          resolve({ echoed: String(event.data) });
        };
        ws.onerror = () => {
          clearTimeout(timer);
          resolve({ error: 'connection error' });
        };
      });
    }, wsUrl);
    expect(result).toEqual({ echoed: 'ping-through-proxy' });

    // Verify the manager is still alive after the WebSocket session.
    const readyResp = await page.request.get(`${baseURL}/api/ready`);
    expect(readyResp.ok()).toBeTruthy();

    await client.stopInstance(instance.id);
    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('install version via API with fixture', async () => {
    // The version installer downloads from GitHub. Without a fixture
    // GitHub endpoint, we verify the installed-versions endpoint
    // reports our seeded fake GOWA version.
    const installed = await client.getInstalledVersions();
    const versions = installed as Array<{ version: string; installed: boolean }>;
    const seeded = versions.find((v) => v.version === FAKE_GOWA_VERSION);
    expect(seeded).toBeTruthy();
    expect(seeded!.installed).toBe(true);
  });

  test('switch instance version via API', async () => {
    const instance = await seedInstance(client, {
      name: 'switch-version-instance',
      gowaVersion: FAKE_GOWA_VERSION,
    });

    // Update the instance to use the same version (effectively a no-op
    // switch, but exercises the update path).
    const updated = await client.updateInstance(instance.id, {
      gowa_version: FAKE_GOWA_VERSION,
    });
    expect(updated.gowa_version).toBe(FAKE_GOWA_VERSION);

    await client.deleteInstance(instance.id);
  });

  test('update instance name via detail settings', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    const instance = await seedInstance(client, { name: 'update-name-instance' });
    await login(page);

    await openInstance(page, 'update-name-instance');
    await openTab(page, 'Settings');

    // The settings section has a name input with a placeholder.
    const nameInput = page.getByPlaceholder('Enter instance name...');
    await nameInput.fill('updated-name-instance');
    await page.getByRole('button', { name: /Save Changes/i }).click();

    // Verify via the API that the name changed.
    const after = await client.getInstance(instance.id);
    expect(after.name).toBe('updated-name-instance');

    await client.deleteInstance(instance.id);

    health.assert(expect);
  });

  test('no console errors or API 5xx across full flow', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    // Full flow: login -> workspace -> create -> start -> stop -> delete -> logout.
    await login(page);

    const name = `health-flow-${Date.now()}`;
    await createInstanceViaUI(page, name);

    const instances = await client.getInstances();
    const created = instances.find((i) => i.name === name);
    expect(created).toBeTruthy();

    // Start via API (UI start tested separately).
    await client.startInstance(created!.id);
    await waitForInstanceStatus(client, created!.id, 'running', 20_000);

    // Stop via API.
    await client.stopInstance(created!.id);
    await waitForInstanceStatus(client, created!.id, 'stopped', 20_000);

    // Delete via API.
    await client.deleteInstance(created!.id);

    await logout(page);

    health.assert(expect);
  });

  test('authentication loop does not occur', async ({ page }) => {
    const health = createHealthTracker();
    health.attach(page);

    // Login and verify we stay on the workspace (no redirect loop back
    // to the login page).
    await login(page);
    await page.waitForLoadState('networkidle');
    await expect(workspaceHome(page)).toBeVisible();

    // Reload the page — the session cookie should keep us authenticated.
    await page.reload();
    await expect(workspaceHome(page)).toBeVisible();

    health.assert(expect);
  });
});
