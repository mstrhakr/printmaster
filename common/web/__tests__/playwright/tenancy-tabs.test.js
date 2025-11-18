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
      if (staticRoutes[url.pathname]) {
        const entry = staticRoutes[url.pathname];
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

async function mockApi(page, user) {
  await page.route('**/api/**', route => {
    const url = route.request().url();
    if (url.includes('/api/v1/auth/me')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(user) });
    }
    if (url.includes('/api/v1/tenants')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([{ id: 't1', name: 'Acme Corp' }]) });
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
  });
}

async function loadApp(page, user) {
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
  await mockApi(page, user);
  await page.goto(`${global.__PM_BASE_URL__}/app`);
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
const operatorUser = { username: 'oliver', role: 'operator', tenant_ids: ['t1'] };
const viewerUser = { username: 'victor', role: 'viewer', tenant_ids: ['t1'] };

test('admin sees tenants tab and actions', async ({ page }) => {
  await loadApp(page, adminUser);
  const tenantsTab = page.locator('#desktop_tabs [data-target="tenants"]').first();
  await expect(tenantsTab).toBeVisible();
  await tenantsTab.click();
  await expect(page.locator('#new_tenant_btn')).toBeVisible();
});

test('operator cannot see tenants tab', async ({ page }) => {
  await loadApp(page, operatorUser);
  await expect(page.locator('[data-target="tenants"]')).toHaveCount(0);
});

test('viewer cannot see add agent button', async ({ page }) => {
  await loadApp(page, viewerUser);
  await expect(page.locator('#join_token_btn')).toBeHidden();
});
