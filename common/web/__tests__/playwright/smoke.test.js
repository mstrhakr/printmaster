const { test, expect } = require('@playwright/test');
const path = require('path');

// Serve local files via file:// protocol
const fixture = path.join(__dirname, '..', '..', 'shared_playwright_fixture.html');

test('shared.js toast and confirm smoke', async ({ page }) => {
  await page.goto('file://' + fixture);

  // ensure shared loaded
  await page.waitForFunction(() => window.__pm_shared && typeof window.__pm_shared.showToast === 'function');

  // trigger toast
  await page.evaluate(() => window.__pm_shared.showToast('Playwright Test', 'success', 500));
  // toast should appear
  const toast = await page.locator('.toast');
  await expect(toast).toHaveCount(1);
  await expect(toast.locator('.toast-message')).toHaveText('Playwright Test');

  // test confirm modal: create modal elements present in fixture
  // Programmatically invoke showConfirm and resolve by clicking confirm button
  const promise = page.evaluate(() => {
    return window.__pm_shared.showConfirm('Confirm from Playwright?', 'Please Confirm', false).then(v => window.__pm_shared._lastConfirmResult = v);
  });

  // Wait briefly for modal to show and click confirm
  await page.waitForSelector('#confirm_modal', { state: 'visible' });
  await page.click('#confirm_modal_confirm');

  // wait for promise to settle and check result
  await page.waitForFunction(() => window.__pm_shared && window.__pm_shared._lastConfirmResult === true);
  const result = await page.evaluate(() => window.__pm_shared._lastConfirmResult);
  expect(result).toBe(true);
});
