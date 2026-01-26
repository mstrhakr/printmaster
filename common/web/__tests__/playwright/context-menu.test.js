/**
 * Context Menu Tests
 * 
 * Tests the context menu system for agents and devices.
 * Uses serial execution with shared page to minimize load times.
 */
const { test, expect } = require('@playwright/test');
const http = require('http');
const fs = require('fs');
const path = require('path');

const indexHtml = path.resolve(__dirname, '../../../../server/web/index.html');
const styleCss = path.resolve(__dirname, '../../../../server/web/style.css');
const appJs = path.resolve(__dirname, '../../../../server/web/app.js');
const rbacJs = path.resolve(__dirname, '../../../../server/web/rbac.js');
const contextMenuJs = path.resolve(__dirname, '../../../../server/web/context-menu.js');
const ssoAdminJs = path.resolve(__dirname, '../../../../server/web/sso-admin.js');
const chartsJs = path.resolve(__dirname, '../../../../server/web/utils/charts.js');
const formattersJs = path.resolve(__dirname, '../../../../server/web/utils/formatters.js');
const sharedCss = path.resolve(__dirname, '../../shared.css');
const sharedJs = path.resolve(__dirname, '../../shared.js');
const cardsJs = path.resolve(__dirname, '../../cards.js');
const metricsJs = path.resolve(__dirname, '../../metrics.js');

function serveFile(res, filePath, contentType) {
  const payload = fs.readFileSync(filePath);
  res.writeHead(200, { 'Content-Type': contentType });
  res.end(payload);
}

function normalizeStaticPath(pathname) {
  if (!pathname) return pathname;
  try {
    const decoded = decodeURIComponent(pathname);
    const [base] = decoded.split('{{');
    return base || decoded;
  } catch (err) {
    return pathname;
  }
}

function startAppFixtureServer() {
  return http.createServer((req, res) => {
    const url = new URL(req.url, 'http://localhost');
    try {
      if (url.pathname === '/' || url.pathname === '/app') {
        return serveFile(res, indexHtml, 'text/html');
      }
      const staticRoutes = {
        '/static/style.css': { file: styleCss, type: 'text/css' },
        '/static/shared.css': { file: sharedCss, type: 'text/css' },
        '/static/shared.js': { file: sharedJs, type: 'application/javascript' },
        '/static/cards.js': { file: cardsJs, type: 'application/javascript' },
        '/static/metrics.js': { file: metricsJs, type: 'application/javascript' },
        '/static/utils/charts.js': { file: chartsJs, type: 'application/javascript' },
        '/static/utils/formatters.js': { file: formattersJs, type: 'application/javascript' },
        '/static/app.js': { file: appJs, type: 'application/javascript' },
        '/static/rbac.js': { file: rbacJs, type: 'application/javascript' },
        '/static/context-menu.js': { file: contextMenuJs, type: 'application/javascript' },
        '/static/sso-admin.js': { file: ssoAdminJs, type: 'application/javascript' },
      };
      const normalizedPath = normalizeStaticPath(url.pathname);
      if (staticRoutes[normalizedPath]) {
        const entry = staticRoutes[normalizedPath];
        return serveFile(res, entry.file, entry.type);
      }
      if (url.pathname === '/favicon.ico') {
        res.writeHead(204);
        return res.end();
      }
      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('not found');
    } catch (err) {
      console.error('fixture server error', err);
      if (!res.headersSent) {
        res.writeHead(500, { 'Content-Type': 'text/plain' });
      }
      res.end('fixture server error');
    }
  });
}

let server;
let activeConnections = new Set();

async function waitForServerReady(url, maxRetries = 10, delay = 100) {
  for (let i = 0; i < maxRetries; i++) {
    try {
      await new Promise((resolve, reject) => {
        const req = http.get(url, (res) => {
          res.resume();
          resolve();
        });
        req.on('error', reject);
        req.setTimeout(1000, () => {
          req.destroy();
          reject(new Error('timeout'));
        });
      });
      return;
    } catch (err) {
      if (i === maxRetries - 1) throw new Error(`Server not ready after ${maxRetries} retries`);
      await new Promise(r => setTimeout(r, delay));
    }
  }
}

// Force serial execution - one worker only
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  server = startAppFixtureServer();
  
  server.on('connection', (conn) => {
    activeConnections.add(conn);
    conn.on('close', () => activeConnections.delete(conn));
  });
  
  await new Promise(resolve => server.listen(0, resolve));
  const { port } = server.address();
  global.__PM_BASE_URL__ = `http://127.0.0.1:${port}`;
  
  await waitForServerReady(`${global.__PM_BASE_URL__}/`);
});

test.afterAll(async () => {
  if (!server) return;
  await new Promise(resolve => setTimeout(resolve, 100));
  for (const conn of activeConnections) {
    conn.destroy();
  }
  activeConnections.clear();
  await new Promise(resolve => server.close(resolve));
});

const adminUser = { username: 'alice', role: 'admin', tenant_ids: [] };

