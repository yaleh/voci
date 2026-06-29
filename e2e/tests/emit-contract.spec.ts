import { test, expect } from '@playwright/test';

/**
 * Emit endpoint contract tests — regression for the Monitor silence bug.
 *
 * Root cause: voci-listen skill used grep pattern '"Rewritten"' (uppercase R),
 * but Event JSON key is "rewritten" (lowercase), so the Monitor never fired.
 *
 * These tests verify the full round-trip from the UI send action through to
 * the /api/voice/emit payload structure.
 */

test.describe('POST /api/voice/emit — endpoint contract', () => {
  test('returns 204 for valid text+kind JSON body', async ({ page }) => {
    await page.goto('/');
    const status = await page.evaluate(async () => {
      const r = await fetch('/api/voice/emit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: 'pwd', kind: 'direct_prompt' }),
      });
      return r.status;
    });
    expect(status).toBe(204);
  });

  test('returns 400 for missing text field', async ({ page }) => {
    await page.goto('/');
    const status = await page.evaluate(async () => {
      const r = await fetch('/api/voice/emit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind: 'direct_prompt' }),
      });
      return r.status;
    });
    expect(status).toBe(400);
  });

  test('returns 405 for GET request', async ({ page }) => {
    await page.goto('/');
    const status = await page.evaluate(async () => {
      const r = await fetch('/api/voice/emit', { method: 'GET' });
      return r.status;
    });
    expect(status).toBe(405);
  });
});

test.describe('Send → dialogue immediately updated (no hint-change required)', () => {
  test('sent message appears in dialogue feed without waiting for hint change', async ({ page }) => {
    // Return the SAME hint on every /api/context call — simulates a static context.
    // Even so, the sent message must appear in the dialogue immediately.
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) })
    );
    await page.route('/api/voice/emit', route => route.fulfill({ status: 204 }));

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.fill('#voci-compose', 'list files');
    await page.click('#send-btn');

    // Dialogue must show the sent text without needing a hint change
    await expect(page.locator('#voci-dialogue')).toContainText('list files', { timeout: 3000 });
  });
});

test.describe('Send button → /api/voice/emit payload', () => {
  test('typing text and clicking Send calls emit with correct text field', async ({ page }) => {
    let emitBody: any = null;

    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) })
    );
    await page.route('/api/voice/emit', async route => {
      emitBody = JSON.parse((await route.request().postData()) || '{}');
      route.fulfill({ status: 204 });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.fill('#voci-compose', 'pwd');
    await page.click('#send-btn');
    await page.waitForTimeout(400);

    expect(emitBody).not.toBeNull();
    expect(emitBody.text).toBe('pwd');
    // kind must be a string (used by the Monitor event handler)
    expect(typeof emitBody.kind).toBe('string');
  });

  test('Enter key in textarea calls emit and clears compose field', async ({ page }) => {
    let emitBody: any = null;

    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) })
    );
    await page.route('/api/voice/emit', async route => {
      emitBody = JSON.parse((await route.request().postData()) || '{}');
      route.fulfill({ status: 204 });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.fill('#voci-compose', 'ls -la');
    await page.press('#voci-compose', 'Enter');
    await page.waitForTimeout(400);

    expect(emitBody).not.toBeNull();
    expect(emitBody.text).toBe('ls -la');
    // Compose field should be cleared after successful send
    await expect(page.locator('#voci-compose')).toHaveValue('');
  });
});
