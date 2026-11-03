// SPDX-License-Identifier: Apache-2.0
//
// goat#2209, driven against the REAL console (layout.html with its top bar,
// style.css and identity-card.js as served), the way onboarding-restore.spec.ts
// does. onboarding-overflow.spec.ts passes at 900px only because its fixture
// page has no top bar; this spec is the one that includes it.
//
// Pinned:
//  - at 900 and 820 px no console page scrolls sideways, and a long signed-in
//    address is either fully visible or truncated with its full value in title;
//  - the repository-access editor keeps input, select and button on one row,
//    the select is not stretched, and the button is a standard console button
//    labelled "Add";
//  - a long email in the identity card's Who panel wraps at natural points
//    (one or two lines, never four mid-word fragments), and the card uses the
//    same 160px label column as .ob-kv where it has room for one.
// Each state is screenshotted in light and dark for inspection.
//
// Mocked mode only (it needs the local console); live mode skips it.

import { test, expect, type APIRequestContext, type Page } from "@playwright/test";
import { CONSOLE_URL, CONSOLE_API_KEY } from "../playwright.config";

const SKIP = !!process.env.INTAKE_URL || process.env.CONSOLE_E2E === "0";
const LONG_ACTOR = "alexandra.administrator-with-a-long-name@identity-plane.partner-organisation.example";
const ADMIN = { "X-Forwarded-Groups": "PLATFORM-ADMIN", "X-Forwarded-Email": LONG_ACTOR };
const RUNNER = { Authorization: `Bearer ${CONSOLE_API_KEY}`, "Content-Type": "application/json" };
const PAGES = ["/", "/identity", "/onboarding", "/holds", "/reactivation-requests", "/permissions", "/audit"];

test.use({ baseURL: CONSOLE_URL, extraHTTPHeaders: ADMIN });
test.skip(SKIP, "needs the local console (mocked mode only)");

async function withTheme(page: Page, theme: "light" | "dark") {
  await page.addInitScript((t) => {
    localStorage.setItem("goat-theme", t);
    if (t === "light") document.documentElement.dataset.theme = "light";
  }, theme);
}

// A submitted candidate with a long, realistic email and organisation: the
// review page shows the identity card and an unlocked repository editor.
async function seedSubmitted(request: APIRequestContext): Promise<{ id: string; email: string }> {
  const email = `maximilian.lastname-hyphenated.${Date.now()}@research.long-partner-organisation.example`;
  const invite = await request.post(CONSOLE_URL + "/api/onboard/invite", {
    headers: RUNNER, data: { env: "zagros", tier: "developer", affiliation: "partner" },
  });
  expect(invite.ok(), await invite.text()).toBeTruthy();
  const { token } = await invite.json();
  const submit = await request.post(CONSOLE_URL + "/public/onboard/submit", {
    data: { token, submission: {
      name: "Maximilian Lastname-Hyphenated", email, nationality: "CAN",
      organization: "Long Partner Organisation for Applied Research and Field Integration",
    } },
  });
  expect(submit.status(), await submit.text()).toBe(201);
  const { proposal_id: id } = await submit.json();
  return { id, email };
}

for (const theme of ["dark", "light"] as const) {
  for (const width of [900, 820]) {
    test(`no console page scrolls sideways at ${width}px (${theme})`, async ({ page }, info) => {
      await withTheme(page, theme);
      await page.setViewportSize({ width, height: 768 });
      for (const path of PAGES) {
        await page.goto(path);
        const { scroll, client } = await page.evaluate(() => ({
          scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth,
        }));
        expect(scroll, `${path} at ${width}px: page is ${scroll}px wide in a ${client}px window`).toBeLessThanOrEqual(client);

        const actor = page.locator("header.topbar .actor");
        await expect(actor).toBeVisible();
        const bar = await page.locator("header.topbar").boundingBox();
        const box = await actor.boundingBox();
        expect(box && bar && box.x + box.width <= bar.x + bar.width + 0.5, `${path}: the signed-in address runs past the bar`).toBeTruthy();
        const clipped = await actor.evaluate((el) => el.scrollWidth > el.clientWidth);
        if (clipped) await expect(actor).toHaveAttribute("title", LONG_ACTOR);
        else await expect(actor).toHaveText(LONG_ACTOR);
      }
      await page.goto("/onboarding");
      await page.screenshot({ path: info.outputPath(`topbar-${width}-${theme}.png`) });
    });
  }

  test(`repository editor and identity card read cleanly at 1366px (${theme})`, async ({ page, request }, info) => {
    const p = await seedSubmitted(request);
    await withTheme(page, theme);
    await page.setViewportSize({ width: 1366, height: 768 });
    await page.goto(`/onboarding?status=submitted&id=${p.id}`);

    // Repository editor: one row, unstretched select, standard "Add" button.
    const input = page.locator("#repoaccess-add-repo");
    const select = page.locator("#repoaccess-add-perm");
    const add = page.locator("#repoaccess-add-btn");
    await expect(input).toBeVisible();
    await expect(add).toHaveText("Add");
    await expect(add).toHaveClass(/\bprimary\b/);
    const [ib, sb, bb] = [await input.boundingBox(), await select.boundingBox(), await add.boundingBox()];
    const panel = await page.locator("#repoaccess-panel").boundingBox();
    expect(ib && sb && bb && panel).toBeTruthy();
    const mid = (b: { y: number; height: number }) => b.y + b.height / 2;
    expect(Math.abs(mid(ib!) - mid(sb!)), "input and select share a row").toBeLessThan(4);
    expect(Math.abs(mid(sb!) - mid(bb!)), "select and button share a row").toBeLessThan(4);
    expect(sb!.width, "the select is only as wide as its options").toBeLessThan(panel!.width / 2);
    expect(ib!.width, "the text field takes the free width").toBeGreaterThan(sb!.width);

    // Identity card: the email wraps only at natural points (@ . / - _), not
    // mid-word. Walk the text one character at a time and record the character
    // before every line change.
    const email = page.locator(".idcard .kv b", { hasText: /^email$/ }).locator("xpath=following-sibling::span[1]");
    await expect(email).toHaveText(p.email);
    const breaks = await email.evaluate((el) => {
      const before: string[] = [];
      const range = document.createRange();
      let lastTop: number | null = null, prev = "";
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const text = node.textContent || "";
        for (let i = 0; i < text.length; i++) {
          range.setStart(node, i); range.setEnd(node, i + 1);
          const r = range.getClientRects()[0];
          if (!r) continue;
          if (lastTop !== null && r.top > lastTop + 1) before.push(prev);
          lastTop = r.top; prev = text[i];
        }
      }
      return before;
    });
    for (const c of breaks) expect("@./-_", `the email broke mid-word after "${c}"`).toContain(c);
    // Where the panel is wide enough for two columns, the label column matches .ob-kv's 160px.
    const cols = await page.locator(".idcard .kv").first().evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(" "));
    if (cols.length === 2) expect(cols[0]).toBe("160px");

    await page.screenshot({ path: info.outputPath(`card-1366-${theme}.png`), fullPage: true });
    await page.setViewportSize({ width: 900, height: 768 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBeTruthy();
    await page.screenshot({ path: info.outputPath(`card-900-${theme}.png`), fullPage: true });
  });
}
