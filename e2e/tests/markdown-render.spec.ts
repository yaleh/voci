import { test, expect } from '@playwright/test';

/**
 * Markdown rendering tests (TASK-72).
 *
 * These tests verify that:
 *   - GFM tables are rendered correctly via marked.js
 *   - XSS is prevented via DOMPurify sanitization
 *
 * Messages are injected via window.__voiceTest.injectMessages(),
 * which calls renderDialogue() directly with synthetic messages.
 */

const CONTEXT_MOCK = { hint: '' };

test.describe('Markdown rendering (TASK-72)', () => {
  test('GFM table rendering — injectMessage renders a GFM table', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    const now = new Date();
    const time = String(now.getHours()).padStart(2, '0') + ':' + String(now.getMinutes()).padStart(2, '0');

    await page.evaluate((t) => {
      (window as any).__voiceTest.injectMessages([
        {
          role: 'assistant',
          text: '| col1 | col2 |\n|------|------|\n| a    | b    |',
          time: t,
        },
      ]);
    }, time);

    await expect(page.locator('#voci-dialogue table')).toHaveCount(1);
  });

  test('XSS prevention — script tags are sanitized', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    const now = new Date();
    const time = String(now.getHours()).padStart(2, '0') + ':' + String(now.getMinutes()).padStart(2, '0');

    await page.evaluate((t) => {
      (window as any).__voiceTest.injectMessages([
        {
          role: 'assistant',
          text: '<script>window.__xss=1</script>hello',
          time: t,
        },
      ]);
    }, time);

    // No <script> tags should be present in the rendered output.
    await expect(page.locator('#voci-dialogue script')).toHaveCount(0);

    // window.__xss must remain undefined (the script must not have executed).
    const xssSet = await page.evaluate(() => (window as any).__xss);
    expect(xssSet).toBeUndefined();
  });
});