const sampleAgents = [
  {
    agent_id: 'agent-001',
    name: 'Office Agent',
    ip: '192.168.1.100',
    version: '0.29.0',
    status: 'online',
    ws_connected: true,
    device_count: 5,
    last_seen: new Date().toISOString()
  }
];

const sampleDevices = [
  {
    serial: 'ABC123',
    model: 'HP LaserJet Pro',
    ip: '192.168.1.50',
    mac: '00:11:22:33:44:55',
    status: 'healthy',
    agent_id: 'agent-001',
    agent_name: 'Office Agent'
  }
];

function createApiHandler(apiCalls = []) {
  return route => {
    const url = route.request().url();
    const method = route.request().method();
    
    // Track POST calls for verification
    if (method === 'POST') {
      const postData = route.request().postData();
      apiCalls.push({ url, method, body: postData ? JSON.parse(postData) : null });
    }
    
    if (url.includes('/api/v1/auth/me')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(adminUser) });
    }
    if (url.includes('/api/v1/agents/list')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sampleAgents) });
    }
    if (url.includes('/api/v1/devices/delete') && method === 'POST') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true, deleted_from_agent: true }) });
    }
    if (url.includes('/api/v1/devices')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sampleDevices) });
    }
    if (url.includes('/api/v1/tenants')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([{ id: 't1', name: 'Test Tenant' }]) });
    }
    if (method === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  };
}

// Helper to check if running on mobile viewport
function isMobileViewport(page) {
  const size = page.viewportSize();
  return size && size.width < 768;
}

// ========== Device Context Menu - Single Page Load ==========

test('device context menu: appearance, actions, delete flow', async ({ page }) => {
  test.skip(isMobileViewport(page), 'Context menus are desktop-only (mobile uses different navigation)');
  // Grant clipboard permissions upfront
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  
  const apiCalls = [];
  
  // Setup mocks
  await page.addInitScript(() => {
    window.EventSource = class {
      constructor() { this.readyState = 1; }
      addEventListener() {}
      close() {}
    };
    window.WebSocket = class {
      constructor() {}
      addEventListener() {}
      close() {}
    };
  });
  await page.route('**/api/**', createApiHandler(apiCalls));
  
  // Load app once
  await page.goto(`${global.__PM_BASE_URL__}/app`, { waitUntil: 'networkidle' });
  await page.waitForSelector('#desktop_tabs .tab[data-target="devices"]', { timeout: 10000 });
  
  // Navigate to devices
  await page.locator('#desktop_tabs [data-target="devices"]').first().click();
  await page.waitForSelector('[data-serial="ABC123"]', { timeout: 10000 });
  
  // --- Test 1: Context menu appears on right-click ---
  const deviceElement = page.locator('[data-serial="ABC123"]').first();
  await deviceElement.click({ button: 'right' });
  
  const contextMenu = page.locator('.pm-context-menu');
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  
  // Verify menu items exist
  await expect(contextMenu.locator('[data-action="show-printer-details"]')).toBeVisible();
  await expect(contextMenu.locator('[data-action="copy-serial"]')).toBeVisible();
  await expect(contextMenu.locator('[data-action="delete-device"]')).toBeVisible();
  
  // Verify delete has danger styling
  const deleteItem = contextMenu.locator('[data-action="delete-device"]');
  await expect(deleteItem).toHaveClass(/danger/);
  
  // Verify dividers exist
  await expect(contextMenu.locator('.pm-context-menu-divider').first()).toBeVisible();
  
  // --- Test 2: Menu closes on escape ---
  await page.keyboard.press('Escape');
  await expect(contextMenu).not.toBeVisible({ timeout: 2000 });
  
  // --- Test 3: Menu closes on outside click ---
  await deviceElement.click({ button: 'right' });
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  await page.click('body', { position: { x: 10, y: 10 } });
  await expect(contextMenu).not.toBeVisible({ timeout: 2000 });
  
  // --- Test 4: Copy serial shows toast ---
  await deviceElement.click({ button: 'right' });
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  await contextMenu.locator('[data-action="copy-serial"]').click();
  
  await expect(page.locator('.toast:visible').first()).toBeVisible({ timeout: 3000 });
  await expect(page.locator('.toast:visible').first()).toContainText('copied');
  
  // Wait for toast to disappear fully
  await page.waitForFunction(() => document.querySelectorAll('.toast').length === 0, { timeout: 5000 });
  
  // --- Test 5: Copy IP shows toast ---
  await deviceElement.click({ button: 'right' });
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  await contextMenu.locator('[data-action="copy-device-ip"]').click();
  
  await expect(page.locator('.toast:visible').first()).toBeVisible({ timeout: 3000 });
  await page.waitForFunction(() => document.querySelectorAll('.toast').length === 0, { timeout: 5000 });
  
  // --- Test 6: Delete device shows confirmation modal ---
  await deviceElement.click({ button: 'right' });
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  await contextMenu.locator('[data-action="delete-device"]').click();
  
  const modal = page.locator('.modal-overlay:visible');
  await expect(modal).toBeVisible({ timeout: 5000 });
  await expect(modal.locator('text=ABC123')).toBeVisible();
  await expect(modal.locator('text=Also delete metrics history')).toBeVisible();
  await expect(modal.locator('text=Also delete from agent')).toBeVisible();
  
  // --- Test 7: Modal checkboxes work ---
  const checkboxes = modal.locator('input[type="checkbox"]');
  await expect(checkboxes.first()).not.toBeChecked();
  await checkboxes.first().click();
  await expect(checkboxes.first()).toBeChecked();
  await checkboxes.nth(1).click();
  await expect(checkboxes.nth(1)).toBeChecked();
  
  // --- Test 8: Cancel closes modal without API call ---
  apiCalls.length = 0; // Clear tracked calls
  await modal.locator('button:has-text("Cancel")').click();
  await expect(modal).not.toBeVisible({ timeout: 2000 });
  expect(apiCalls.filter(c => c.url.includes('/delete'))).toHaveLength(0);
  
  // --- Test 9: Delete sends correct API request ---
  await deviceElement.click({ button: 'right' });
  await contextMenu.locator('[data-action="delete-device"]').click();
  await expect(modal).toBeVisible({ timeout: 5000 });
  
  // Check both options
  const modalCheckboxes = modal.locator('input[type="checkbox"]');
  await modalCheckboxes.first().click();
  await modalCheckboxes.nth(1).click();
  
  // Click delete
  await modal.locator('button:has-text("Delete")').click();
  
  // Wait for API call
  await page.waitForTimeout(500);
  
  const deleteCall = apiCalls.find(c => c.url.includes('/devices/delete'));
  expect(deleteCall).toBeTruthy();
  expect(deleteCall.body.serial).toBe('ABC123');
  expect(deleteCall.body.delete_metrics).toBe(true);
  expect(deleteCall.body.delete_from_agent).toBe(true);
  expect(deleteCall.body.agent_id).toBe('agent-001');
});

