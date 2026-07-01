import { test, expect } from '@playwright/test';

/**
 * Phase 4: Frontend-backend integration tests against the real Go httptest server.
 * Uses BASE_URL set by globalSetup.ts. No network mocking.
 */

test.describe('GET / returns index.html', () => {
  test('page loads with title containing voci', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/voci/i);
  });
});

test.describe('GET /recorder.bundle.js returns script', () => {
  test('recorder.bundle.js is served and contains renderDialogue', async ({ page }) => {
    const response = await page.request.get('/recorder.bundle.js');
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain('renderDialogue');
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

test.describe('Page load renders dialogue container', () => {
  test('voci-dialogue element is present after load', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await expect(page.locator('#voci-dialogue')).toBeAttached();
  });
});
