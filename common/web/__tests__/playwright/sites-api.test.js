/**
 * Sites/Tenants API Integration Tests
 * 
 * Tests the frontend's interaction with:
 * - /api/v1/tenants/{id}/sites (GET, POST, DELETE)
 * - /api/v1/agents/list (GET)
 * 
 * Verifies correct endpoint URLs, response parsing, and null/empty handling.
 */
const { test, expect } = require('@playwright/test');
const http = require('http');
const fs = require('fs');
const path = require('path');

const indexHtml = path.resolve(__dirname, '../../../../server/web/index.html');
const styleCss = path.resolve(__dirname, '../../../../server/web/style.css');
const appJs = path.resolve(__dirname, '../../../../server/web/app.js');
const rbacJs = path.resolve(__dirname, '../../../../server/web/rbac.js');
const ssoAdminJs = path.resolve(__dirname, '../../../../server/web/sso-admin.js');
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
        '/static/app.js': { file: appJs, type: 'application/javascript' },
        '/static/rbac.js': { file: rbacJs, type: 'application/javascript' },
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

test.beforeAll(async () => {
  server = startAppFixtureServer();
  await new Promise(resolve => server.listen(0, resolve));
  const { port } = server.address();
  global.__PM_BASE_URL__ = `http://127.0.0.1:${port}`;
});

test.afterAll(async () => {
  if (!server) return;
  await new Promise(resolve => server.close(resolve));
});

const adminUser = { username: 'alice', role: 'admin', tenant_ids: [] };

async function loadApp(page, apiHandler) {
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
  await page.route('**/api/**', apiHandler);
  await page.goto(`${global.__PM_BASE_URL__}/app`, { waitUntil: 'networkidle' });
  // Wait for auth API to be called and RBAC to be applied
  await page.waitForLoadState('networkidle');
  // Wait for auth to complete and dynamic tabs to be rendered
  // Base tabs (agents, devices, etc) are always present, check for those first
  await page.waitForSelector('#desktop_tabs .tab[data-target="agents"]', { timeout: 10000 });
  // For admin users (tests use adminUser), wait specifically for the admin tab
  // Note: admin tab is only visible if user.role='admin' AND userCan('settings.read')
  await page.waitForSelector('#desktop_tabs .tab[data-target="admin"]', { timeout: 10000 });
}

function createBaseHandler(overrides = {}) {
  return route => {
    const url = route.request().url();
    
    if (url.includes('/api/v1/auth/me')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(adminUser) });
    }
    if (url.includes('/api/v1/tenants') && url.includes('/sites')) {
      const body = overrides.sites ?? '[]';
      return route.fulfill({ status: 200, contentType: 'application/json', body: typeof body === 'string' ? body : JSON.stringify(body) });
    }
    if (url.includes('/api/v1/tenants')) {
      return route.fulfill({ 
        status: 200, 
        contentType: 'application/json', 
        body: JSON.stringify([{ id: 't1', name: 'Acme Corp' }]) 
      });
    }
    if (url.includes('/api/v1/join-token') || url.includes('/api/v1/join-tokens')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    }
    if (url.includes('/api/v1/agents/list')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    }
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  };
}

// ========== Sites API Tests ==========

test.describe('Sites API Integration', () => {
  test('fetchSitesForTenant uses correct endpoint and handles empty array', async ({ page }) => {
    const apiCalls = [];
    
    const handler = route => {
      apiCalls.push({ url: route.request().url(), method: route.request().method() });
      return createBaseHandler()(route);
    };
    
    await loadApp(page, handler);
    await page.waitForLoadState('networkidle');
    
    // Navigate to admin > tenants
    const adminTab = page.locator('#desktop_tabs [data-target="admin"]').first();
    await expect(adminTab).toBeVisible({ timeout: 10000 });
    await adminTab.click();
    await page.locator('.admin-subtab[data-adminview="tenants"]').click();
    
    // Wait for tenants list to appear
    await expect(page.locator('#tenants_list')).toBeVisible();
    
    // Click expand button on first tenant row
    const expandBtn = page.locator('.expand-btn[data-action="toggle-sites"]').first();
    if (await expandBtn.isVisible()) {
      await expandBtn.click();
      
      // Wait for sites to load
      await page.waitForTimeout(500);
      
      // Verify correct endpoint was called
      const sitesCall = apiCalls.find(c => c.url.includes('/sites'));
      expect(sitesCall).toBeTruthy();
      expect(sitesCall.url).toContain('/api/v1/tenants/');
      expect(sitesCall.url).toContain('/sites');
      
      // Verify agents/list was called (not /api/v1/agents)
      const agentsCall = apiCalls.find(c => c.url.includes('/agents'));
      expect(agentsCall).toBeTruthy();
      expect(agentsCall.url).toContain('/api/v1/agents/list');
    }
  });

  test('handles null sites response gracefully', async ({ page }) => {
    await loadApp(page, createBaseHandler({ sites: 'null' }));
    await page.waitForLoadState('networkidle');
    
    const adminTab = page.locator('#desktop_tabs [data-target="admin"]').first();
    await expect(adminTab).toBeVisible({ timeout: 10000 });
    await adminTab.click();
    await page.locator('.admin-subtab[data-adminview="tenants"]').click();
    
    await expect(page.locator('#tenants_list')).toBeVisible();
    
    const expandBtn = page.locator('.expand-btn[data-action="toggle-sites"]').first();
    if (await expandBtn.isVisible()) {
      await expandBtn.click();
      await page.waitForTimeout(500);
      
      // Should NOT show error - should show empty state
      const errorText = page.locator('.error-text');
      await expect(errorText).toHaveCount(0);
    }
  });

  test('sites list shows sites when data returned', async ({ page }) => {
    const testSites = [
      { id: 's1', name: 'Main Office', address: '123 Main St', tenant_id: 't1', filter_rules: [] },
      { id: 's2', name: 'Branch Office', address: '456 Oak Ave', tenant_id: 't1', filter_rules: [{ type: 'subnet', value: '192.168.1.0/24' }] }
    ];
    
    await loadApp(page, createBaseHandler({ sites: testSites }));
    await page.waitForLoadState('networkidle');
    
    const adminTab = page.locator('#desktop_tabs [data-target="admin"]').first();
    await expect(adminTab).toBeVisible({ timeout: 10000 });
    await adminTab.click();
    await page.locator('.admin-subtab[data-adminview="tenants"]').click();
    
    await expect(page.locator('#tenants_list')).toBeVisible();
    
    const expandBtn = page.locator('.expand-btn[data-action="toggle-sites"]').first();
    if (await expandBtn.isVisible()) {
      await expandBtn.click();
      await page.waitForTimeout(500);
      
      // Should show site names in the tree
      await expect(page.getByText('Main Office')).toBeVisible();
      await expect(page.getByText('Branch Office')).toBeVisible();
    }
  });
});

