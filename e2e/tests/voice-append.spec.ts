import { test, expect } from '@playwright/test';

/**
 * Voice append mode tests (TASK-60).
 *
 * These tests verify that:
 *   - ASR results are appended at cursor position, not overwriting compose
 *   - The × Clear button appears when compose has content, hides when empty
 *   - Ambiguous results leave compose unchanged
 *   - Ambiguous results show a status notification
 *
 * Because getting real microphone audio in Playwright is impractical, we use
 * window.__voiceTest.processAudio (exposed by recorder.js) to inject a fake
 * audio blob directly into the processing pipeline, while mocking
 * /api/voice/transcribe to return a controlled response.
 */

const CONTEXT_MOCK = { hint: '' };

async function setupRoutes(page: any, transcribeBody: object) {
  await page.route('/api/context', route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
  );
  await page.route('/api/voice/transcribe', route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(transcribeBody) })
  );
}

async function triggerProcessAudio(page: any) {
  // Simulate what stopRec does: capture cursor position, then run processAudio.
  // The route mock intercepts the POST and returns the configured response.
  await page.evaluate(async () => {
    (window as any).__voiceTest.captureInsertAt();
    const blob = new Blob([], { type: 'audio/webm' });
    await (window as any).__voiceTest.processAudio(blob);
  });
  // Give the async chain time to resolve.
  await page.waitForTimeout(300);
}

test.describe('Voice append mode (TASK-60)', () => {
  test('T1: append_two_segments — two consecutive recognitions concatenate in compose', async ({ page }) => {
    let callCount = 0;
    const responses = [
      { Kind: 'direct_prompt', Rewritten: 'hello' },
      { Kind: 'direct_prompt', Rewritten: 'world' },
    ];

    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
    );
    await page.route('/api/voice/transcribe', route => {
      const body = responses[callCount] || responses[responses.length - 1];
      callCount++;
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // First recognition: compose starts empty, should become "hello"
    await triggerProcessAudio(page);
    await expect(page.locator('#voci-compose')).toHaveValue('hello');

    // Second recognition: should append " world" → "hello world"
    await triggerProcessAudio(page);
    await expect(page.locator('#voci-compose')).toHaveValue('hello world');
  });

  test('T2: clear_btn_appears — clear button visible after recognition, hides after click', async ({ page }) => {
    await setupRoutes(page, { Kind: 'direct_prompt', Rewritten: 'some text' });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Initially clear-btn should be hidden (compose is empty)
    await expect(page.locator('#clear-btn')).toBeHidden();

    // After recognition, clear-btn should appear
    await triggerProcessAudio(page);
    await expect(page.locator('#voci-compose')).toHaveValue('some text');
    await expect(page.locator('#clear-btn')).toBeVisible();

    // Click clear-btn — compose empties, button hides
    await page.click('#clear-btn');
    await expect(page.locator('#voci-compose')).toHaveValue('');
    await expect(page.locator('#clear-btn')).toBeHidden();
  });

  test('T3: ambiguous_leaves_compose — ambiguous result does not modify existing compose text', async ({ page }) => {
    await setupRoutes(page, { Kind: 'ambiguous', Rewritten: '' });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Pre-fill compose with some text
    await page.fill('#voci-compose', 'existing text');
    await expect(page.locator('#voci-compose')).toHaveValue('existing text');

    // Ambiguous recognition — compose must be unchanged
    await triggerProcessAudio(page);
    await expect(page.locator('#voci-compose')).toHaveValue('existing text');
  });

  test('T4: ambiguous_shows_status — ambiguous result shows status notification', async ({ page }) => {
    await setupRoutes(page, { Kind: 'ambiguous', Rewritten: '' });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Initially status div is hidden
    await expect(page.locator('#voci-status')).toBeHidden();

    // Ambiguous recognition — status should appear with the right message
    await triggerProcessAudio(page);
    await expect(page.locator('#voci-status')).toBeVisible();
    await expect(page.locator('#voci-status')).toContainText('未识别');
  });

  test('T5: clear_btn_appears_from_typing — clear button shows when user types manually', async ({ page }) => {
    await page.route('/api/context', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(CONTEXT_MOCK) })
    );

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await expect(page.locator('#clear-btn')).toBeHidden();
    await page.fill('#voci-compose', 'typed');
    // Trigger input event so updateClearBtn runs
    await page.dispatchEvent('#voci-compose', 'input');
    await expect(page.locator('#clear-btn')).toBeVisible();

    // Clear via button
    await page.click('#clear-btn');
    await expect(page.locator('#voci-compose')).toHaveValue('');
    await expect(page.locator('#clear-btn')).toBeHidden();
  });
});
