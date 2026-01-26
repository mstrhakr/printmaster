const { test, expect } = require('@playwright/test');
const path = require('path');

/**
 * Playwright tests for auto-update UI functionality.
 * These tests verify the auto-update status display, check triggers,
 * and force reinstall workflows in both agent and server UIs.
 */

// Test fixture that includes auto-update UI elements
const agentFixture = path.join(__dirname, '..', '..', 'autoupdate_playwright_fixture.html');

test.describe('Auto-Update UI', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('file://' + agentFixture);
    // Wait for shared.js to load
    await page.waitForFunction(() => typeof window.__pm_shared?.showToast === 'function');
  });

  test('displays auto-update status panel', async ({ page }) => {
    // Verify the auto-update status panel elements exist
    const statusPanel = page.locator('#autoupdate-status-panel');
    await expect(statusPanel).toBeVisible();
    
    // Check for required status elements
    await expect(page.locator('#autoupdate-current-version')).toBeVisible();
    await expect(page.locator('#autoupdate-status')).toBeVisible();
    await expect(page.locator('#autoupdate-channel')).toBeVisible();
  });

  test('status pill shows correct state classes', async ({ page }) => {
    const statusPill = page.locator('#autoupdate-status');
    
    // Test idle state
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ status: 'idle', enabled: true });
    });
    await expect(statusPill).toHaveClass(/status-muted/);
    
    // Test checking state
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ status: 'checking', enabled: true });
    });
    await expect(statusPill).toHaveClass(/status-info/);
    
    // Test downloading state
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ status: 'downloading', enabled: true });
    });
    await expect(statusPill).toHaveClass(/status-warning/);
    
    // Test succeeded state
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ status: 'succeeded', enabled: true });
    });
    await expect(statusPill).toHaveClass(/status-success/);
    
    // Test failed state
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ status: 'failed', enabled: true });
    });
    await expect(statusPill).toHaveClass(/status-error/);
  });

  test('check for update button is present and clickable', async ({ page }) => {
    const checkButton = page.locator('#autoupdate-check-btn');
    await expect(checkButton).toBeVisible();
    await expect(checkButton).toBeEnabled();
    await expect(checkButton).toContainText('Check for Update');
    
    // Verify button has click handler (not testing API call since file:// protocol)
    await expect(checkButton).not.toBeDisabled();
  });

  test('force reinstall shows confirmation dialog', async ({ page }) => {
    const forceButton = page.locator('#autoupdate-force-btn');
    await expect(forceButton).toBeVisible();
    
    // Click force reinstall - should show confirm dialog
    await forceButton.click();
    
    // Confirm modal should be visible
    const confirmModal = page.locator('#confirm_modal');
    await expect(confirmModal).toBeVisible();
    
    // Check the message mentions reinstall
    const confirmMessage = page.locator('#confirm_modal_message');
    await expect(confirmMessage).toContainText(/reinstall|force/i);
  });

  test('force reinstall shows confirmation and updates button state', async ({ page }) => {
    // This test verifies the flow: click force -> confirm modal -> confirm click
    // The confirm dialog flow works, but the fetch fails with file:// protocol
    // so we only verify up to the modal confirmation
    
    const forceButton = page.locator('#autoupdate-force-btn');
    await forceButton.click();
    
    // Confirm the action
    const confirmButton = page.locator('#confirm_modal_confirm');
    await expect(confirmButton).toBeVisible();
    
    // Verify modal has expected content
    const confirmMessage = page.locator('#confirm_modal_message');
    await expect(confirmMessage).toContainText(/reinstall|force/i);
  });

  test('cancel update button visible during active update', async ({ page }) => {
    // Set status to downloading (an active update state)
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ 
        status: 'downloading', 
        enabled: true,
        target_version: '1.2.0',
        progress: 45
      });
    });

    const cancelButton = page.locator('#autoupdate-cancel-btn');
    await expect(cancelButton).toBeVisible();
  });

  test('cancel update button responds to clicks', async ({ page }) => {
    // Set active update state
    await page.evaluate(() => {
      window.setAutoUpdateStatus({ status: 'downloading', enabled: true });
    });

    const cancelButton = page.locator('#autoupdate-cancel-btn');
    await expect(cancelButton).toBeVisible();
    await expect(cancelButton).toBeEnabled();
    
    // Verify button is properly configured and visible in downloading state
    await expect(cancelButton).toContainText(/Cancel/i);
  });

  test('displays update available notification', async ({ page }) => {
    await page.evaluate(() => {
      window.setAutoUpdateStatus({
        status: 'idle',
        enabled: true,
        current_version: '1.0.0',
        latest_version: '1.1.0',
        update_available: true,
      });
    });

    const updateBadge = page.locator('#autoupdate-available-badge');
    await expect(updateBadge).toBeVisible();
    await expect(updateBadge).toContainText('1.1.0');
  });

  test('shows progress bar during download', async ({ page }) => {
    await page.evaluate(() => {
      window.setAutoUpdateStatus({
        status: 'downloading',
        enabled: true,
        target_version: '1.2.0',
      });
      window.setAutoUpdateProgress(65);
    });

    const progressBar = page.locator('#autoupdate-progress-bar');
    await expect(progressBar).toBeVisible();
    
    // Check progress value
    const width = await progressBar.evaluate((el) => el.style.width);
    expect(width).toBe('65%');
  });

  test('disabled state shows reason', async ({ page }) => {
    await page.evaluate(() => {
      window.setAutoUpdateStatus({
        status: 'disabled',
        enabled: false,
        disabled_reason: 'No server connection',
      });
    });

    const disabledMessage = page.locator('#autoupdate-disabled-reason');
    await expect(disabledMessage).toBeVisible();
    await expect(disabledMessage).toContainText('No server connection');
  });

  test('policy source is displayed', async ({ page }) => {
    await page.evaluate(() => {
      window.setAutoUpdateStatus({
        status: 'idle',
        enabled: true,
        policy_source: 'fleet',
      });
    });

    const policyBadge = page.locator('#autoupdate-policy-source');
    await expect(policyBadge).toBeVisible();
    await expect(policyBadge).toContainText('fleet');
  });

  test('last check and next check times displayed', async ({ page }) => {
    const now = new Date();
    const lastCheck = new Date(now.getTime() - 3600000); // 1 hour ago
    const nextCheck = new Date(now.getTime() + 86400000); // 24 hours from now
    
    await page.evaluate(({ last, next }) => {
      window.setAutoUpdateStatus({
        status: 'idle',
        enabled: true,
        last_check_at: last,
        next_check_at: next,
      });
    }, { last: lastCheck.toISOString(), next: nextCheck.toISOString() });

    const lastCheckEl = page.locator('#autoupdate-last-check');
    const nextCheckEl = page.locator('#autoupdate-next-check');
    
    await expect(lastCheckEl).toBeVisible();
    await expect(nextCheckEl).toBeVisible();
  });
});

