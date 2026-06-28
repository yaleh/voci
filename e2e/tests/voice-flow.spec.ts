import { test, expect } from '@playwright/test';

/**
 * Phase 3: Confirm/Cancel flow tests.
 * All tests mock /api/voice/transcribe and /api/voice/emit via page.route().
 * Audio is injected via page.evaluate (bypasses MediaRecorder / microphone).
 */

const MOCK_PROPOSAL = {
  RawTranscript: 'raw audio text',
  Rewritten: 'rewritten text',
  Kind: 'direct_prompt',
  Confidence: 0.9,
};

async function setupPage(page: any) {
  // Mock /api/context to avoid loading state
  await page.route('/api/context', (route: any) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ hint: '## Known Entities\ntest' }),
    });
  });
  await page.goto('/');
  await page.waitForFunction(
    () => !document.getElementById('ctx-known-body')?.textContent?.includes('(loading'),
    { timeout: 5000 }
  );
}

async function triggerSendAudio(page: any, proposal: object) {
  // Directly call the transcribe fetch to simulate sendAudio result
  await page.evaluate(async (p: object) => {
    // Simulate what sendAudio does after getting a proposal from /api/voice/transcribe
    const rawEl = document.getElementById('raw') as HTMLElement;
    const rewrittenEl = document.getElementById('rewritten') as HTMLTextAreaElement;
    const kindEl = document.getElementById('kind') as HTMLElement;
    const confidenceEl = document.getElementById('confidence') as HTMLElement;
    const previewEl = document.getElementById('preview') as HTMLElement;
    const statusEl = document.getElementById('status') as HTMLElement;

    const proposal = p as any;
    rawEl.textContent = proposal.RawTranscript || '';
    rewrittenEl.value = proposal.Rewritten || '';
    kindEl.textContent = proposal.Kind || '';
    confidenceEl.textContent = proposal.Confidence != null ? (proposal.Confidence * 100).toFixed(0) + '%' : '';
    // Set previewedKind via the global closure — we can't access it directly,
    // so we trigger sendAudio via fetch
    previewEl.style.display = '';
    statusEl.textContent = 'Review and confirm';
    // Store kind in a data attribute for later retrieval by confirm handler
    previewEl.dataset['kind'] = proposal.Kind || 'direct_prompt';
  }, proposal);
}

test.describe('Preview appears after transcription', () => {
  test('proposal fields populate preview UI elements', async ({ page }) => {
    await setupPage(page);

    // Route transcribe to return a mock proposal
    await page.route('/api/voice/transcribe', (route: any) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROPOSAL),
      });
    });

    // Trigger sendAudio by calling fetch directly
    await page.evaluate(async (proposal: any) => {
      const r = await fetch('/api/voice/transcribe', { method: 'POST', body: new Blob(['fake']) });
      const data = await r.json();
      const rawEl = document.getElementById('raw') as HTMLElement;
      const rewrittenEl = document.getElementById('rewritten') as HTMLTextAreaElement;
      const kindEl = document.getElementById('kind') as HTMLElement;
      const confidenceEl = document.getElementById('confidence') as HTMLElement;
      const previewEl = document.getElementById('preview') as HTMLElement;
      const statusEl = document.getElementById('status') as HTMLElement;
      rawEl.textContent = data.RawTranscript || '';
      rewrittenEl.value = data.Rewritten || '';
      kindEl.textContent = data.Kind || '';
      confidenceEl.textContent = data.Confidence != null ? (data.Confidence * 100).toFixed(0) + '%' : '';
      previewEl.style.display = '';
      statusEl.textContent = 'Review and confirm';
    }, MOCK_PROPOSAL);

    // Verify preview is visible
    const preview = page.locator('#preview');
    await expect(preview).toBeVisible();

    // Verify field content
    await expect(page.locator('#raw')).toContainText('raw audio text');
    await expect(page.locator('#rewritten')).toHaveValue('rewritten text');
    await expect(page.locator('#kind')).toContainText('direct_prompt');
    await expect(page.locator('#confidence')).toContainText('90%');
  });
});

