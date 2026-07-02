import { test, expect } from '@playwright/test';

/**
 * Frontend unit tests for the Markdown renderer (mdToHtml = marked + DOMPurify).
 *
 * These are backend-independent: /api/context is stubbed empty and each case
 * calls window.__voiceTest.mdToHtml(input) directly, then parses the returned
 * HTML string into a detached DOM to assert structure. No Go server data path
 * is exercised — this isolates the rendering contract itself.
 */

const EMPTY_CONTEXT = { hint: '' };

// Runs mdToHtml(input) in the page and reports structural facts about the output.
async function render(page: import('@playwright/test').Page, input: string) {
  return page.evaluate((md) => {
    const html = (window as any).__voiceTest.mdToHtml(md);
    const el = document.createElement('div');
    el.innerHTML = html;
    return {
      html,
      tables: el.querySelectorAll('table').length,
      tds: el.querySelectorAll('td').length,
      ths: el.querySelectorAll('th').length,
      lis: el.querySelectorAll('li').length,
      uls: el.querySelectorAll('ul').length,
      ps: el.querySelectorAll('p').length,
      pres: el.querySelectorAll('pre').length,
      codes: el.querySelectorAll('code').length,
      strongs: el.querySelectorAll('strong').length,
      scripts: el.querySelectorAll('script').length,
      hrs: el.querySelectorAll('hr').length,
      text: el.textContent || '',
    };
  }, input);
}

test.beforeEach(async ({ page }) => {
  await page.route('/api/context', route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EMPTY_CONTEXT) })
  );
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  await page.waitForFunction(() => !!(window as any).__voiceTest?.mdToHtml);
});

test.describe('mdToHtml rendering contract (frontend unit)', () => {
  test('GFM table → <table> with cells', async ({ page }) => {
    const r = await render(page, '| A | B |\n|---|---|\n| 1 | 2 |');
    expect(r.tables).toBe(1);
    expect(r.ths).toBe(2);
    expect(r.tds).toBe(2);
  });

  test('bullet list → three <li>', async ({ page }) => {
    const r = await render(page, '- one\n- two\n- three');
    expect(r.uls).toBe(1);
    expect(r.lis).toBe(3);
  });

  test('blank-line-separated text → multiple <p>', async ({ page }) => {
    const r = await render(page, '第一段\n\n第二段\n\n第三段');
    expect(r.ps).toBe(3);
  });

  test('fenced HTML code block is ESCAPED, not rendered', async ({ page }) => {
    const r = await render(page, '```html\n<table><tr><td>CODE</td></tr></table>\n```');
    // The HTML inside a code fence must be shown as literal text, not a live table.
    expect(r.pres).toBe(1);
    expect(r.tables).toBe(0);
    expect(r.text).toContain('<table>');       // literal angle brackets survive as text
    expect(r.html).toContain('&lt;table&gt;'); // escaped in the markup
  });

  test('XSS <script> is sanitized away', async ({ page }) => {
    const r = await render(page, '<script>window.__xss=1</script>hello');
    expect(r.scripts).toBe(0);
    const xss = await page.evaluate(() => (window as any).__xss);
    expect(xss).toBeUndefined();
  });

  test('inline code + bold render as <code> and <strong>', async ({ page }) => {
    const r = await render(page, 'use `foo()` and **bar**');
    expect(r.codes).toBe(1);
    expect(r.strongs).toBe(1);
  });

  test('thematic break → <hr>', async ({ page }) => {
    const r = await render(page, 'above\n\n---\n\nbelow');
    expect(r.hrs).toBe(1);
    expect(r.ps).toBe(2);
  });
});
