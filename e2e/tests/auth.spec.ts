import { test, expect } from '@playwright/test';

/**
 * Auth overlay tests — Fix A.
 *
 * Two modes:
 *   - 401 mode  : server has BearerToken set (--share); unauthenticated probe returns 401.
 *                 Overlay must appear, be fully opaque, and block all context data.
 *   - local mode: server has no BearerToken; probe returns 200.
 *                 Overlay must NOT appear.
 */

test.describe('Auth overlay — 401 mode (server requires Bearer token)', () => {
  test('overlay is shown when /api/context probe returns 401', async ({ page }) => {
    await page.route('/api/context', route => route.fulfill({ status: 401 }));
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await expect(page.locator('#voci-token-setup')).toBeVisible();
  });

  test('overlay background is fully opaque — no content bleed-through', async ({ page }) => {
    await page.route('/api/context', route => route.fulfill({ status: 401 }));
    await page.goto('/');
    await expect(page.locator('#voci-token-setup')).toBeVisible();

    const bgColor = await page.locator('#voci-token-setup').evaluate(
      (el: HTMLElement) => window.getComputedStyle(el).backgroundColor
    );
    // Semi-transparent (e.g. rgba(0,0,0,0.7)) would expose context data behind the overlay.
    // getComputedStyle returns "rgba(r, g, b, a)" when alpha < 1, else "rgb(r, g, b)".
    expect(bgColor).not.toMatch(/rgba\([^)]+,\s*0\.[0-9]+\)/);
  });

  test('context data is NOT rendered when auth required and no token stored', async ({ page }) => {
    await page.route('/api/context', route => route.fulfill({ status: 401 }));
    // Clear any stored token
    await page.addInitScript(() => localStorage.removeItem('voci_token'));
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Dialogue feed must not contain any user/assistant messages
    const dialogueHtml = await page.locator('#voci-dialogue').innerHTML();
    expect(dialogueHtml).not.toContain('role="user"');
    // The "you" label used for user turns must not appear
    expect(dialogueHtml).not.toMatch(/>\s*you\s*</);
  });

  test('overlay dismissed and context loads after saveToken()', async ({ page }) => {
    let callCount = 0;
    await page.route('/api/context', route => {
      if (callCount++ === 0) {
        route.fulfill({ status: 401 });
      } else {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ hint: '' }),
        });
      }
    });
    await page.addInitScript(() => localStorage.removeItem('voci_token'));
    await page.goto('/');

    const overlay = page.locator('#voci-token-setup');
    await expect(overlay).toBeVisible();

    await page.fill('#voci-token', 'test-bearer-token-abc');
    await page.click('button[onclick="saveToken()"]');

    await expect(overlay).toBeHidden();
  });
});

test.describe('Auth overlay — local mode (server returns 200, no token required)', () => {
  test('overlay NOT shown when /api/context returns 200', async ({ page }) => {
    // Ensure no token in localStorage — local mode should work without one
    await page.addInitScript(() => localStorage.removeItem('voci_token'));
    // Default server from globalSetup returns 200; no route override needed
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await expect(page.locator('#voci-token-setup')).not.toBeVisible();
  });
});
