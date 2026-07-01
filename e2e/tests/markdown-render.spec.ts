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

// The structured /api/context contract: a `dialogue` array of {role, text},
// where text is complete Markdown (JSON-encoded, so newlines and the blank line
// before the table survive byte-for-byte). This is the exact shape Go's
// handleContext + SessionSource.Dialogue produce.
const CONTEXT_WITH_TABLE = {
  hint: '',
  dialogue: [
    { role: 'assistant', text: '总结：\n\n| 列A | 列B |\n|---|---|\n| 值1 | 值2 |' },
  ],
};

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

  test('GFM table from structured dialogue field renders as <table>', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_WITH_TABLE) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Wait for renderContext to consume the dialogue field and render the table.
    await page.waitForFunction(() => {
      const dlg = document.getElementById('voci-dialogue');
      return dlg && dlg.querySelector('table') !== null;
    }, { timeout: 5000 });

    await expect(page.locator('#voci-dialogue table')).toHaveCount(1);
    await expect(page.locator('#voci-dialogue th').first()).toContainText('列A');
    // The full table (3 body cells + 2 headers) must be present — proving the
    // blank line before the table survived and marked parsed it as a block.
    await expect(page.locator('#voci-dialogue td')).toHaveCount(2);
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
