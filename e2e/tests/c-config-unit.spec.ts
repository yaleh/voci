import { test, expect } from '@playwright/test';

/**
 * C-class config resolveConfig() unit tests (TASK-73).
 *
 * Backend-independent: /api/context and /api/config are page.route()-mocked.
 * Tests verify the resolution hierarchy:
 *   URL query param > localStorage > hardcoded default
 * with type validation and min/max clamping.
 */

const EMPTY_CONTEXT = { hint: '' };
const EMPTY_CONFIG = {};

test.describe('C-class resolveConfig() (mocked, TASK-73)', () => {
  test('URL query param ?contextPollMs=1000 overrides default 5000', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/?contextPollMs=1000');
    await page.waitForLoadState('networkidle');

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getCConfig());
    expect(cfg.contextPollMs).toBe(1000);
    // Verify other params are untouched (still at defaults).
    expect(cfg.statusHideMs).toBe(2000);
  });

  test('localStorage voci_c_contextPollMs=3000 overrides default 5000', async ({ page }) => {
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

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getCConfig());
    expect(cfg.contextPollMs).toBe(3000);
  });

  test('URL query param takes priority over localStorage', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('voci_c_contextPollMs', '3000');
    });

    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/?contextPollMs=7000');
    await page.waitForLoadState('networkidle');

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getCConfig());
    expect(cfg.contextPollMs).toBe(7000);
  });

  test('malformed value ?contextPollMs=abc falls back to default 5000', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/?contextPollMs=abc');
    await page.waitForLoadState('networkidle');

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getCConfig());
    expect(cfg.contextPollMs).toBe(5000);
  });

  test('out-of-range value ?contextPollMs=100 is clamped to min 500', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/?contextPollMs=100');
    // Poll interval is clamped to 500ms, which can race with networkidle detection.
    // Wait for the page to be ready and the config hook to be available.
    await page.waitForLoadState('domcontentloaded');
    await page.waitForFunction(() => (window as any).__voiceTest?.getCConfig != null, {}, { timeout: 5000 });

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getCConfig());
    expect(cfg.contextPollMs).toBe(500);
  });

  test('window.__voiceTest.getCConfig() returns all 7 resolved defaults', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
    );
    await page.route('/api/config', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONFIG) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    const cfg = await page.evaluate(() => (window as any).__voiceTest.getCConfig());
    // All 7 keys present
    const keys = Object.keys(cfg).sort();
    expect(keys).toEqual([
      'contextPollMs', 'entitySlice', 'localMsgCap',
      'postEmitDelayMs', 'statusHideMs', 'taskListSlice', 'taskPillSlice',
    ].sort());
    // Default values
    expect(cfg.contextPollMs).toBe(5000);
    expect(cfg.statusHideMs).toBe(2000);
    expect(cfg.entitySlice).toBe(6);
    expect(cfg.taskPillSlice).toBe(4);
    expect(cfg.taskListSlice).toBe(6);
    expect(cfg.localMsgCap).toBe(40);
    expect(cfg.postEmitDelayMs).toBe(600);
  });
});
