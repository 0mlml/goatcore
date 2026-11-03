// SPDX-License-Identifier: Apache-2.0
//
// E2E spec for #2109: the onboarding review page's "Access & authorities"
// panel overflowed the viewport because nested grid tracks (.ob-wrap,
// .idcard .grid, .idcard .kv) had no minmax(0,1fr)/min-width:0, so a long
// value anywhere in the identity card pushed the whole page wider than the
// window instead of wrapping. Every Go test was green, because this is a
// layout defect only a browser rendering the real CSS against real content
// can see.
//
// The review page (/onboarding) is a Go template, so there is no static
// file to load; the fixture below reproduces the .ob-wrap > .ob-list +
// .ob-detail structure the template renders (onboarding.html) and loads
// the real style.css and identity-card.js from web/static/ via serve.mjs,
// the same way onboarding-find.spec.ts's fixture does. renderIdentityCard
// is called for the reported failed-candidate state with a long identity;
// its repository request returns a maximum-length unbroken repo name so the
// interactive editor is stressed the way a real coalition-partner card is.

import { test, expect, type Route } from "@playwright/test";

const LIVE_URL = process.env.INTAKE_URL || "";
const LONG_EMAIL = "alicesmith12345@coalitionpartnerorganisationsubdomainwithnospaces.directorate-of-liaison.example.org";

function fixture(theme: "dark" | "light"): string {
  return `<!doctype html>
<html${theme === "light" ? ' data-theme="light"' : ""}><head><meta charset="utf-8"><title>Onboarding overflow fixture</title>
<link rel="stylesheet" href="/style.css">
</head>
<body>
<main>
<div class="ob-wrap">
  <div class="ob-list" id="ob-stage">
    <a class="row sel" href="/onboarding?status=failed&id=proposal-1">
      <div class="h">alice-smith</div>
      <div class="m"><span>${LONG_EMAIL}</span><span>zagros</span><span class="ob-age" data-status="failed"></span></div>
    </a>
    <a class="row" href="/onboarding?status=failed&id=proposal-2">
      <div class="h">bob-jones</div>
      <div class="m"><span>bob@example.org</span><span>zagros</span><span class="ob-age" data-status="failed"></span></div>
    </a>
  </div>
  <div class="ob-list ob-find" id="ob-find" hidden>
    <a class="row" href="/onboarding?status=failed&id=proposal-1" data-filter="proposal-1 ${LONG_EMAIL} Alice Smith alice-smith">
      <div class="h">Alice Smith</div>
      <div class="m"><span class="ob-badge stage">Failed</span><span>${LONG_EMAIL}</span><span>alice-smith</span><span>from operator</span><span>zagros</span></div>
    </a>
  </div>
  <div class="ob-detail">
    <div id="idcard"></div>
    <h3>Provisioning Failed · Retry the Mint</h3>
    <pre class="ob-failnote">repository grant failed for a coalition partner repository</pre>
    <div class="ob-actions"><button class="ok">Retry provisioning</button></div>
  </div>
</div>
</main>
<script>window.GOAT_CONSOLE_ADMIN = true;</script>
<script src="/identity-card.js"></script>
<script>
renderIdentityCard(document.getElementById('idcard'), {
  id: "proposal-1",
  handle: "alice-smith",
  status: "failed",
  tier: "developer",
  env: "zagros",
  sponsor: "operator",
  created_at: 1730000000,
  manifest_json: JSON.stringify({
    identity: {
      name: "Alice Smith",
      email: "alicesmithtestcandidatenospaceshere@anextremelylongcoalitionpartnerorganisationsubdomainnospaceshere.example.org",
      type: "person",
      nationality: "USA"
    },
    affiliation: { kind: "coalition", nation: "ISL", relationship: "liaison officer posted under a bilateral framework agreement" },
    vendor: { display_name: "Anextremelylongcoalitionpartnerorganisationdisplaynamewithnospacesforlayouttesting" },
    netbird: { enabled: true, auto_groups: ["TACTICAL", "DEV-CLIQUE"], ttl_days: 90 },
    ory_site: { enabled: true, groups: ["panoscope-admin"], credentials: { passkey: true } }
  })
});
</script>
</body></html>`;
}

