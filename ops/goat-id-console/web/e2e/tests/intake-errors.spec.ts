import { test, expect, type Page } from '@playwright/test';

const valid = { valid: true, sponsor: 'Test Sponsor', tier_locked: true, prebound: { tier: 'developer' }, machine_quota_locked: false, org_locked: false };

async function fillPerson(page: Page) {
  await page.getByLabel(/^Full name/).fill('Test Candidate');
  await page.getByLabel(/^Email/).fill('candidate@example.invalid');
  await page.getByLabel(/^Nationality/).fill('CAN');
  await page.getByLabel(/^Organization/).fill('Test Org');
  await page.getByLabel(/^Sector/).selectOption('commercial');
}

test('missing token explains the missing link without inviting form entry', async ({ page }) => {
  await page.goto('/onboard.html');
  await expect(page.locator('#gate')).toContainText('missing');
  await expect(page.locator('#lead')).not.toContainText('Fill in');
  await expect(page.locator('#form')).toBeHidden();
});

test('preview loading is visible until a valid invitation arrives', async ({ page }) => {
  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/onboard/preview?*', async route => { await pending; await route.fulfill({ json: valid }); });
  await page.goto('/onboard.html?t=test');
  await expect(page.locator('#lead')).toContainText(/checking|loading/i);
  await expect(page.locator('#form')).toBeHidden();
  release();
  await expect(page.locator('#form')).toBeVisible();
});

for (const status of [429, 500, 503]) {
  test(`preview ${status} offers retry without invalidating the invite`, async ({ page }) => {
    await page.route('**/api/onboard/preview?*', route => route.fulfill({ status, json: { error: { message: 'internal service unavailable' } } }));
    await page.goto('/onboard.html?t=test');
    await expect(page.locator('#gate')).toContainText(/try again|retry/i);
    await expect(page.locator('#gate')).not.toContainText(/not valid|new invite|internal service/i);
  });
}

test('expired invitation directs the candidate to their sponsor', async ({ page }) => {
  await page.route('**/api/onboard/preview?*', route => route.fulfill({ json: { valid: false, reason: 'expired' } }));
  await page.goto('/onboard.html?t=test');
  await expect(page.locator('#gate')).toContainText(/sponsor.*new invite/i);
});

for (const plain of [false, true]) {
  test(`submit failure preserves input and offers retry (plain=${plain})`, async ({ page }) => {
    await page.route('**/api/onboard/preview?*', route => route.fulfill({ json: valid }));
    await page.route('**/api/onboard/submit', route => route.fulfill(plain
      ? { status: 503, contentType: 'text/plain', body: 'upstream internal address' }
      : { status: 503, json: { error: { message: 'upstream internal address' } } }));
    await page.goto('/onboard.html?t=test');
    await fillPerson(page);
    await page.getByRole('button', { name: 'Submit for review' }).click();
    await expect(page.locator('#err')).toContainText(/try again|retry/i);
    await expect(page.locator('#err')).not.toContainText('upstream');
    await expect(page.getByLabel(/^Full name/)).toHaveValue('Test Candidate');
    await expect(page.getByRole('button', { name: 'Submit for review' })).toBeEnabled();
  });
}