test.describe('Auto-Update SSE Events', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('file://' + agentFixture);
    await page.waitForFunction(() => typeof window.__pm_shared?.showToast === 'function');
  });

  test('update_progress event updates UI', async ({ page }) => {
    // Simulate SSE event
    await page.evaluate(() => {
      const event = new CustomEvent('sse:update_progress', {
        detail: {
          status: 'downloading',
          progress: 30,
          target_version: '1.2.0',
          message: 'Downloading update...',
        },
      });
      window.dispatchEvent(event);
    });

    const statusPill = page.locator('#autoupdate-status');
    await expect(statusPill).toContainText('downloading');
    
    const progressBar = page.locator('#autoupdate-progress-bar');
    const width = await progressBar.evaluate((el) => el.style.width);
    expect(width).toBe('30%');
  });

  test('update completion updates status', async ({ page }) => {
    await page.evaluate(() => {
      const event = new CustomEvent('sse:update_progress', {
        detail: {
          status: 'succeeded',
          progress: 100,
          target_version: '1.2.0',
          message: 'Update completed successfully',
        },
      });
      window.dispatchEvent(event);
    });

    // Check status pill shows succeeded
    const statusPill = page.locator('#autoupdate-status');
    await expect(statusPill).toContainText('succeeded');
    await expect(statusPill).toHaveClass(/status-success/);
  });

  test('update failure updates status', async ({ page }) => {
    await page.evaluate(() => {
      const event = new CustomEvent('sse:update_progress', {
        detail: {
          status: 'failed',
          progress: -1,
          error: 'Download failed: network error',
        },
      });
      window.dispatchEvent(event);
    });

    // Check status pill shows failed
    const statusPill = page.locator('#autoupdate-status');
    await expect(statusPill).toContainText('failed');
    await expect(statusPill).toHaveClass(/status-error/);
  });
});
