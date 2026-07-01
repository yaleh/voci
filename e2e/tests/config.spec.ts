import { test, expect } from '@playwright/test';

/**
 * /api/config real-chain tests (D-class VAD tuning values).
 *
 * The shared Go server (internal/daemon/playwright_setup_test.go) is built with
 * non-default VADThreshold=0.05 / MinAudioMs=500. These tests hit the real server
 * over real HTTP and confirm the browser picks up the real values, not the
 * hardcoded frontend fallback defaults (0.01 / 300).
 */

test.describe('/api/config (real-chain, TASK config management)', () => {
  test('GET /api/config returns the real non-default VAD values', async ({ page, baseURL }) => {
    const resp = await page.request.get(`${baseURL}/api/config`);
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.vadThreshold).toBeCloseTo(0.05);
    expect(body.minAudioMs).toBe(500);
  });

  test('page load fetches /api/config and overrides the frontend VAD defaults', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.waitForFunction(() => {
      const cfg = (window as any).__voiceTest?.getVadConfig?.();
      return cfg && cfg.vadThreshold === 0.05;
    }, { timeout: 5000 });

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getVadConfig());
    expect(cfg.vadThreshold).toBeCloseTo(0.05);
    expect(cfg.minAudioMs).toBe(500);
  });
});
