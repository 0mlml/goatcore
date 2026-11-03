import { test, expect } from '@playwright/test';
import { CONSOLE_URL } from '../playwright.config';

test.use({ baseURL: CONSOLE_URL });
test.skip(!!process.env.INTAKE_URL || process.env.CONSOLE_E2E === '0', 'requires local console');

test('candidate accepts an invite using the real console and receives a clickable setup link', async ({ page }) => {
  await page.goto('/invites');
  await page.getByLabel(/^Preset login groups/).fill('GRAFANA-READONLY');
  await page.getByRole('button', { name: 'Generate invite URL' }).click();
  const link = await page.locator('.recovery-link-box code').innerText();
  expect(link).toBeTruthy();
  await page.goto(new URL(link!, CONSOLE_URL).pathname);
  await expect(page.locator('nav, .stepup-strip, footer')).toHaveCount(0);
  await page.getByLabel('Email', { exact: true }).fill('candidate@example.invalid');
  await page.getByLabel('Display name').fill('Test Candidate');
  await page.getByRole('button', { name: /accept invitation/i }).click();
  await expect(page.getByRole('link', { name: 'Set Up Credentials' })).toHaveAttribute('href', 'https://login.example.invalid/recover?flow=fixture');
  await expect(page.getByText('123456', { exact: true })).toBeVisible();
  await expect(page.locator('nav, .stepup-strip, footer')).toHaveCount(0);
});

test('invalid invite has a candidate error page', async ({ page }) => {
  const response = await page.goto('/invites/missing/accept');
  expect(response?.status()).toBe(404);
  await expect(page.getByRole('alert')).toContainText('sponsor');
  await expect(page.locator('nav, .stepup-strip, footer')).toHaveCount(0);
});

test('bound invitation keeps its email read-only and uses the candidate completion page', async ({ page }) => {
  await page.goto('/invites');
  await page.getByLabel(/^Preset login groups/).fill('GRAFANA-READONLY');
  await page.getByLabel(/^Invitee email/).fill('candidate@example.invalid');
  await page.getByRole('button', { name: 'Generate invite URL' }).click();
  const link = await page.locator('.recovery-link-box code').innerText();
  const response = await page.goto(new URL(link, CONSOLE_URL).pathname);
  expect(response?.headers()['cache-control']).toBe('no-store');
  await expect(page.getByLabel('Email', { exact: true })).toHaveValue('candidate@example.invalid');
  await expect(page.getByLabel('Email', { exact: true })).toHaveAttribute('readonly', '');
  await expect(page.locator('nav, .stepup-strip, footer')).toHaveCount(0);
  await page.getByLabel('Display name').fill('Test Candidate');
  await page.getByRole('button', { name: /accept invitation/i }).click();
  await expect(page.getByRole('link', { name: 'Set Up Credentials' })).toHaveAttribute('href', 'https://login.example.invalid/recover?flow=fixture');
  await expect(page.getByText('123456', { exact: true })).toBeVisible();
  await expect(page.locator('nav, .stepup-strip, footer')).toHaveCount(0);
});