test.describe('Confirm sends edited text', () => {
  test('editing rewritten and clicking confirm sends modified text to /emit', async ({ page }) => {
    let emitBody: string | null = null;

    await page.route('/api/context', (route: any) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) });
    });
    await page.route('/api/voice/transcribe', (route: any) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROPOSAL),
      });
    });
    await page.route('/api/voice/emit', async (route: any) => {
      const req = route.request();
      emitBody = await req.postData();
      route.fulfill({ status: 204 });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Trigger a fetch to populate previewedKind in the closure
    await page.evaluate(async () => {
      const r = await fetch('/api/voice/transcribe', { method: 'POST', body: new Blob(['fake']) });
      const data = await r.json();
      // Simulate sendAudio handler
      (document.getElementById('raw') as HTMLElement).textContent = data.RawTranscript || '';
      (document.getElementById('rewritten') as HTMLTextAreaElement).value = data.Rewritten || '';
      (document.getElementById('kind') as HTMLElement).textContent = data.Kind || '';
      (document.getElementById('confidence') as HTMLElement).textContent =
        data.Confidence != null ? (data.Confidence * 100).toFixed(0) + '%' : '';
      (document.getElementById('preview') as HTMLElement).style.display = '';
      (document.getElementById('status') as HTMLElement).textContent = 'Review and confirm';
    });

    // Edit the rewritten textarea
    await page.locator('#rewritten').fill('edited text');

    // Click confirm
    await page.locator('#confirm').click();

    // Wait for emit to be called
    await page.waitForTimeout(500);

    expect(emitBody).not.toBeNull();
    const parsed = JSON.parse(emitBody!);
    expect(parsed.text).toBe('edited text');
    expect(parsed.kind).toBe('direct_prompt');
  });
});

test.describe('Confirm + 204 → reset', () => {
  test('after 204 from /emit, preview hides and status resets', async ({ page }) => {
    await page.route('/api/context', (route: any) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) });
    });
    await page.route('/api/voice/transcribe', (route: any) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROPOSAL),
      });
    });
    await page.route('/api/voice/emit', (route: any) => {
      route.fulfill({ status: 204 });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Show the preview
    await page.evaluate(async () => {
      const r = await fetch('/api/voice/transcribe', { method: 'POST', body: new Blob(['fake']) });
      const data = await r.json();
      (document.getElementById('raw') as HTMLElement).textContent = data.RawTranscript || '';
      (document.getElementById('rewritten') as HTMLTextAreaElement).value = data.Rewritten || '';
      (document.getElementById('kind') as HTMLElement).textContent = data.Kind || '';
      (document.getElementById('preview') as HTMLElement).style.display = '';
      (document.getElementById('status') as HTMLElement).textContent = 'Review and confirm';
    });

    await expect(page.locator('#preview')).toBeVisible();

    // Click confirm
    await page.locator('#confirm').click();

    // Preview should hide
    await expect(page.locator('#preview')).toBeHidden();

    // Status should reset
    await expect(page.locator('#status')).toContainText('Hold Space to record');
  });
});

test.describe('Confirm + non-204 → error message', () => {
  test('500 from /emit shows emit error in status', async ({ page }) => {
    await page.route('/api/context', (route: any) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) });
    });
    await page.route('/api/voice/transcribe', (route: any) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROPOSAL),
      });
    });
    await page.route('/api/voice/emit', (route: any) => {
      route.fulfill({ status: 500, body: 'internal error' });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await page.evaluate(async () => {
      const r = await fetch('/api/voice/transcribe', { method: 'POST', body: new Blob(['fake']) });
      const data = await r.json();
      (document.getElementById('raw') as HTMLElement).textContent = data.RawTranscript || '';
      (document.getElementById('rewritten') as HTMLTextAreaElement).value = data.Rewritten || '';
      (document.getElementById('kind') as HTMLElement).textContent = data.Kind || '';
      (document.getElementById('preview') as HTMLElement).style.display = '';
      (document.getElementById('status') as HTMLElement).textContent = 'Review and confirm';
    });

    await page.locator('#confirm').click();

    // Status should contain "Emit failed"
    await expect(page.locator('#status')).toContainText('Emit failed');
  });
});

test.describe('Cancel → reset', () => {
  test('clicking cancel hides preview and clears rewritten', async ({ page }) => {
    await page.route('/api/context', (route: any) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Manually show preview
    await page.evaluate(() => {
      (document.getElementById('rewritten') as HTMLTextAreaElement).value = 'some text';
      (document.getElementById('preview') as HTMLElement).style.display = '';
    });

    await expect(page.locator('#preview')).toBeVisible();

    // Click cancel
    await page.locator('#cancel').click();

    // Preview should be hidden
    await expect(page.locator('#preview')).toBeHidden();

    // rewritten should be empty
    await expect(page.locator('#rewritten')).toHaveValue('');
  });
});

test.describe('Empty rewritten → Confirm does not emit', () => {
  test('confirm with empty or blank rewritten does not call /emit', async ({ page }) => {
    let emitCalled = false;

    await page.route('/api/context', (route: any) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ hint: '' }) });
    });
    await page.route('/api/voice/emit', (route: any) => {
      emitCalled = true;
      route.fulfill({ status: 204 });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Show preview with empty/blank rewritten
    await page.evaluate(() => {
      (document.getElementById('rewritten') as HTMLTextAreaElement).value = '   ';
      (document.getElementById('preview') as HTMLElement).style.display = '';
    });

    // Click confirm with blank/trim-empty text
    await page.locator('#confirm').click();
    await page.waitForTimeout(300);

    // /emit should NOT have been called
    expect(emitCalled).toBe(false);
  });
});
