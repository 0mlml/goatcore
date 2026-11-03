import { test, expect } from '@playwright/test';
import { CONSOLE_URL, IDENTITY_URL } from '../playwright.config';

const localFixtureEnabled = !process.env.INTAKE_URL && process.env.CONSOLE_E2E !== '0';

test.beforeEach(async ({ request }) => {
  test.skip(!localFixtureEnabled, 'needs the local console');
  const response = await request.post(`${IDENTITY_URL}/__fixture/reset`);
  expect(response.status()).toBe(204);
});

test.afterEach(async ({ request }) => {
  if (!localFixtureEnabled) return;
  const response = await request.post(`${IDENTITY_URL}/__fixture/reset`);
  expect(response.status()).toBe(204);
});

test('Save groups persists the supported Kratos child-path patch', async ({ page, request }) => {
  await page.setExtraHTTPHeaders({
    'X-Forwarded-Groups': 'PLATFORM-ADMIN',
    'X-Forwarded-Email': 'admin@example.invalid',
  });

  const id = '00000000-0000-4000-8000-000000000001';
  await page.goto(`${CONSOLE_URL}/users/${id}?idp=tenant`);
  const groups = page.getByLabel('Comma-separated login groups');
  await expect(groups).toHaveValue('GRAFANA-READONLY');
  await expect(page.getByLabel("Also revoke this user's sessions")).toBeChecked();

  await groups.fill('GRAFANA-READONLY, COMPLIANCE');
  await page.getByRole('button', { name: 'Save groups' }).click();

  await expect(page).toHaveURL(`${CONSOLE_URL}/users/${id}?idp=tenant`);
  await expect(page.getByLabel('Comma-separated login groups')).toHaveValue('GRAFANA-READONLY, COMPLIANCE');

  const stored = await (await request.get(`${IDENTITY_URL}/__fixture/identity`)).json();
  expect(stored.metadata_public).toEqual({
    groups: ['GRAFANA-READONLY', 'COMPLIANCE'],
    org: { short_tag: 'fixture-org' },
    aff: { id: 'fixture-org', kind: 'partner' },
    vendor: { short_tag: 'legacy-fixture-org' },
    unrelated: 'preserved',
  });
  expect(stored.revokeCalls).toBe(1);
});
