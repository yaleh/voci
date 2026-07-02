import { test, expect } from '@playwright/test';

/**
 * Regression test: dialogue feed must not re-render on every /api/context poll.
 *
 * Root cause: the hint includes a live "## Claude Code Session" section whose
 * "- ran:" line changes on every poll. Without an HTML-level dedup guard,
 * dialogueFeed.innerHTML is reassigned every 5 s, re-triggering the msg-in
 * CSS animation and producing visible flicker.
 */
test.describe('No-flicker: dialogue feed does not mutate on unchanged messages', () => {
  test('dialogueFeed receives no mutations after initial render when messages are stable', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Inject a MutationObserver that counts innerHTML-level child mutations on
    // #voci-dialogue after the page has settled.
    const mutationCount = await page.evaluate(() => {
      return new Promise<number>((resolve) => {
        const feed = document.getElementById('voci-dialogue');
        if (!feed) { resolve(-1); return; }

        // Let the first render settle (initial context fetch).
        setTimeout(() => {
          let count = 0;
          const obs = new MutationObserver((mutations) => {
            // Only count mutations that actually changed child nodes (re-renders).
            const childListChanges = mutations.filter(m => m.type === 'childList' && m.addedNodes.length > 0);
            count += childListChanges.length;
          });
          obs.observe(feed, { childList: true, subtree: false });

          // Observe for 12 s — covers at least 2 full poll cycles (5 s each).
          setTimeout(() => {
            obs.disconnect();
            resolve(count);
          }, 12_000);
        }, 3_000); // settle window after initial render
      });
    });

    // With the fix: 0 mutations expected (dialogue messages are stable).
    // Without the fix: 2+ mutations (one per 5 s poll cycle).
    expect(mutationCount).toBe(0);
  });
});

test.describe('No-flicker: context panel sections use dedup guards', () => {
  test('recorder.bundle.js contains lastDialogueHtml dedup variable', async ({ page }) => {
    const response = await page.request.get('/recorder.bundle.js');
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toContain('lastDialogueHtml');
  });
});
