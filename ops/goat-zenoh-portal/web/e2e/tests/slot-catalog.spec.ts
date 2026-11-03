// SPDX-License-Identifier: Apache-2.0
//
// Browser smoke for the Slot catalog page (vendor.html + vendor.js), the
// portal's widest-audience page. Pins the behaviours a redesign must keep:
//
//   * ONE catalog request per page load, no /api/orgs or per-topic fan-out;
//   * search a slot by name / alias / prefix fragment / topic word, Enter opens it;
//   * chips filter and carry the counts; the result line reconciles them;
//   * the ?vendor= / ?v= inbound deep-link contract still selects a slot;
//   * no per-topic "Last seen" column; the per-slot "Last publish" fact is in
//     the card header and silently absent when nothing is recorded;
//   * the page renders on a slot (vendor) key, not only for operators;
//   * the first topic row is on screen without scrolling at 1366×768.
//
// Backend is mocked via page.route: no Go portal, no DB.

import { test, expect, type Page, type Route } from "@playwright/test";

const P = {
  mantis: "a946693f0afcb5e707a9ecdc6d69ff6a",
  heron: "c899ef93aa2a57e8968e621a52ab296d",
  gateway: "0a3a73c67d4c5d0fef7f35c2df493161",
  pending: "4a7af443356ce8425a4d6efdde4081db",
  quiet: "15a14405dbb3b8effc8e3a0816e9695a",
};

function catalog(now: number) {
  const schema = (v: string) => ({ version: v, format: "protobuf-source", size_bytes: 10, sha256: "ab".repeat(32), uploaded_at: now - 86_400 });
  return {
    totals: { orgs: 5, orgs_with_topics: 4, orgs_without_topics: 1, topics: 6, schema_versions: 4, formats: 3 },
    orgs: [
      {
        prefix: P.mantis, display_name: "mantis-alpha", synthetic: true, approved: true,
        alias: "mantis-alpha", last_publish_at: now - 180, publish_count: 74427,
        topics: [
          { key_pattern: `${P.mantis}/mantis/alpha-fob/imagery/v1`, format: "protobuf:Entity", description: "Mantis imagery frames", schemas: [schema("v1"), schema("v2")], publish_count: 0, created_at: now - 9e5 },
          { key_pattern: `${P.mantis}/archive-vault/theater/unused/v1`, format: "protobuf:Hello", description: "auto-registered for panoscope decode", schemas: [schema("v1")], publish_count: 0, created_at: now - 9e5 },
        ],
      },
      {
        prefix: P.heron, display_name: "heron-bravo", synthetic: true, approved: true,
        last_publish_at: now - 30 * 3600, publish_count: 12,
        topics: [
          { key_pattern: `${P.heron}/heron/bravo-fob/status/v1`, format: "json", description: "Heron platform status", schemas: [schema("v1")], publish_count: 0, created_at: now - 9e5 },
          { key_pattern: `${P.heron}/heron/bravo-fob/tracks/v1`, format: "protobuf:Entity", description: "", schemas: [], publish_count: 0, created_at: now - 9e5 },
        ],
      },
      {
        prefix: P.gateway, display_name: "radar-gateway-01", synthetic: false, approved: true, alias: "rg01",
        publish_count: 0,
        topics: [
          { key_pattern: `${P.gateway}/radar/gw01/plots/v1`, format: "cbor", description: "", schemas: [], publish_count: 0, created_at: now - 9e5 },
        ],
      },
      {
        prefix: P.pending, display_name: "partner-liaison-x", synthetic: false, approved: false, publish_count: 0,
        topics: [
          { key_pattern: `${P.pending}/liaison-x/reports/v1`, format: "json", description: "", schemas: [], publish_count: 0, created_at: now - 9e5 },
        ],
      },
      { prefix: P.quiet, display_name: "weather-feed-eu", synthetic: false, approved: true, publish_count: 0, topics: [] },
    ],
  };
}

type Mocks = { catalogCalls: string[]; otherApiCalls: string[] };

