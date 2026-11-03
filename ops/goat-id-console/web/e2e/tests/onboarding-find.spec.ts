// SPDX-License-Identifier: Apache-2.0
//
// E2E spec for the two scripts on /onboarding, onboard-queue.js (the
// self-refreshing queue) and onboard-find.js (the cross-stage candidate find,
// #1306), running together in Chromium the way the page runs them. It exists
// because the first cut of the find shipped a reload that could never fire:
// the poll wrote the new badge count before it asked whether a find was in
// progress, so on every later poll the badge already matched the server and
// a candidate who arrived mid-search stayed out of the hidden index until a
// navigation. Every Go test was green. Only the two scripts running against
// the page's markup, with the clock advanced past the poll, sees that.
//
// The page is a Go template, so there is no static file to load. The fixture
// below is the contract the scripts read and nothing else: the ids
// (candidate-filter, ob-stage, ob-find), the tab strip (.ob-tabs a[data-stage]
// .c), the index rows (a.row[data-filter]) and the two notes (.ob-find-none,
// .ob-find-more). TestTemplates_onboardingFindBlock pins the same ids on the
// rendered template from the Go side, so a rename breaks one of the two.
//
// Mocked mode only: the spec answers /onboarding and /api/onboard/proposals/
// counts itself; the two scripts come from serve.mjs.

import { test, expect, type Page, type Route } from "@playwright/test";

const LIVE_URL = process.env.INTAKE_URL || "";
const POLL_MS = 20_000; // onboard-queue.js POLL_MS

function fixture(): string {
  return `<!doctype html>
<html><head><meta charset="utf-8"><title>Onboarding fixture</title></head>
<body>
<input id="candidate-filter" type="search" autocomplete="off" oninput="filterCandidates(this.value)">
<div class="ob-tabs">
  <a href="/onboarding?status=proposed" data-stage="proposed" class="on">Awaiting sponsor<span class="c">3</span></a>
  <a href="/onboarding?status=sponsor_approved" data-stage="sponsor_approved">Awaiting admin<span class="c" hidden>0</span></a>
</div>
<div class="ob-wrap">
  <div class="ob-list" id="ob-stage">
    <a class="row" href="/onboarding?status=proposed&id=prop-1"><div class="h">alice-smith</div></a>
    <a class="row" href="/onboarding?status=proposed&id=prop-2"><div class="h">bob-jones</div></a>
    <a class="row" href="/onboarding?status=proposed&id=prop-3"><div class="h">cat-poe</div></a>
  </div>
  <div class="ob-list ob-find" id="ob-find" hidden>
    <a class="row" href="/onboarding?status=proposed&id=prop-1" data-filter="prop-1 alice@example.org Alice Smith alice-smith alice-gh"><div class="h">Alice Smith</div></a>
    <a class="row" href="/onboarding?status=enrolled&id=prop-2" data-filter="prop-2 bob@example.org Bob Jones bob-jones"><div class="h">Bob Jones</div></a>
    <a class="row" href="/onboarding?status=invited&id=inv-1" data-filter="inv-1 cat@example.org"><div class="h">cat@example.org</div></a>
    <div class="ob-empty ob-find-none" hidden>No candidate matches.</div>
    <div class="ob-note ob-find-more" hidden>Showing the first 50 matches · narrow the search.</div>
  </div>
  <div class="ob-detail"></div>
</div>
<script src="/onboard-queue.js"></script>
<script src="/onboard-find.js"></script>
</body></html>`;
}

type Harness = {
  loads: number; // documents served for /onboarding; 1 is the first load
  polls: number; // counts requests answered
  counts: { proposed: number; sponsor_approved: number };
  urls: string[]; // every request the page made
  errors: string[]; // uncaught page errors; every test expects none
};

async function mount(page: Page, path = "/onboarding"): Promise<Harness> {
  const h: Harness = { loads: 0, polls: 0, counts: { proposed: 3, sponsor_approved: 0 }, urls: [], errors: [] };
  page.on("pageerror", (e) => h.errors.push(String(e)));
  page.on("request", (r) => h.urls.push(r.url()));
  await page.route("**/onboarding*", (route: Route) => {
    h.loads++;
    return route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: fixture() });
  });
  await page.route("**/api/onboard/proposals/counts", (route: Route) => {
    h.polls++;
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(h.counts) });
  });
  // The poll runs on setInterval; a fake clock makes each tick deliberate
  // and the spec independent of wall time.
  await page.clock.install();
  await page.goto(path);
  return h;
}

