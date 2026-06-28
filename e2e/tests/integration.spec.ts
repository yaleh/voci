import { test, expect } from '@playwright/test';

/**
 * Phase 4: Frontend-backend integration tests against the real Go httptest server.
 * Uses BASE_URL set by globalSetup.ts. No network mocking.
 */

test.describe('GET / returns index.html', () => {
  test('page loads with title and h1 containing voci', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/voci/i);
    await expect(page.locator('h1')).toContainText('voci');
  });
});

test.describe('GET /recorder.js returns script', () => {
  test('recorder.js is served and contains renderDialogue', async ({ page }) => {
    const baseURL = process.env.BASE_URL || page.url().replace(/\/[^/]*$/, '');
    const response = await page.request.get('/recorder.js');
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain('renderDialogue');
  });
});

test.describe('Page load triggers /api/context', () => {
  test('after page load, ctx-known-body no longer shows loading', async ({ page }) => {
    await page.goto('/');
    // Wait for network to settle (context fetch completes)
    await page.waitForLoadState('networkidle');
    // ctx-known-body should not display the initial loading placeholder
    const knownText = await page.locator('#ctx-known-body').textContent();
    expect(knownText).not.toContain('(loading…)');
    expect(knownText).not.toContain('(loading');
  });
});

test.describe('/api/context JSON structure', () => {
  test('returns JSON with a hint field of type string', async ({ page }) => {
    await page.goto('/');
    const result = await page.evaluate(async () => {
      const r = await fetch('/api/context');
      const data = await r.json();
      return { status: r.status, hasHint: 'hint' in data, hintType: typeof data.hint };
    });
    expect(result.status).toBe(200);
    expect(result.hasHint).toBe(true);
    expect(result.hintType).toBe('string');
  });
});
