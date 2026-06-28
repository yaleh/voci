import { test, expect } from '@playwright/test';

/**
 * Phase 2: JS logic unit tests for context panel rendering.
 * All tests mock /api/context via page.route() — no real audio needed.
 */

async function loadPageWithHint(page: any, hint: string) {
  // Mock /api/context to return the supplied hint
  await page.route('/api/context', (route: any) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ hint }),
    });
  });
  // Mock /api/voice/transcribe to prevent accidental real calls
  await page.route('/api/voice/transcribe', (route: any) => {
    route.fulfill({ status: 503, body: 'mocked' });
  });
  await page.goto('/');
  // Wait for context to be populated (loading… replaced)
  await page.waitForFunction(
    () => !document.getElementById('ctx-known-body')?.textContent?.includes('(loading'),
    { timeout: 5000 }
  );
}

test.describe('renderDialogue — U: line inline', () => {
  test('U: line renders as .dialogue-turn div without details', async ({ page }) => {
    const hint = '## Recent Dialogue\nU: hello world';
    await loadPageWithHint(page, hint);

    const body = page.locator('#ctx-dialogue-body');
    await expect(body.locator('.dialogue-turn')).toHaveCount(1);
    // U: lines should NOT produce a <details> element
    await expect(body.locator('details')).toHaveCount(0);
    await expect(body).toContainText('hello world');
  });
});

test.describe('renderDialogue — short A: line inline', () => {
  test('short A: line (<=120 chars) renders inline without details', async ({ page }) => {
    const shortContent = 'A short assistant response.';
    const hint = `## Recent Dialogue\nA: ${shortContent}`;
    await loadPageWithHint(page, hint);

    const body = page.locator('#ctx-dialogue-body');
    await expect(body.locator('details')).toHaveCount(0);
    await expect(body.locator('.dialogue-turn')).toHaveCount(1);
    await expect(body).toContainText(shortContent);
  });
});

test.describe('renderDialogue — long A: line collapsible', () => {
  test('long A: line (>120 chars) renders as collapsible details with summary ending in …', async ({ page }) => {
    const longContent = 'A'.repeat(121) + ' this is a very long assistant response that exceeds the threshold';
    const hint = `## Recent Dialogue\nA: ${longContent}`;
    await loadPageWithHint(page, hint);

    const body = page.locator('#ctx-dialogue-body');
    // Should have a <details class="dialogue-turn">
    const details = body.locator('details.dialogue-turn');
    await expect(details).toHaveCount(1);
    // Summary should end with …
    const summary = details.locator('summary');
    const summaryText = await summary.textContent();
    expect(summaryText).toMatch(/…$/);
  });
});

test.describe('escHtml XSS protection', () => {
  test('script tags in hint content are escaped, not executed', async ({ page }) => {
    const hint = '## Known Entities\n<script>alert(1)</script>\n## Recent Dialogue\nU: <script>evil()</script>';
    await loadPageWithHint(page, hint);

    // Check that the page doesn't have any live script tags injected into body
    const injectedScripts = await page.evaluate(() => {
      const body = document.getElementById('ctx-known-body');
      // If XSS worked, a script element would be present in body
      return body ? body.getElementsByTagName('script').length : -1;
    });
    expect(injectedScripts).toBe(0);

    // The literal text &lt;script&gt; should appear in the DOM (as escaped text)
    const knownBody = await page.locator('#ctx-known-body').textContent();
    expect(knownBody).toContain('<script>');
    // But the raw tag should NOT be in innerHTML as an actual tag
    const hasRawTag = await page.evaluate(() => {
      const el = document.getElementById('ctx-known-body');
      return el ? el.innerHTML.includes('<script>') : false;
    });
    expect(hasRawTag).toBe(false);

    // In dialogue, check escHtml escaped XSS
    const dialogueHTML = await page.evaluate(() => {
      const el = document.getElementById('ctx-dialogue-body');
      return el ? el.innerHTML : '';
    });
    // Should contain &lt; not actual < in a script tag
    expect(dialogueHTML).toContain('&lt;script&gt;');
  });
});

test.describe('extractSection — Known Entities and Active Tasks', () => {
  test('extracts correct sections', async ({ page }) => {
    const hint = '## Known Entities\nfoo\n## Active Tasks\nbar\n## Recent Dialogue\nU: baz';
    await loadPageWithHint(page, hint);

    const knownText = await page.locator('#ctx-known-body').textContent();
    expect(knownText?.trim()).toBe('foo');

    const tasksText = await page.locator('#ctx-tasks-body').textContent();
    expect(tasksText?.trim()).toBe('bar');

    const dialogueText = await page.locator('#ctx-dialogue-body').textContent();
    expect(dialogueText).toContain('baz');
  });
});

test.describe('Four panel initial open/closed state', () => {
  test('ctx-known and ctx-tasks are open; ctx-dialogue and ctx-session are closed', async ({ page }) => {
    const hint = '## Known Entities\nalice\n## Active Tasks\ntask1';
    await loadPageWithHint(page, hint);

    // ctx-known should have open attribute
    const knownOpen = await page.locator('#ctx-known').getAttribute('open');
    expect(knownOpen).not.toBeNull();

    // ctx-tasks should have open attribute
    const tasksOpen = await page.locator('#ctx-tasks').getAttribute('open');
    expect(tasksOpen).not.toBeNull();

    // ctx-dialogue should NOT have open attribute
    const dialogueOpen = await page.locator('#ctx-dialogue').getAttribute('open');
    expect(dialogueOpen).toBeNull();

    // ctx-session should NOT have open attribute
    const sessionOpen = await page.locator('#ctx-session').getAttribute('open');
    expect(sessionOpen).toBeNull();
  });
});

test.describe('Panel toggle', () => {
  test('clicking ctx-dialogue summary opens it', async ({ page }) => {
    const hint = '## Recent Dialogue\nU: hello';
    await loadPageWithHint(page, hint);

    // Verify closed initially
    expect(await page.locator('#ctx-dialogue').getAttribute('open')).toBeNull();

    // Click the summary to open
    await page.locator('#ctx-dialogue summary').click();

    // Now should have open attribute
    const openAttr = await page.locator('#ctx-dialogue').getAttribute('open');
    expect(openAttr).not.toBeNull();
  });
});
