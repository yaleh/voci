import { test, expect } from '@playwright/test';

/**
 * C-class settings panel UI tests (TASK-73).
 *
 * Backend-independent: /api/context, /api/config, and /api/voice/emit are
 * page.route()-mocked. Tests verify settings panel visibility, display of
 * effective values, localStorage persistence, and reset behaviour.
 */

const EMPTY_CONTEXT = { hint: '' };
const EMPTY_CONFIG = {};

test.describe('C-class settings panel (mocked, TASK-73)', () => {
  test('settings panel is hidden by default', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    const panel = page.locator('#voci-csettings');
    await expect(panel).toBeHidden();
  });

  test('settings panel revealed by keyboard shortcut ?', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.keyboard.press('?');
    await expect(page.locator('#voci-csettings')).toBeVisible();
  });

  test('settings panel can be closed with Escape key', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.keyboard.press('?');
    await expect(page.locator('#voci-csettings')).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(page.locator('#voci-csettings')).toBeHidden();
  });

  test('settings panel shows current effective value of contextPollMs', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('voci_c_contextPollMs', '3000');
    });

    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.keyboard.press('?');

    // The input for contextPollMs should show the effective value 3000 (from localStorage).
    await expect(page.locator('#voci-csettings input[name="contextPollMs"]')).toHaveValue('3000');
  });

  test('saving a value writes to localStorage under voci_c_ prefix', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.keyboard.press('?');

    // Change contextPollMs and save.
    await page.fill('#voci-csettings input[name="contextPollMs"]', '8000');
    await page.click('#voci-csettings-save');

    const stored = await page.evaluate(() => localStorage.getItem('voci_c_contextPollMs'));
    expect(stored).toBe('8000');
  });

  test('resetting removes localStorage key and shows hardcoded default', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('voci_c_contextPollMs', '3000');
    });

    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.keyboard.press('?');

    // Reset all C-class settings.
    await page.click('#voci-csettings-reset');

    // localStorage key should be removed.
    const stored = await page.evaluate(() => localStorage.getItem('voci_c_contextPollMs'));
    expect(stored).toBeNull();

    // The input should show the hardcoded default (5000).
    await expect(page.locator('#voci-csettings input[name="contextPollMs"]')).toHaveValue('5000');
  });
});