// ========== Agent Context Menu - Single Page Load ==========

test('agent context menu: appearance and actions', async ({ page }) => {
  test.skip(isMobileViewport(page), 'Context menus are desktop-only (mobile uses different navigation)');
  
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  
  await page.addInitScript(() => {
    window.EventSource = class {
      constructor() { this.readyState = 1; }
      addEventListener() {}
      close() {}
    };
    window.WebSocket = class {
      constructor() {}
      addEventListener() {}
      close() {}
    };
  });
  await page.route('**/api/**', createApiHandler());
  
  await page.goto(`${global.__PM_BASE_URL__}/app`, { waitUntil: 'networkidle' });
  await page.waitForSelector('#desktop_tabs .tab[data-target="agents"]', { timeout: 10000 });
  
  // Navigate to agents (should already be there by default)
  await page.locator('#desktop_tabs [data-target="agents"]').first().click();
  await page.waitForSelector('[data-agent-id="agent-001"]', { timeout: 10000 });
  
  // Wait for context menu handlers to be registered
  await page.waitForFunction(() => typeof window.PMContextMenu !== 'undefined', { timeout: 5000 });
  
  // Brief pause to ensure DOM is stable and event handlers are attached
  await page.waitForTimeout(200);
  
  const agentElement = page.locator('[data-agent-id="agent-001"]').first();
  await expect(agentElement).toBeVisible();
  
  // --- Test 1: Context menu appears ---
  await agentElement.click({ button: 'right', force: true });
  
  const contextMenu = page.locator('.pm-context-menu');
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  
  // Verify agent menu items
  await expect(contextMenu.locator('[data-action="view-agent"]')).toBeVisible();
  await expect(contextMenu.locator('[data-action="copy-agent-id"]')).toBeVisible();
  await expect(contextMenu.locator('[data-action="delete-agent"]')).toBeVisible();
  
  // Verify delete has danger styling
  await expect(contextMenu.locator('[data-action="delete-agent"]')).toHaveClass(/danger/);
  
  // --- Test 2: Copy agent ID shows toast ---
  await contextMenu.locator('[data-action="copy-agent-id"]').click();
  
  await expect(page.locator('.toast:visible').first()).toBeVisible({ timeout: 3000 });
  await expect(page.locator('.toast:visible').first()).toContainText('Agent ID');
  
  await page.waitForFunction(() => document.querySelectorAll('.toast').length === 0, { timeout: 5000 });
  
  // --- Test 3: Copy IP shows toast ---
  await agentElement.click({ button: 'right' });
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  await contextMenu.locator('[data-action="copy-agent-ip"]').click();
  
  await expect(page.locator('.toast:visible').first()).toBeVisible({ timeout: 3000 });
  await page.waitForFunction(() => document.querySelectorAll('.toast').length === 0, { timeout: 5000 });
  
  // --- Test 4: Menu closes on escape ---
  await agentElement.click({ button: 'right' });
  await expect(contextMenu).toBeVisible({ timeout: 5000 });
  await page.keyboard.press('Escape');
  await expect(contextMenu).not.toBeVisible({ timeout: 2000 });
});
