import { test, expect, type APIRequestContext } from '@playwright/test';
import axe from 'axe-core';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { CONSOLE_URL, CONSOLE_API_KEY } from '../playwright.config';

test.skip(!!process.env.INTAKE_URL || process.env.CONSOLE_E2E === '0', 'requires local console');
const routes = ['/identity', '/onboarding', '/onboarding/invite', '/users/00000000-0000-4000-8000-000000000001', '/onboard.html?t=test'];
const runner = { Authorization: `Bearer ${CONSOLE_API_KEY}` };

async function seedIdentity(request: APIRequestContext, suffix: string) {
  const email = `identity.accessibility.${suffix}@example.test`;
  const handle = `identity-accessibility-${suffix}`;
  const response = await request.post(CONSOLE_URL + '/api/onboard/register', {
    headers: runner,
    data: { email, handle, env: 'zagros', minted_by: 'e2e-runner' },
  });
  expect(response.status(), await response.text()).toBe(201);
  return { email, handle };
}

for (const theme of ['dark', 'light']) {
  for (const path of routes) {
    if (theme === 'light' && path.startsWith('/onboard.html')) continue;
    test(`${path} has accessible controls in ${theme}`, async ({ page, request }, testInfo) => {
      await page.addInitScript(value => {
        localStorage.setItem('goat-theme', value);
      }, theme);
      await page.route('**/api/onboard/preview?*', route => route.fulfill({ json: {
        valid: true, sponsor: 'Test Sponsor', tier_locked: true, prebound: { tier: 'developer' },
        machine_quota_locked: false, org_locked: false,
      } }));
      let identityHandle = '';
      if (path === '/identity') {
        const identity = await seedIdentity(request, `${theme}-${testInfo.workerIndex}-${testInfo.retry}-${Date.now()}`);
        identityHandle = identity.handle;
        await page.setExtraHTTPHeaders({ 'X-Forwarded-Email': identity.email });
      }
      await page.goto(path.startsWith('/onboard.html') ? path : CONSOLE_URL + path);
      if (path.startsWith('/onboard.html')) await expect(page.locator('#form')).toBeVisible();
      if (path === '/identity') await expect(page.getByLabel('Your handle')).toHaveValue(identityHandle);
      await page.addScriptTag({ content: axe.source });
      const violations = await page.evaluate(async () => {
        const result = await (window as any).axe.run();
        return result.violations.filter((v: any) => ['serious', 'critical'].includes(v.impact))
          .map((v: any) => ({ id: v.id, nodes: v.nodes.map((n: any) => ({ target: n.target, summary: n.failureSummary })) }));
      });
      expect(violations).toEqual([]);
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
      const inputs = page.locator('input:not([type=hidden]):not([type=checkbox]), select, textarea');
      for (const input of await inputs.all()) {
        if (!await input.isVisible()) continue;
        expect(await input.evaluate(el => parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThanOrEqual(16);
      }
      const firstInput = inputs.first();
      if (await firstInput.isVisible()) {
        await firstInput.focus();
        const focus = await firstInput.evaluate(el => {
          const style = getComputedStyle(el);
          return { color: style.outlineColor, background: style.backgroundColor, style: style.outlineStyle, width: parseFloat(style.outlineWidth) };
        });
        expect(focus.style).not.toBe('none');
        expect(focus.width).toBeGreaterThanOrEqual(2);
        expect(contrast(focus.color, focus.background)).toBeGreaterThanOrEqual(3);
      }
      if (path === '/identity') {
        await page.getByLabel('Your handle').focus();
        await page.keyboard.press('Tab');
        await expect(page.getByRole('button', { name: 'Copy', exact: true })).toBeFocused();
      }
      if (path === '/onboarding') {
        const current = page.locator('.ob-tabs [aria-current="page"]');
        await expect(current).toHaveCount(1);
        const colors = await current.evaluate(el => ({ color: getComputedStyle(el).color, background: getComputedStyle(el).backgroundColor }));
        expect(contrast(colors.color, colors.background)).toBeGreaterThanOrEqual(4.5);
      }
    });
  }
}

test('candidate change-request fields have labels and usable targets', async ({ page }) => {
  await page.route('**/api/onboard/preview?*', route => route.fulfill({ json: {
    valid: true, tier_locked: true, machine_quota_locked: true, org_locked: false,
    prebound: { tier: 'developer', machine_quota: 3 }, selectable_tiers: [{ id: 'browse', label: 'Browse' }],
  } }));
  await page.goto('/onboard.html?t=test');
  const target = page.getByRole('button', { name: 'Request a different number' });
  const box = await target.boundingBox();
  expect(box?.width).toBeGreaterThanOrEqual(24);
  expect(box?.height).toBeGreaterThanOrEqual(24);
  await target.click();
  await expect(page.getByLabel(/Machines \/ devices/)).toBeVisible();
  await page.getByRole('button', { name: 'Request a different access level' }).click();
  await expect(page.getByLabel('Access level', { exact: true })).toBeVisible();
  await page.addScriptTag({ content: axe.source });
  const violations = await page.evaluate(async () => (await (window as any).axe.run()).violations.filter((v: any) => ['serious', 'critical'].includes(v.impact)));
  expect(violations.map((v: any) => v.id)).toEqual([]);
});

test.describe('rendered challenge and offboarding states', () => {
  test.describe.configure({ mode: 'default' });
  let fixtures: string;
  test.beforeAll(() => {
    fixtures = mkdtempSync(join(tmpdir(), 'console-accessibility-'));
    execFileSync('go', ['test', './internal/handlers', '-run', '^TestAccessibilityBrowserFixtures$', '-count=1',
      '-args', `-browser-fixtures-dir=${fixtures}`], {
      cwd: fileURLToPath(new URL('../../../', import.meta.url)),
      timeout: 120_000,
    });
  });
  for (const theme of ['dark', 'light']) {
    for (const state of ['verify', 'enroll', 'resume', 'clear', 'lingering', 'unverified']) {
      test(`${state} uses readable state colors in ${theme}`, async ({ page }, testInfo) => {
        await page.addInitScript(value => {
          localStorage.setItem('goat-theme', value);
          history.replaceState({ goatStepUpSubmitted: true }, '');
        }, theme);
        await page.route('**/accessibility-fixture', route => route.fulfill({
          contentType: 'text/html', body: readFileSync(join(fixtures, `${state}.html`), 'utf8'),
        }));
        await page.goto(CONSOLE_URL + '/accessibility-fixture');
        expect(await page.locator('html').evaluate(el => getComputedStyle(el).colorScheme)).toBe(theme);
        const challenge = ['verify', 'enroll', 'resume'].includes(state);
        const control = page.locator(challenge ? '.stepup-card .btn' : '.verdict');
        await expect(control).toBeVisible();
        if (challenge) {
          const labels = { verify: 'Verify with second factor', enroll: 'Enroll an authenticator', resume: 'Continue' };
          await expect(control).toHaveText(labels[state as keyof typeof labels]);
        } else {
          await expect(control).toHaveAttribute('data-verdict', state);
        }
        await control.screenshot({ path: testInfo.outputPath(`${state}-${theme}.png`) });
        const colors = () => control.evaluate(el => ({
          color: getComputedStyle(el).color, background: getComputedStyle(el).backgroundColor,
        }));
        let pair = await colors();
        expect(pair.background).not.toBe('rgba(0, 0, 0, 0)');
        expect(contrast(pair.color, pair.background)).toBeGreaterThanOrEqual(4.5);
        if (challenge) {
          await control.hover();
          pair = await colors();
          expect(contrast(pair.color, pair.background)).toBeGreaterThanOrEqual(4.5);
          await page.mouse.move(0, 0);
          await control.focus();
          await expect(control).toBeFocused();
          expect(await control.evaluate(el => getComputedStyle(el).outlineStyle)).not.toBe('none');
        }
        await page.addScriptTag({ content: axe.source });
        const violations = await page.evaluate(async () => (await (window as any).axe.run())
          .violations.filter((v: any) => ['serious', 'critical'].includes(v.impact)).map((v: any) => v.id));
        expect(violations).toEqual([]);
      });
    }
  }
});

test('expanded proposal repository input uses the shared input size', async ({ page, request }) => {
  const headers = { Authorization: `Bearer ${CONSOLE_API_KEY}` };
  const invite = await request.post(CONSOLE_URL + '/api/onboard/invite', {
    headers, data: { env: 'zagros', tier: 'developer', affiliation: 'partner' },
  });
  expect(invite.ok()).toBeTruthy();
  const { token } = await invite.json();
  const submitted = await request.post(CONSOLE_URL + '/public/onboard/submit', {
    data: { token, submission: { name: 'Input Fixture', email: `input.${Date.now()}@example.test`, organization: 'Fixture', nationality: 'CAN' } },
  });
  expect(submitted.status()).toBe(201);
  const { proposal_id: id } = await submitted.json();
  await page.goto(`${CONSOLE_URL}/onboarding?status=submitted&id=${id}`);
  const input = page.locator('#repoaccess-add-repo');
  await expect(input).toBeVisible();
  expect(await input.evaluate(el => parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThanOrEqual(16);
});

function contrast(a: string, b: string) {
  function luminance(color: string) {
    const channels = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(v => {
      const c = v / 255;
      return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
  }
  const x = luminance(a), y = luminance(b);
  return (Math.max(x, y) + .05) / (Math.min(x, y) + .05);
}
