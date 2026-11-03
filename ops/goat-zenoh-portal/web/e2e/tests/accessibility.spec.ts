import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
const prefix = 'a946693f0afcb5e707a9ecdc6d69ff6a';
const topic = { key_pattern: `${prefix}/fixture/status/v1`, format: 'json', description: 'Fixture status', schemas: [], publish_count: 1, created_at: 1 };
async function mock(page: Page, theme = 'light', state = 'populated') {
  await page.addInitScript((theme) => { localStorage.setItem('portalApiKey', 'synthetic'); localStorage.setItem('portalTheme', theme); }, theme);
  // Mirror the server's display-only lexicon substitution for static fixtures.
  await page.route(/\.html(?:\?|$)/, async r => {
    const response = await r.fetch();
    const lex: Record<string, string> = { PRODUCT_NAME: 'Fabric', PRODUCT_TITLE: 'Fabric Portal', ORG: 'slot', ORGS: 'slots', ORG_TITLE: 'Slot', ORGS_TITLE: 'Slots', ORG_UPPER: 'SLOT', PANOSCOPE_URL: '/index.html' };
    const body = (await response.text()).replace(/__LEX_([A-Z_]+)__/g, (token, key) => lex[key] ?? token);
    await r.fulfill({ response, body });
  });
  await page.route('**/api/**', r => {
    const path = new URL(r.request().url()).pathname;
    let body: unknown;
    if (path === '/api/branding') body = { product_name: 'Fabric Portal', org: 'slot', orgs: 'slots', org_title: 'Slot', orgs_title: 'Slots' };
    else if (state === 'signed-out' && path !== '/api/health') return r.fulfill({ status: 401, json: { error: { message: 'Sign in first.' } } });
    else if (state === 'empty' && path === '/api/orgs') body = { orgs: [] };
    else if (state === 'empty' && path.endsWith('/topics')) body = { topics: [] };
    else if (state === 'empty' && path === '/api/catalog') body = { totals: { orgs: 0, topics: 0, schema_versions: 0, formats: 0 }, orgs: [] };
    else if (state === 'empty' && path.startsWith('/api/schemas/render/')) return r.fulfill({ status: 404, json: { error: { message: 'Schema not found.' } } });
    else if (path === '/api/whoami') body = { kind: 'operator', operator: true, visible_orgs: 1 };
    else if (path === '/api/health') body = { version: 'test', uptime: '1h' };
    else if (path === '/api/orgs') body = { orgs: [{ prefix, display_name: 'Fixture slot', created_at: 1 }] };
    else if (path.endsWith('/topics')) body = { topics: [topic] };
    else if (path === '/api/catalog') body = { totals: { orgs: 1, topics: 1, schema_versions: 0, formats: 1 }, orgs: [{ prefix, display_name: 'Fixture slot', approved: true, synthetic: true, topics: [topic] }] };
    else if (path.startsWith('/api/schemas/render/')) body = { key: topic.key_pattern, format: 'json', vendor: prefix, schema: { version: 'v1', format_storage: 'json', all_versions: ['v1'] }, source_lang: 'json', rendered: { files: [], messages: [], enums: [] }, proto_source: {}, examples: [] };
    else if (path === '/api/siblings') body = {};
    else return r.fulfill({ status: 404, json: { error: { message: 'Unmocked fixture endpoint' } } });
    return r.fulfill({ json: body });
  });
}
for (const theme of ['light', 'dark']) for (const width of [390, 1440]) {
  for (const path of ['/index.html', '/vendor.html', `/topics.html?vendor=${prefix}`, `/schema.html?key=${prefix}/fixture/status/v1`]) {
    test(`${path.split('?')[0]} ${theme} ${width} accessibility`, async ({ page }, testInfo) => {
      await mock(page, theme);
      await page.setViewportSize({ width, height: 900 });
      await page.goto(path);
      await page.locator(`#theme-toggle button[data-mode="${theme}"]`).click();
      if (path.startsWith('/index')) await expect(page.locator('#vendors-table tbody tr')).toHaveCount(1);
      if (path.startsWith('/vendor')) await expect(page.locator('tr.t')).toHaveCount(1);
      if (path.startsWith('/topics')) await expect(page.locator('#topics-table tbody tr')).toHaveCount(1);
      if (path.startsWith('/schema')) {
        await expect(page.locator('#m-download')).toBeVisible();
        if (width === 390) expect((await page.locator('#load-schema').boundingBox())!.height).toBeGreaterThanOrEqual(44);
      }
      await expect(page.locator('body')).not.toContainText('__LEX_');
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      const placeholderRatios = await page.locator('input[placeholder]:visible, textarea[placeholder]:visible').evaluateAll(elements => {
        const luminance = (color: string) => {
          const rgb = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(v => v / 255).map(v => v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
          return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
        };
        return elements.map(el => {
          const foreground = luminance(getComputedStyle(el, '::placeholder').color);
          const background = luminance(getComputedStyle(el).backgroundColor);
          return (Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05);
        });
      });
      for (const ratio of placeholderRatios) expect(ratio).toBeGreaterThanOrEqual(4.5);
      const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']).analyze();
      const violations = result.violations.filter(v => ['serious', 'critical'].includes(v.impact || '')).map(v => ({ id: v.id, nodes: v.nodes.map(n => ({ target: n.target, summary: n.failureSummary })) }));
      expect(violations).toEqual([]);
      await page.screenshot({ path: testInfo.outputPath("page.png"), fullPage: true });
    });
  }
}
test('catalog shortcuts require list focus and preserve modifiers and typing', async ({ page }) => {
  await mock(page); await page.goto('/vendor.html');
  await expect(page.locator('tr.t')).toHaveCount(1);
  await page.locator('header h1').click();
  await page.keyboard.press('/'); await expect(page.locator('#q')).not.toBeFocused();
  await page.locator('#cards').focus(); await page.keyboard.press('j'); await expect(page.locator('tr.t')).toBeFocused();
  await page.keyboard.press('Shift+Enter'); await expect(page).toHaveURL(/vendor\.html$/);
  await page.keyboard.press('Control+/'); await expect(page.locator('tr.t')).toBeFocused();
  await page.keyboard.press('/'); await expect(page.locator('#q')).toBeFocused();
  await page.keyboard.type('jk'); await expect(page.locator('#q')).toHaveValue('jk');
});

test('keyboard focus exposes tooltip and retains a visible ring', async ({ page }) => {
  await mock(page); await page.goto(`/topics.html?vendor=${prefix}`);
  const tip = page.locator('.tip:visible').first();
  await expect(tip).toHaveAttribute('tabindex', '0');
  // Enter keyboard modality, then walk the real tab order to the first tooltip.
  for (let i = 0; i < 60 && !(await tip.evaluate(el => el === document.activeElement)); i++) await page.keyboard.press('Tab');
  await expect(tip).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('.tip-bubble')).toBeVisible();
  expect(await tip.evaluate(el => getComputedStyle(el).outlineStyle)).not.toBe('none');
  await page.keyboard.press('Escape');
  await expect(page.locator('.tip-bubble')).toHaveCount(0);
  await expect(tip).toBeFocused();
});
test('mobile inputs avoid zoom and reduced motion disables transitions', async ({ page }) => {
  await mock(page); await page.setViewportSize({ width: 390, height: 900 });
  await page.emulateMedia({ reducedMotion: 'reduce' }); await page.goto('/vendor.html');
  expect(await page.locator('#q').evaluate(el => parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThanOrEqual(16);
  const duration = await page.locator('.card .tw').first().evaluate(el => getComputedStyle(el).transitionDuration);
  expect(duration.split(',').every(value => parseFloat(value) <= 0.001)).toBe(true);
});

for (const state of ['empty', 'signed-out']) for (const path of ['/index.html', '/vendor.html', `/topics.html?vendor=${prefix}`, `/schema.html?key=${topic.key_pattern}`]) {
  test(`${state} ${path.split('?')[0]} stays accessible on mobile system dark`, async ({ page }) => {
    await mock(page, 'system', state);
    if (state === 'signed-out') await page.addInitScript(() => localStorage.removeItem('portalApiKey'));
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto(path);
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    if (path.startsWith('/schema')) await expect(page.locator('#error')).toBeVisible();
    else if (path.startsWith('/vendor')) await expect(page.locator('#cards')).not.toContainText('Loading');
    else if (path.startsWith('/topics') && state === 'empty') {
      await expect(page.locator('#topics-empty')).toBeVisible();
      await expect(page.locator('#topics-table')).toBeHidden();
    } else if (path.startsWith('/topics')) await expect(page.locator('#topics-table')).not.toContainText('Loading');
    else await expect(page.locator('#vendors-table')).not.toContainText('Loading');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']).analyze();
    expect(result.violations.filter(v => ['serious', 'critical'].includes(v.impact || ''))).toEqual([]);
  });
}
test('desktop catalog survives mobile resize and touch controls', async ({ browser }) => {
  const context = await browser.newContext({ hasTouch: true, viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();
  await mock(page); await page.goto('/vendor.html');
  await expect(page.locator('tr.t')).toHaveCount(1);
  await page.setViewportSize({ width: 390, height: 900 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  const summary = page.locator('.card summary').first();
  await summary.tap(); await expect(page.locator('tr.t')).not.toBeVisible();
  await summary.tap(); await expect(page.locator('tr.t')).toBeVisible();
  await page.locator('#q').tap(); await expect(page.locator('#q')).toBeFocused();
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(page.locator('tr.t')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(1440);
  await context.close();
});

for (const width of [390, 1440]) test(`collapsed catalog keeps copy and inspect usable at ${width}px`, async ({ page }) => {
  await mock(page);
  await page.setViewportSize({ width, height: 900 });
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, 'writeText', {
      value: async (text: string) => { (window as any).copiedPrefix = text; },
    });
  });
  await page.goto('/vendor.html');
  await expect(page.locator('tr.t')).toHaveCount(1);
  await page.locator('.card summary').click();
  await expect(page.locator('tr.t')).not.toBeVisible();
  const tools = page.locator('.card-shell .card-tools');
  const copy = tools.getByRole('button', { name: 'Copy prefix' });
  const inspect = tools.getByRole('link', { name: 'Inspect slot', exact: true });
  await expect(copy).toBeVisible();
  await expect(inspect).toBeVisible();
  await copy.click();
  expect(await page.evaluate(() => (window as any).copiedPrefix)).toBe(prefix);
  await expect(copy).toHaveText('Copy prefix');
  await expect(page.locator('.card')).not.toHaveAttribute('open', '');
  await inspect.click();
  await expect(page).toHaveURL(`/inspector.html?key=${encodeURIComponent(prefix + '/**')}`);
});