// Mock the shell reads + the catalog. Every OTHER /api/* request is recorded
// and answered 404 so a reintroduced fan-out fails the spec loudly.
async function mockPortal(page: Page, opts: { operator?: boolean } = {}): Promise<Mocks> {
  const now = Math.floor(Date.now() / 1000);
  const m: Mocks = { catalogCalls: [], otherApiCalls: [] };
  await page.addInitScript(() => localStorage.setItem("portalApiKey", "vp_test_key"));
  await page.route("**/api/**", (route: Route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    switch (url.pathname) {
      case "/api/branding":
        return json({ product_name: "Fabric Portal", org: "slot", orgs: "slots", org_title: "Slot", orgs_title: "Slots" });
      case "/api/whoami":
        return json(opts.operator
          ? { kind: "operator", visible_orgs: 5 }
          : { kind: "vendor", vendor: P.gateway, visible_orgs: 5 });
      case "/api/siblings":
        return json({ panoscope: "", stethoscope: "https://stethoscope.example", flashlight: "https://flashlight.example" });
      case "/api/catalog":
        m.catalogCalls.push(url.search);
        return json(catalog(now));
      default:
        m.otherApiCalls.push(url.pathname + url.search);
        return json({ error: { code: "not_mocked", message: `not mocked: ${url.pathname}` } }, 404);
    }
  });
  return m;
}

async function open(page: Page, query = "") {
  await page.goto(`/vendor.html${query}`);
  await expect(page.locator("#result")).toContainText("visible slots");
}

