const { test, expect } = require('@playwright/test');
const http = require('http');
const fs = require('fs');
const path = require('path');

const loginHtmlPath = path.resolve(__dirname, '../../../../server/web/login.html');
const loginJsPath = path.resolve(__dirname, '../../../../server/web/login.js');
const loginCssPath = path.resolve(__dirname, '../../../../server/web/style.css');
const sharedCssPath = path.resolve(__dirname, '../../shared.css');
const sharedJsPath = path.resolve(__dirname, '../../shared.js');

let server;
let baseURL;

function serveFile(res, filePath, contentType) {
  const payload = fs.readFileSync(filePath);
  res.writeHead(200, { 'Content-Type': contentType });
  res.end(payload);
}

async function waitForLocalLoginSection(page, timeout = 5000) {
  const section = page.locator('#local_login_section');
  await expect(section).toBeVisible({ timeout });
  return section;
}

function waitForAuthOptions(page) {
  return page.waitForResponse(resp => resp.url().includes('/api/v1/auth/options') && resp.status() === 200);
}

function startFixtureServer() {
  return http.createServer((req, res) => {
    try {
      const url = new URL(req.url, 'http://localhost');
      if (url.pathname === '/login') {
        return serveFile(res, loginHtmlPath, 'text/html');
      }
      if (url.pathname === '/' || url.pathname === '/app') {
        res.writeHead(200, { 'Content-Type': 'text/html' });
        res.end('<html><body>OK</body></html>');
        return;
      }
      if (url.pathname === '/static/shared.css') {
        return serveFile(res, sharedCssPath, 'text/css');
      }
      if (url.pathname === '/static/shared.js') {
        return serveFile(res, sharedJsPath, 'application/javascript');
      }
      if (url.pathname === '/static/style.css') {
        return serveFile(res, loginCssPath, 'text/css');
      }
      if (url.pathname === '/static/login.js') {
        return serveFile(res, loginJsPath, 'application/javascript');
      }

      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('not found');
    } catch (err) {
      console.error('fixture server error', err);
      if (!res.headersSent) {
        res.writeHead(500, { 'Content-Type': 'text/plain' });
      }
      if (!res.writableEnded) {
        res.end('fixture server error: ' + err.message);
      }
    }
  });
}

test.beforeAll(async () => {
  server = startFixtureServer();
  await new Promise(resolve => server.listen(0, resolve));
  const address = server.address();
  baseURL = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
  if (!server) return;
  await new Promise(resolve => server.close(resolve));
});

test('renders SSO provider button and starts OIDC flow', async ({ page }) => {
  await page.route('**/api/v1/auth/options**', route => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        local_login: false,
        providers: [
          { slug: 'entra', display_name: 'Entra', button_text: 'Sign in with Entra', auto_login: false }
        ],
      }),
    });
  });
  await page.route('**/auth/oidc/start/**', route => {
    route.fulfill({ status: 200, body: 'redirected' });
  });

  const authOptionsLoaded = waitForAuthOptions(page);
  await page.goto(`${baseURL}/login?tenant=acme`);
  await authOptionsLoaded;
  const ssoButton = page.getByRole('button', { name: 'Sign in with Entra' });
  await expect(ssoButton).toBeVisible();

  const startRequest = page.waitForRequest('**/auth/oidc/start/**');
  await ssoButton.click();
  const request = await startRequest;
  expect(request.url()).toContain('/auth/oidc/start/entra');
  expect(request.url()).toContain('tenant=acme');
});

test('auto-login provider redirects without interaction', async ({ page }) => {
  await page.route('**/api/v1/auth/options**', route => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        local_login: false,
        providers: [
          { slug: 'auto', display_name: 'Auto', auto_login: true }
        ],
      }),
    });
  });
  await page.route('**/auth/oidc/start/**', route => route.fulfill({ status: 200, body: 'auto' }));

  const startRequest = page.waitForRequest('**/auth/oidc/start/**');
  await page.goto(`${baseURL}/login`);
  const request = await startRequest;
  expect(request.url()).toContain('/auth/oidc/start/auto');
});

test('local login form submits and navigates to redirect target', async ({ page }) => {
  await page.route('**/api/v1/auth/options**', route => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ local_login: true, providers: [] }),
    });
  });
  await page.route('**/api/v1/auth/login', route => {
    route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });

  const authOptionsLoaded = waitForAuthOptions(page);
  await page.goto(`${baseURL}/login?redirect=%2Fapp`);
  await authOptionsLoaded;
  await waitForLocalLoginSection(page);
  const usernameInput = page.locator('#login_username');
  const passwordInput = page.locator('#login_password');
  await usernameInput.fill('admin', { timeout: 5000 });
  await passwordInput.fill('secret', { timeout: 5000 });

  const navigation = page.waitForURL('**/app');
  await page.click('#login_submit');
  await navigation;
  await expect(page).toHaveURL(/\/app$/);
});
