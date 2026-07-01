import { test, expect, devices } from '@playwright/test';

/**
 * Mobile viewport tests.
 *
 * Two scenarios:
 *   - Token overlay visible on mobile (iPhone 14)
 *   - PTT button visible in mobile viewport (iPhone 14)
 */

test.use({ ...devices['iPhone 14'] });

test.describe('Mobile viewport — iPhone 14', () => {
  test('token overlay visible on mobile', async ({ page }) => {
    await page.route('/api/context', route => route.fulfill({ status: 401 }));
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await expect(page.locator('#voci-token-setup')).toBeVisible();
  });

  test('PTT button visible in mobile viewport', async ({ page }) => {
    await page.route('/api/context', route => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ hint: '' }),
    }));
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    const micBtn = page.locator('#mic-btn');
    await expect(micBtn).toBeVisible();

    // Verify it's within the viewport (not scrolled off-screen)
    const box = await micBtn.boundingBox();
    expect(box).not.toBeNull();
    if (box) {
      expect(box.y).toBeGreaterThanOrEqual(0);
    }
  });
});