test.describe("slot catalog", () => {
  test("one catalog request, no orgs listing, no per-topic fan-out; old select is gone", async ({ page }) => {
    const m = await mockPortal(page);
    await open(page);
    expect(m.catalogCalls).toHaveLength(1);
    expect(m.catalogCalls[0]).toContain("include=empty");
    expect(m.otherApiCalls).toEqual([]);
    await expect(page.locator("#vendor-select")).toHaveCount(0);
    // Default landing is the synthetic scope; the result line reconciles the
    // scope count with the goatnet total the masthead describes.
    await expect(page.locator('.chip[data-f="kind"][data-v="synthetic"]')).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator("#result")).toHaveText("2 of 5 visible slots · 4 topics");
    await expect(page.locator("#who")).toHaveText("5 slots visible");
  });

  test("first topic row is on screen without scrolling at 1366×768", async ({ page }) => {
    await page.setViewportSize({ width: 1366, height: 768 });
    await mockPortal(page);
    await open(page);
    const row = page.locator("tr.t").first();
    await expect(row).toBeVisible();
    const box = await row.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.y + box!.height).toBeLessThanOrEqual(768);
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
  });

  test("typing a slot name and pressing Enter opens it and writes ?vendor=", async ({ page }) => {
    await mockPortal(page);
    await open(page);
    await page.locator("#cards").focus();
    await page.keyboard.press("/");
    await expect(page.locator("#q")).toBeFocused();
    await page.keyboard.type("mantis");
    await expect(page.locator("#suggest")).toHaveClass(/open/);
    await expect(page.locator("#suggest .it").first()).toContainText("mantis-alpha");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(`vendor=${P.mantis}`));
    await expect(page.locator(".card")).toHaveCount(1);
    await expect(page.locator(".card .name")).toHaveText("mantis-alpha");
    await expect(page.locator("#rail .ri.sel")).toContainText("mantis-alpha");
  });

  test("a prefix fragment and a display alias both find the slot", async ({ page }) => {
    await mockPortal(page);
    await open(page);
    await page.fill("#q", P.gateway.slice(0, 6));
    await expect(page.locator("#suggest .it").first()).toContainText("radar-gateway-01");
    await page.fill("#q", "rg01");
    await expect(page.locator("#suggest .it").first()).toContainText("radar-gateway-01");
    await page.keyboard.press("Enter");
    // Selecting a real slot from the synthetic scope still shows it.
    await expect(page.locator(".card .name")).toHaveText("radar-gateway-01");
    // The alias is display-only: the Inspector link is composed from the prefix.
    await expect(page.locator(".card-shell .card-tools a")).toHaveAttribute("href", `/inspector.html?key=${encodeURIComponent(P.gateway + "/**")}`);
  });

  test("a topic word suggests the topic; Enter opens it in the Inspector at key=<topic>", async ({ page }) => {
    await mockPortal(page);
    await open(page);
    await page.fill("#q", "imagery");
    await expect(page.locator("#suggest .grp", { hasText: "Topics" })).toBeVisible();
    await page.keyboard.press("Enter");
    const key = `${P.mantis}/mantis/alpha-fob/imagery/v1`;
    await expect(page).toHaveURL(new RegExp(`/inspector\\.html\\?key=${encodeURIComponent(key).replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`));
  });

  test("chips carry the counts and filter; the format chip narrows rows", async ({ page }) => {
    await mockPortal(page);
    await open(page);
    await expect(page.locator('[data-count="all"]')).toHaveText("4");
    await expect(page.locator('[data-count="synthetic"]')).toHaveText("2");
    await expect(page.locator('[data-count="real"]')).toHaveText("1");
    await expect(page.locator('[data-count="pending"]')).toHaveText("1");
    await expect(page.locator('[data-count="empty"]')).toHaveText("1");

    await page.click('.chip[data-f="kind"][data-v="all"]');
    await expect(page.locator(".card")).toHaveCount(4);
    await expect(page.locator("#result")).toHaveText("4 of 5 visible slots · 6 topics");
    await expect(page).toHaveURL(/kind=all/);

    await page.click('.chip[data-f="fmt"][data-v="json"]');
    await expect(page.locator("tr.t")).toHaveCount(2);
    for (const pill of await page.locator("tr.t .fmt").allTextContents()) expect(pill).toBe("json");

    await page.click('.chip[data-f="fmt"][data-v="json"]'); // toggle off
    await page.click('.chip[data-f="schema"]');
    await expect(page.locator("tr.t")).toHaveCount(3);
    await expect(page.locator("tr.t .none")).toHaveCount(0);

    await page.click('.chip[data-f="schema"]');
    await page.click('.chip[data-f="published"]');
    await expect(page.locator(".card")).toHaveCount(1);
    await expect(page.locator(".card .name")).toHaveText("mantis-alpha");
  });

  test("Nothing declared chip shows the registered-but-silent slots", async ({ page }) => {
    await mockPortal(page);
    await open(page); // default scope is synthetic; the chip counts and shows every kind
    await page.click('.chip[data-f="empty"]');
    await expect(page.locator(".card")).toHaveCount(1);
    await expect(page.locator(".card .name")).toHaveText("weather-feed-eu");
    await expect(page.locator(".card .nothing")).toContainText("No topic declared yet");
  });

  test("?vendor= and ?v= deep links still select a slot; ?v=__all__ widens the scope", async ({ page }) => {
    await mockPortal(page);
    await open(page, `?vendor=${P.heron}`);
    await expect(page.locator(".card")).toHaveCount(1);
    await expect(page.locator(".card .name")).toHaveText("heron-bravo");

    await open(page, `?v=${P.heron}`);
    await expect(page.locator(".card .name")).toHaveText("heron-bravo");
    await expect(page).toHaveURL(new RegExp(`vendor=${P.heron}`));

    await open(page, "?v=__all__");
    await expect(page.locator(".card")).toHaveCount(4);
  });

  test("rows: prefix elided, one schema column, sibling links keep key=<topic>, no Last seen column", async ({ page }) => {
    await mockPortal(page);
    await open(page, `?vendor=${P.mantis}`);
    const headers = await page.locator(".card th").allTextContents();
    expect(headers.map((h) => h.trim().toLowerCase())).not.toContain("last seen");
    expect(headers.map((h) => h.trim().toLowerCase())).toEqual(["topic", "format", "schema", ""]);

    const row = page.locator("tr.t", { hasText: "imagery" });
    await expect(row.locator(".key .el")).toHaveText("…/");
    await expect(row.locator(".key .tail")).toHaveText("mantis/alpha-fob/imagery/v1");
    await expect(row.locator(".vp")).toHaveText(["v1", "v2"]);
    const key = `${P.mantis}/mantis/alpha-fob/imagery/v1`;
    await expect(row).toHaveAttribute("data-href", `/inspector.html?key=${encodeURIComponent(key)}`);
    await expect(row.locator(".acts a", { hasText: "Diagnose" })).toHaveAttribute("href", `https://stethoscope.example/?key=${encodeURIComponent(key)}`);
    await expect(row.locator(".acts a", { hasText: "Tail" })).toHaveAttribute("href", `https://flashlight.example/?key=${encodeURIComponent(key)}`);
    await expect(row.locator(".acts a", { hasText: "Schema" })).toHaveAttribute("href", `/schema.html?key=${encodeURIComponent(key)}&version=v2`);

    // Operator-note boilerplate is hidden; a real description shows.
    await expect(page.locator("tr.t .d")).toHaveCount(1);
    await expect(page.locator("tr.t .d")).toHaveText("Mantis imagery frames");
    // A topic with no schema says so once.
    await open(page, `?vendor=${P.heron}`);
    await expect(page.locator("tr.t", { hasText: "tracks" }).locator(".none")).toHaveText("no schema");
  });

  test("card header shows the per-slot Last publish fact, or that none is recorded", async ({ page }) => {
    await mockPortal(page);
    await open(page, "?kind=all");
    const mantis = page.locator(`.card[data-p="${P.mantis}"]`);
    // The mock stamps "now" before the page renders; 3 min holds for 30 s after that stamp.
    await expect(mantis.locator(".seen")).toHaveText("Last publish 3 min ago · 74,427 publishes");
    await expect(mantis.locator(".dot")).toHaveClass(/ok/);
    const heron = page.locator(`.card[data-p="${P.heron}"]`);
    await expect(heron.locator(".dot")).toHaveClass(/old/);
    const gw = page.locator(`.card[data-p="${P.gateway}"]`);
    await expect(gw.locator(".seen")).toHaveText("no publish recorded yet");
    await expect(gw.locator(".badge")).toHaveCount(0);
    await expect(page.locator(`.card[data-p="${P.pending}"] .badge`)).toHaveText("pending");
    // "synthetic" appears exactly once per synthetic card.
    await expect(mantis.locator("summary")).toContainText("synthetic");
    expect((await mantis.locator("summary").innerText()).toLowerCase().split("synthetic").length - 1).toBe(1);
  });

  test("keyboard: j/k move between rows, Enter opens the focused row", async ({ page }) => {
    await mockPortal(page);
    await open(page, `?vendor=${P.mantis}`);
    await page.locator("#cards").focus();
    await page.keyboard.press("j");
    await expect(page.locator("tr.t").first()).toBeFocused();
    await page.keyboard.press("j");
    await expect(page.locator("tr.t").nth(1)).toBeFocused();
    await page.keyboard.press("k");
    await expect(page.locator("tr.t").first()).toBeFocused();
    const href = await page.locator("tr.t").first().getAttribute("data-href");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(href!.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  });

  test("renders on a slot key with an operator-only surface refused", async ({ page }) => {
    // Non-operator whoami; anything but the catalog answers 404. The page
    // must still render its rows and never call an operator-only endpoint.
    const m = await mockPortal(page, { operator: false });
    const errors: string[] = [];
    page.on("console", (msg) => { if (msg.type() === "error") errors.push(msg.text()); });
    await open(page);
    await expect(page.locator("tr.t").first()).toBeVisible();
    expect(m.otherApiCalls.filter((p) => p.startsWith("/api/progress"))).toEqual([]);
    expect(errors.filter((e) => !/favicon/.test(e))).toEqual([]);
  });

  test("Enter with the typeahead closed filters the page; it never acts on a hidden suggestion", async ({ page }) => {
    await mockPortal(page);
    await open(page, "?kind=all");
    await page.click("#q");
    await page.keyboard.type("imagery");
    await expect(page.locator("#suggest")).toHaveClass(/open/);
    await expect(page.locator("#suggest .it").first()).toContainText("imagery");
    // Click away (closes the box), come back, press Enter.
    await page.locator("body").click({ position: { x: 1, y: 1 } });
    await expect(page.locator("#suggest")).not.toHaveClass(/open/);
    await page.click("#q");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/vendor\.html\?.*q=imagery/);
    await expect(page).not.toHaveURL(/inspector\.html/);
    await expect(page.locator("tr.t")).toHaveCount(1);
  });

  test("j/k skip rows inside a collapsed card", async ({ page }) => {
    await mockPortal(page);
    await open(page, "?kind=all");
    const first = page.locator(".card").first();
    await first.locator("summary").click();
    await expect(first).not.toHaveAttribute("open", "");
    await page.locator("#cards").focus();
    await page.keyboard.press("j");
    const focused = page.locator("tr.t:focus");
    await expect(focused).toHaveCount(1);
    expect(await focused.evaluate((el) => !!el.closest("details[open]"))).toBe(true);
    await page.keyboard.press("j");
    await expect(page.locator("tr.t:focus")).toHaveCount(1);
    expect(await page.locator("tr.t:focus").evaluate((el) => !!el.closest("details[open]"))).toBe(true);
  });

  test("a format filter the new kind does not offer is dropped, not kept invisibly", async ({ page }) => {
    await mockPortal(page);
    await open(page, "?kind=all");
    await page.click('.chip[data-f="fmt"][data-v="cbor"]');
    await expect(page).toHaveURL(/format=cbor/);
    await page.click('.chip[data-f="kind"][data-v="synthetic"]');
    await expect(page.locator(".card")).toHaveCount(2);
    await expect(page).not.toHaveURL(/format=/);
    await expect(page.locator('.chip[data-f="fmt"][aria-pressed="true"]')).toHaveCount(0);
  });

  test("a selected slot outside the current scope still has a selected rail item", async ({ page }) => {
    await mockPortal(page);
    await open(page, `?vendor=${P.gateway}`); // default scope is synthetic; gateway is real
    await expect(page.locator(".card")).toHaveCount(1);
    await expect(page.locator("#rail .ri.sel")).toHaveCount(1);
    await expect(page.locator("#rail .ri.sel")).toHaveAttribute("data-p", P.gateway);
    await expect(page.locator("#rail .ri.sel")).toContainText("radar-gateway-01");
    // The format chips describe the selected slot, not the scope it sits outside of.
    await expect(page.locator('.chip[data-f="fmt"][data-v="cbor"]')).toHaveCount(1);
  });

  test("picking a no-topic slot from search does not leave the Nothing-declared filter on", async ({ page }) => {
    await mockPortal(page);
    await open(page);
    await page.click("#q");
    await page.keyboard.type("weather-feed-eu");
    await expect(page.locator("#suggest .it").first()).toContainText("weather-feed-eu");
    await page.keyboard.press("Enter");
    await expect(page.locator(".card")).toHaveCount(1);
    await page.click('.chip[data-f="kind"][data-v="all"]');
    await expect(page.locator(".card")).toHaveCount(4);
    await expect(page.locator('.chip[data-f="empty"]')).toHaveAttribute("aria-pressed", "false");
    await expect(page).not.toHaveURL(/empty=1/);
  });

  test("empty state names what to loosen and offers a way out", async ({ page }) => {
    await mockPortal(page);
    await open(page);
    await page.fill("#q", "zzz-nothing");
    await page.keyboard.press("Enter");
    await expect(page.locator(".empty")).toContainText("Nothing matches zzz-nothing among synthetic slots");
    await page.click("#loosen");
    await expect(page.locator(".card")).toHaveCount(4);
    await expect(page.locator("#q")).toHaveValue("");
  });
});