test.describe("onboarding queue + find (mocked backend)", () => {
  test.skip(!!LIVE_URL, "INTAKE_URL is set — running live mode instead");

  test("a count that moves mid-find reloads once the box is cleared, and only then", async ({ page }) => {
    const h = await mount(page);
    const input = page.locator("#candidate-filter");
    const badge = page.locator('.ob-tabs a[data-stage="proposed"] .c');

    await input.fill("ali");
    await expect(page.locator("body")).toHaveClass(/ob-finding/);
    await expect(page.locator("#ob-stage")).toBeHidden();
    await expect(page.locator("#ob-find a.row", { hasText: "Alice Smith" })).toBeVisible();

    // A candidate arrives while the operator is searching: the badge carries
    // the news, the page is not reloaded out from under the find.
    h.counts = { proposed: 4, sponsor_approved: 0 };
    await page.clock.runFor(POLL_MS);
    await expect.poll(() => h.polls).toBe(1);
    await expect(badge).toHaveText("4");
    await page.clock.runFor(POLL_MS);
    await expect.poll(() => h.polls).toBe(2);
    expect(h.loads).toBe(1);

    // The box is cleared: the stage list is back, and the first poll after
    // that pays the reload the find held back. Before the fix the badge
    // already equalled the server here, nothing "moved", and this reload
    // never came.
    await input.fill("");
    await expect(page.locator("body")).not.toHaveClass(/ob-finding/);
    await expect(page.locator("#ob-stage")).toBeVisible();
    await page.clock.runFor(POLL_MS);
    await expect.poll(() => h.loads).toBe(2);
    await page.waitForLoadState("load");

    // The fresh page agrees with the server: nothing moves, nothing reloads.
    h.counts = { proposed: 3, sponsor_approved: 0 };
    const pollsBefore = h.polls;
    await page.clock.runFor(POLL_MS);
    await expect.poll(() => h.polls).toBeGreaterThan(pollsBefore);
    await page.clock.runFor(POLL_MS);
    await expect.poll(() => h.polls).toBeGreaterThan(pollsBefore + 1);
    expect(h.loads).toBe(2);
    expect(h.errors).toEqual([]);
  });

  test("the find filters the index in the browser and keeps the term in the fragment", async ({ page }) => {
    const h = await mount(page, "/onboarding#q=bob");
    const input = page.locator("#candidate-filter");
    const row = (text: string) => page.locator("#ob-find a.row", { hasText: text });

    // A fragment prefills the box and applies before anyone types.
    await expect(input).toHaveValue("bob");
    await expect(page.locator("#ob-stage")).toBeHidden();
    await expect(row("Bob Jones")).toBeVisible();
    await expect(row("Alice Smith")).toBeHidden();
    await expect(row("cat@example.org")).toBeHidden();

    await input.fill("example.org");
    await expect(page.locator("#ob-find a.row:visible")).toHaveCount(3);
    expect(new URL(page.url()).hash).toBe("#q=example.org");

    await input.fill("zzz");
    await expect(page.locator(".ob-find-none")).toBeVisible();
    await expect(page.locator("#ob-find a.row:visible")).toHaveCount(0);

    // One character is below the floor: the stage list is back, the
    // fragment is gone.
    await input.fill("z");
    await expect(page.locator("#ob-stage")).toBeVisible();
    await expect(page.locator("#ob-find")).toBeHidden();
    expect(new URL(page.url()).hash).toBe("");

    // A term the fragment has to encode survives a reload.
    await input.fill("Alice Smith");
    expect(new URL(page.url()).hash).toBe("#q=Alice%20Smith");
    await page.reload();
    await expect(input).toHaveValue("Alice Smith");
    await expect(row("Alice Smith")).toBeVisible();
    await expect(row("Bob Jones")).toBeHidden();

    // Nothing typed ever left the browser: no request carried a q parameter.
    for (const u of h.urls) expect(new URL(u).searchParams.has("q")).toBe(false);
    expect(h.errors).toEqual([]);
  });
});