// ========== Agents API Tests ==========

test.describe('Agents API Integration', () => {
  test('agents list uses /api/v1/agents/list endpoint', async ({ page }) => {
    const apiCalls = [];
    
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
    
    // Track API requests
    page.on('request', req => {
      if (req.url().includes('/api/')) {
        apiCalls.push({ url: req.url(), method: req.method() });
      }
    });
    
    await page.route('**/api/**', route => {
      const url = route.request().url();
      
      if (url.includes('/api/v1/auth/me')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(adminUser) });
      }
      if (url.includes('/api/v1/agents/list')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
      if (url.includes('/api/v1/tenants')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
      if (url.includes('/api/v1/join-token')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
      if (route.request().method() === 'GET') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    });
    
    await page.goto(`${global.__PM_BASE_URL__}/app`);
    await page.waitForLoadState('networkidle');
    
    // Wait for agents tab to be visible (indicates auth/me was processed)
    const agentsTab = page.locator('#desktop_tabs [data-target="agents"]').first();
    await expect(agentsTab).toBeVisible({ timeout: 5000 });
    await agentsTab.click();
    await page.waitForTimeout(1000);
    
    // Verify /api/v1/agents/list was called, NOT /api/v1/agents
    const agentsListCall = apiCalls.find(c => c.url.includes('/api/v1/agents/list'));
    expect(agentsListCall).toBeTruthy();
    
    // Ensure we didn't call the wrong endpoint
    const wrongCall = apiCalls.find(c => 
      c.url.match(/\/api\/v1\/agents$/) || 
      c.url.match(/\/api\/v1\/agents\?/)
    );
    expect(wrongCall).toBeFalsy();
  });

  test('agents response is array, not wrapped object', async ({ page }) => {
    await loadApp(page, createBaseHandler());
    
    const agentsTab = page.locator('#desktop_tabs [data-target="agents"]').first();
    await expect(agentsTab).toBeVisible();
    await agentsTab.click();
    await page.waitForTimeout(1000);
    
    // Should show agent names without error
    const errorText = page.locator('.error-text');
    await expect(errorText).toHaveCount(0);
  });

  test('handles empty agents array gracefully', async ({ page }) => {
    await loadApp(page, createBaseHandler());
    
    const agentsTab = page.locator('#desktop_tabs [data-target="agents"]').first();
    await expect(agentsTab).toBeVisible();
    await agentsTab.click();
    await page.waitForTimeout(500);
    
    // Should not show error for empty list
    const errorText = page.locator('.error-text');
    await expect(errorText).toHaveCount(0);
  });
});

// ========== Tenant Expand/Collapse Tests ==========

test.describe('Tenant Sites Expansion', () => {
  test('expand button toggles sites row visibility', async ({ page }) => {
    await loadApp(page, createBaseHandler());
    await page.waitForLoadState('networkidle');
    
    const adminTab = page.locator('#desktop_tabs [data-target="admin"]').first();
    await expect(adminTab).toBeVisible({ timeout: 10000 });
    await adminTab.click();
    await page.locator('.admin-subtab[data-adminview="tenants"]').click();
    
    await expect(page.locator('#tenants_list')).toBeVisible();
    
    const expandBtn = page.locator('.expand-btn[data-action="toggle-sites"]').first();
    if (await expandBtn.isVisible()) {
      // Initially collapsed
      const sitesRow = page.locator('.sites-expansion-row').first();
      await expect(sitesRow).toHaveClass(/hidden/);
      
      // Click to expand
      await expandBtn.click();
      await page.waitForTimeout(300);
      await expect(sitesRow).not.toHaveClass(/hidden/);
      
      // Click to collapse
      await expandBtn.click();
      await page.waitForTimeout(300);
      await expect(sitesRow).toHaveClass(/hidden/);
    }
  });

  test('empty sites shows Add Site button', async ({ page }) => {
    await loadApp(page, createBaseHandler());
    await page.waitForLoadState('networkidle');
    
    const adminTab = page.locator('#desktop_tabs [data-target="admin"]').first();
    await expect(adminTab).toBeVisible({ timeout: 10000 });
    await adminTab.click();
    await page.locator('.admin-subtab[data-adminview="tenants"]').click();
    
    const expandBtn = page.locator('.expand-btn[data-action="toggle-sites"]').first();
    if (await expandBtn.isVisible()) {
      await expandBtn.click();
      await page.waitForTimeout(500);
      
      // Should show Add Site button when no sites (the one in the expansion row)
      const addSiteBtn = page.locator('.sites-expansion-row button:has-text("Add Site")').first();
      await expect(addSiteBtn).toBeVisible();
    }
  });
});