async function mount(page: import("@playwright/test").Page, theme: "dark" | "light", width = 1366) {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.route("**/onboarding-overflow-fixture", (route: Route) =>
    route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: fixture(theme) }),
  );
  await page.route("**/onboarding/proposal-1/repo-access", (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json; charset=utf-8",
      body: JSON.stringify({
        github: "alice-coalition-partner",
        has_token: true,
        effective: [
          {
            // GitHub's limits are 39 characters for an owner and 100 for a
            // repository. Neither is required to contain a wrap opportunity.
            owner: "PartnerOrganisationWithVeryLongName123",
            repo: "r".repeat(100),
            permission: "maintain",
            source: "policy",
          },
        ],
        overrides: { add: [], remove: [] },
      }),
    }),
  );
  await page.setViewportSize({ width, height: 768 });
  await page.goto("/onboarding-overflow-fixture");
  await expect(page.locator("#repoaccess-add-btn")).toBeVisible();
  return errors;
}

async function expectRowsToContainTheirChildren(page: import("@playwright/test").Page, list: string) {
  const overflows = await page.locator(`${list} .row`).evaluateAll((rows) =>
    rows.flatMap((row) => {
      const rowRight = row.getBoundingClientRect().right;
      return Array.from(row.querySelectorAll("*"))
        .filter((child) => child.getBoundingClientRect().right > rowRight + 0.5)
        .map((child) => ({ tag: child.tagName, text: child.textContent }));
    }),
  );
  expect(overflows).toEqual([]);
}

test.describe("onboarding review — access & authorities panel stays inside the viewport (#2109)", () => {
  test.skip(!!LIVE_URL, "INTAKE_URL is set — this fixture spec doesn't apply in live mode");

  for (const theme of ["dark", "light"] as const) {
    test(`a long identity and repository slug stay inside a 1366px ${theme} viewport`, async ({ page }) => {
      const errors = await mount(page, theme);
      await expect(page.locator(".idcard .panel h3", { hasText: "Access & authorities" })).toBeVisible();

      // No horizontal page scroll: the document is never wider than the
      // viewport it's rendered in.
      const widths = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      expect(widths.scrollWidth).toBeLessThanOrEqual(widths.clientWidth);

      // The detail panel itself stays inside the viewport — the regression
      // this issue reported was the right edge of .ob-detail (and the .idcard
      // .grid panels inside it) running off the right of the window.
      const viewport = page.viewportSize();
      expect(viewport).not.toBeNull();
      const detailBox = await page.locator(".ob-detail").boundingBox();
      expect(detailBox).not.toBeNull();
      expect(detailBox!.x + detailBox!.width).toBeLessThanOrEqual(viewport!.width);

      // Same for each .idcard .grid panel ("Who" and "Access & authorities"):
      // each is a grid track that must shrink, not overflow, when its content
      // is long.
      const panelBoxes = await page.locator(".idcard .grid > .panel").all();
      expect(panelBoxes.length).toBeGreaterThanOrEqual(2);
      for (const panel of panelBoxes) {
        const box = await panel.boundingBox();
        expect(box).not.toBeNull();
        expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width);
      }

      await expectRowsToContainTheirChildren(page, "#ob-stage");
      await page.locator("#ob-stage").evaluate((element) => { element.hidden = true; });
      await page.locator("#ob-find").evaluate((element) => { element.hidden = false; });
      await expectRowsToContainTheirChildren(page, "#ob-find");

      expect(errors).toEqual([]);
    });
  }

  test("the identity panels stack when the detail pane is too narrow", async ({ page }) => {
    const errors = await mount(page, "dark", 900);
    const panels = await page.locator(".idcard .grid > .panel").evaluateAll((elements) =>
      elements.map((element) => {
        const box = element.getBoundingClientRect();
        return { x: box.x, y: box.y };
      }),
    );
    expect(panels).toHaveLength(2);
    expect(panels[1].y).toBeGreaterThan(panels[0].y);
    const widths = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }));
    expect(widths.scrollWidth).toBeLessThanOrEqual(widths.clientWidth);
    expect(errors).toEqual([]);
  });
});
