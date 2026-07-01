import { test, expect } from '@playwright/test';

/**
 * Frontend unit tests for the /api/config fetch-and-override logic.
 *
 * Backend-independent: both /api/context and /api/config are page.route()-mocked,
 * so this isolates the JS var-override contract without needing the real Go server.
 */

const EMPTY_CONTEXT = { hint: '' };

test.describe('/api/config frontend override (mocked, TASK config management)', () => {
  test('mocked /api/config values override the local VAD defaults', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vadThreshold: 0.2, minAudioMs: 750 }) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.waitForFunction(() => {
      const cfg = (window as any).__voiceTest?.getVadConfig?.();
      return cfg && cfg.vadThreshold === 0.2;
    }, { timeout: 5000 });

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getVadConfig());
    expect(cfg.vadThreshold).toBe(0.2);
    expect(cfg.minAudioMs).toBe(750);
  });

  test('a failed /api/config fetch leaves the hardcoded fallback defaults intact', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route => route.abort());

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getVadConfig());
    expect(cfg.vadThreshold).toBe(0.01);
    expect(cfg.minAudioMs).toBe(300);
  });
});
