import { test, expect } from '@playwright/test';

// The component reads an already-rendered .secret; no identity service is involved.
for (const denied of [false, true]) {
  test(`credential copy reports its result (denied=${denied})`, async ({ page }) => {
    await page.goto('/onboard.html');
    await page.evaluate(deny => {
      const pre = document.createElement('pre');
      pre.className = 'secret'; pre.textContent = 'synthetic-credential-for-test';
      document.body.append(pre);
      Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (value: string) => {
        if (deny) throw new Error('denied');
        (window as any).copiedCredential = value;
      } } });
    }, denied);
    await page.addScriptTag({ url: '/secret-copy.js' });
    await page.getByRole('button', { name: 'Copy', exact: true }).click();
    if (denied) await expect(page.getByRole('status').last()).toContainText('Copy failed');
    else {
      await expect(page.getByRole('status').last()).toContainText('Copied');
      expect(await page.evaluate(() => (window as any).copiedCredential)).toBe('synthetic-credential-for-test');
    }
  });
}
