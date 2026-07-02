import { test, expect } from '@playwright/test';

/**
 * Activity stream tests (TASK-77).
 *
 * These tests verify that:
 *   - Tool call events render .voci-activity-row in the activity feed
 *   - A no-session (heartbeat/idle only) connection does not crash the page
 */

const CONTEXT_MOCK = { hint: '' };

test.describe('Activity stream (TASK-77)', () => {
  test('tool_call event renders .voci-activity-row', async ({ page }) => {
    // Mock /api/context to return empty hint.
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
    );

    // Mock /api/activity to return a tool_call SSE event followed by idle heartbeat.
    await page.route('/api/activity', route =>
      route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: 'event: tool_call\ndata: Read\n\nevent: idle\ndata: {}\n\n',
      })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Trigger the activity stream connection immediately (normally deferred by 2s).
    await page.evaluate(() => {
      (window as any).__voiceTest.connectActivityStream();
    });

    // Wait for the activity feed to contain a .voci-activity-row.
    await page.waitForSelector('.voci-activity-row', { timeout: 5000 });

    const rows = page.locator('.voci-activity-row');
    await expect(rows).not.toHaveCount(0);
  });

  test('no_session does not crash page', async ({ page }) => {
    // Mock /api/context to return empty hint.
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
    );

    // Mock /api/activity to return idle heartbeat only (simulating no session).
    await page.route('/api/activity', route =>
      route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: 'event: idle\ndata: {}\n\n',
      })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Trigger the activity stream connection immediately.
    await page.evaluate(() => {
      (window as any).__voiceTest.connectActivityStream();
    });

    await page.waitForTimeout(500);

    // Verify the page is still functional: the dialogue feed and entities toggle exist.
    await expect(page.locator('#voci-dialogue')).toBeVisible();
    await expect(page.locator('#entities-toggle')).toBeVisible();

    // The page should not have crashed — the voci-dialogue element must still be present.
    await expect(page.locator('#voci-dialogue')).toBeVisible();
  });
});
