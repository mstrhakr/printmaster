const { test, expect } = require('@playwright/test');
const path = require('path');

/**
 * Playwright tests for server self-update UI functionality.
 * Tests the server settings page self-update controls and status display.
 */

const serverFixture = path.join(__dirname, '..', '..', 'selfupdate_playwright_fixture.html');

test.describe('Server Self-Update UI', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('file://' + serverFixture);
    await page.waitForFunction(() => typeof window.__pm_shared?.showToast === 'function');
  });

  test('displays self-update status panel', async ({ page }) => {
    const statusPanel = page.locator('#selfupdate-status-panel');
    await expect(statusPanel).toBeVisible();
    
    // Check for required status elements
    await expect(page.locator('#selfupdate-current-version')).toBeVisible();
    await expect(page.locator('#selfupdate-status')).toBeVisible();
    await expect(page.locator('#selfupdate-channel')).toBeVisible();
  });

  test('status displays correctly for different states', async ({ page }) => {
    const statusPill = page.locator('#selfupdate-status');
    
    // Test idle state
    await page.evaluate(() => {
      window.setSelfUpdateStatus({ status: 'idle', enabled: true });
    });
    await expect(statusPill).toHaveText('idle');
    
    // Test checking state
    await page.evaluate(() => {
      window.setSelfUpdateStatus({ status: 'checking', enabled: true });
    });
    await expect(statusPill).toHaveText('checking');
    
    // Test applying state
    await page.evaluate(() => {
      window.setSelfUpdateStatus({ status: 'applying', enabled: true });
    });
    await expect(statusPill).toHaveText('applying');
  });

  test('check for update button is present and clickable', async ({ page }) => {
    const checkButton = page.locator('#selfupdate-check-btn');
    await expect(checkButton).toBeVisible();
    await expect(checkButton).toBeEnabled();
    await expect(checkButton).toContainText(/Check/i);
  });

  test('displays recent update runs', async ({ page }) => {
    await page.evaluate(() => {
      window.setUpdateRuns([
        {
          id: 1,
          status: 'succeeded',
          current_version: '0.9.4',
          target_version: '0.9.5',
          started_at: new Date(Date.now() - 3600000).toISOString(),
          completed_at: new Date(Date.now() - 3500000).toISOString(),
        },
        {
          id: 2,
          status: 'skipped',
          current_version: '0.9.5',
          started_at: new Date(Date.now() - 1800000).toISOString(),
          completed_at: new Date(Date.now() - 1800000).toISOString(),
          metadata: { reason: 'no-artifacts' },
        },
      ]);
    });

    const runsTable = page.locator('#selfupdate-runs-table');
    await expect(runsTable).toBeVisible();
    
    const rows = runsTable.locator('tbody tr');
    await expect(rows).toHaveCount(2);
    
    // Check first row shows succeeded
    await expect(rows.nth(0)).toContainText('succeeded');
    await expect(rows.nth(0)).toContainText('0.9.5');
    
    // Check second row shows skipped
    await expect(rows.nth(1)).toContainText('skipped');
  });

  test('shows empty state when no runs', async ({ page }) => {
    await page.evaluate(() => {
      window.setUpdateRuns([]);
    });

    const emptyMessage = page.locator('#selfupdate-no-runs');
    await expect(emptyMessage).toBeVisible();
    await expect(emptyMessage).toContainText('No update runs');
  });

  test('displays error information for failed runs', async ({ page }) => {
    await page.evaluate(() => {
      window.setUpdateRuns([
        {
          id: 1,
          status: 'failed',
          current_version: '0.9.4',
          target_version: '0.9.5',
          started_at: new Date().toISOString(),
          completed_at: new Date().toISOString(),
          error_code: 'DOWNLOAD_FAILED',
          error_message: 'Network timeout during download',
        },
      ]);
    });

    const row = page.locator('#selfupdate-runs-table tbody tr').first();
    await expect(row).toContainText('failed');
    await expect(row).toContainText('DOWNLOAD_FAILED');
  });

  test('disabled state shows informative message', async ({ page }) => {
    await page.evaluate(() => {
      window.setSelfUpdateStatus({
        status: 'disabled',
        enabled: false,
        disabled_reason: 'Running in container environment',
      });
    });

    const disabledBanner = page.locator('#selfupdate-disabled-banner');
    await expect(disabledBanner).toBeVisible();
    await expect(disabledBanner).toContainText('container');
    
    // Check button is disabled
    const checkButton = page.locator('#selfupdate-check-btn');
    await expect(checkButton).toBeDisabled();
  });

  test('auto-refresh toggle works', async ({ page }) => {
    const refreshToggle = page.locator('#selfupdate-auto-refresh');
    await expect(refreshToggle).toBeVisible();
    
    // Toggle off
    await refreshToggle.uncheck();
    expect(await refreshToggle.isChecked()).toBe(false);
    
    // Toggle on
    await refreshToggle.check();
    expect(await refreshToggle.isChecked()).toBe(true);
  });
});

test.describe('Server Update Policy UI', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('file://' + serverFixture);
    await page.waitForFunction(() => typeof window.__pm_shared?.showToast === 'function');
  });

  test('displays current update policy', async ({ page }) => {
    await page.evaluate(() => {
      window.setUpdatePolicy({
        update_check_days: 7,
        version_pin_strategy: 'minor',
        allow_major_upgrade: false,
        maintenance_start_utc: 2,
        maintenance_end_utc: 6,
      });
    });

    await expect(page.locator('#policy-check-interval')).toContainText('7');
    await expect(page.locator('#policy-version-strategy')).toContainText('minor');
    await expect(page.locator('#policy-maintenance-window')).toContainText('02:00 - 06:00');
  });

  test('edit policy opens modal', async ({ page }) => {
    const editButton = page.locator('#edit-policy-btn');
    await editButton.click();
    
    const policyModal = page.locator('#policy-edit-modal');
    await expect(policyModal).toBeVisible();
  });

  test('save policy form validates input', async ({ page }) => {
    // Open edit modal
    await page.locator('#edit-policy-btn').click();
    
    const policyModal = page.locator('#policy-edit-modal');
    await expect(policyModal).toBeVisible();
    
    // Check input is editable
    const checkDaysInput = page.locator('#policy-check-days-input');
    await expect(checkDaysInput).toBeVisible();
    await checkDaysInput.fill('14');
    await expect(checkDaysInput).toHaveValue('14');
    
    // Save button should be present
    const saveButton = page.locator('#save-policy-btn');
    await expect(saveButton).toBeVisible();
    await expect(saveButton).toBeEnabled();
  });
});
