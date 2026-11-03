// SPDX-License-Identifier: Apache-2.0
//
// E2E spec for holds.js, the script on the Mesh holds page (/holds,
// goat#1042), running in Chromium against the markup contract the Go
// template renders. The page is a Go template, so the fixture below carries
// only what the script reads: the quick filters (#holds-summary
// button.holds-band[data-band]), the text filter (#holds-filter), one
// <tbody class="hold-row"> per held peer with data-band / data-kind /
// data-attention / data-filter / data-held / data-inactive, the miss line (#holds-miss) and the two
// sortable headers (th.sortable[data-sort] > button). The Go handler test
// pins the same attributes on the rendered page from the other side.
//
// Pins the acceptance criteria that no Go test can: the quick filters
// narrow by kind (dormancy / security) or by state (needs attention) — two
// facts, so one row can answer to both — and compose with the text filter,
// the active one is marked, a miss shows "no held peer matches", Held since and Inactive sort
// client-side with a direction toggle and a marked header, rows with no
// value sort last, and no request leaves the page while doing any of it.
// Mocked mode only; live mode skips it.

import { test, expect, type Page } from "@playwright/test";

const LIVE_URL = process.env.INTAKE_URL || "";

// band = the sort band the template computed (attention outranks kind);
// kind + attention = the two facts the quick filters actually read.
function row(id: string, kind: string, attention: boolean, filter: string, held: string, inactive: string, detail = ""): string {
  const band = attention ? "attention" : kind;
  return `<tbody class="hold-row" data-band="${band}" data-kind="${kind}" data-attention="${attention ? "1" : "0"}" data-filter="${filter}" data-held="${held}" data-inactive="${inactive}">
  <tr><td class="who">${id}</td><td></td><td></td><td></td><td></td><td></td><td></td><td class="hold-action"></td></tr>
  ${detail ? `<tr class="hold-detail"><td colspan="8">${detail}</td></tr>` : ""}
</tbody>`;
}

function fixture(): string {
  return `<!doctype html>
<html><head><meta charset="utf-8"><title>Holds fixture</title></head>
<body>
<p class="holds-summary" id="holds-summary" role="group">
  <button type="button" class="holds-band active" data-band="all" aria-pressed="true">On hold <b>5</b></button>
  <button type="button" class="holds-band" data-band="dormancy" aria-pressed="false">Dormancy <b>3</b></button>
  <button type="button" class="holds-band" data-band="security" aria-pressed="false">Security <b>2</b></button>
  <button type="button" class="holds-band" data-band="attention" aria-pressed="false">Needs attention <b>1</b></button>
</p>
<input type="text" id="holds-filter" autocomplete="off">
<table class="holds" id="holds-table">
  <thead><tr>
    <th>Member</th><th>Device</th><th>Kind</th><th>State</th>
    <th class="sortable" data-sort="held" aria-sort="none"><button type="button">Held since</button></th>
    <th class="sortable" data-sort="inactive" aria-sort="none"><button type="button">Inactive</button></th>
    <th>Before the hold</th><th></th>
  </tr></thead>
  ${row("orphan", "dormancy", true, "kim-lee blue ltd blue peer-kim-lee-blue-zagros-old", "1755000000", "", "record names a peer the mesh does not list")}
  ${row("john", "security", false, "john-roe acme corp acme peer-john-roe-acme-zagros-desktop", "1756900000", "21", "Reason: credential exposure suspected")}
  ${row("norec", "security", false, "peer-nobody-zagros-box", "0", "2")}
  ${row("jane-phone", "dormancy", false, "jane-doe acme corp acme peer-jane-doe-acme-zagros-phone", "1757100000", "19")}
  ${row("jane-laptop", "dormancy", false, "jane-doe acme corp acme peer-jane-doe-acme-zagros-laptop", "1756700000", "13")}
</table>
<p id="holds-miss" hidden>no held peer matches</p>
<script src="/holds.js"></script>
</body></html>`;
}

async function mount(page: Page): Promise<{ urls: string[]; errors: string[] }> {
  const seen = { urls: [] as string[], errors: [] as string[] };
  page.on("pageerror", (e) => seen.errors.push(String(e)));
  page.on("request", (r) => seen.urls.push(r.url()));
  await page.route("**/holds", (route) =>
    route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: fixture() }),
  );
  await page.goto("/holds");
  return seen;
}

// Visible rows, in table order, by their member cell.
async function visibleRows(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll("table.holds tbody.hold-row"))
      .filter((tb) => !(tb as HTMLElement).hidden)
      .map((tb) => (tb.querySelector("td.who") as HTMLElement).textContent || ""),
  );
}

test.describe("mesh holds page script (mocked backend)", () => {
  test.skip(!!LIVE_URL, "INTAKE_URL is set — running live mode instead");

  test("quick filters narrow to a band, compose with the text filter, and mark the active one", async ({ page }) => {
    const seen = await mount(page);
    const requestsAfterLoad = seen.urls.length;

    await page.click('button[data-band="security"]');
    expect(await visibleRows(page)).toEqual(["john", "norec"]);
    await expect(page.locator('button[data-band="security"]')).toHaveClass(/active/);
    await expect(page.locator('button[data-band="security"]')).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator('button[data-band="all"]')).not.toHaveClass(/active/);
    await expect(page.locator('button[data-band="all"]')).toHaveAttribute("aria-pressed", "false");

    // The two filters compose: a band plus a term.
    await page.fill("#holds-filter", "ACME");
    expect(await visibleRows(page)).toEqual(["john"]);
    await expect(page.locator("#holds-miss")).toBeHidden();

    // A term nothing in the band matches is a miss, and the miss is worded.
    await page.fill("#holds-filter", "jane");
    expect(await visibleRows(page)).toEqual([]);
    await expect(page.locator("#holds-miss")).toBeVisible();
    await expect(page.locator("#holds-miss")).toHaveText("no held peer matches");

    // Back to all: the term alone narrows; the details line moves with its row.
    await page.click('button[data-band="all"]');
    expect(await visibleRows(page)).toEqual(["jane-phone", "jane-laptop"]);
    await expect(page.locator("#holds-miss")).toBeHidden();
    await expect(page.locator("tbody.hold-row", { hasText: "credential exposure" })).toBeHidden();

    // Org name, org tag and device name all match; a cleared box restores everything.
    await page.fill("#holds-filter", "blue ltd");
    expect(await visibleRows(page)).toEqual(["orphan"]);
    await page.fill("#holds-filter", "zagros-box");
    expect(await visibleRows(page)).toEqual(["norec"]);
    await page.fill("#holds-filter", "");
    expect(await visibleRows(page)).toEqual(["orphan", "john", "norec", "jane-phone", "jane-laptop"]);

    // Kind and state are two facts: the orphan is a dormancy hold that
    // needs attention, so it answers to Dormancy AND to Needs attention.
    await page.click('button[data-band="dormancy"]');
    expect(await visibleRows(page)).toEqual(["orphan", "jane-phone", "jane-laptop"]);
    await page.click('button[data-band="attention"]');
    expect(await visibleRows(page)).toEqual(["orphan"]);
    await page.click('button[data-band="all"]');

    expect(seen.urls.length).toBe(requestsAfterLoad);
    expect(seen.errors).toEqual([]);
  });

  test("Held since and Inactive sort client-side, toggle direction, mark the header; no value sorts last", async ({ page }) => {
    const seen = await mount(page);
    const requestsAfterLoad = seen.urls.length;
    const held = page.locator('th[data-sort="held"]');
    const inactive = page.locator('th[data-sort="inactive"]');

    await held.locator("button").click();
    expect(await visibleRows(page)).toEqual(["jane-phone", "john", "jane-laptop", "orphan", "norec"]);
    await expect(held).toHaveClass(/sorted/);
    await expect(held).toHaveAttribute("aria-sort", "descending");
    await expect(inactive).toHaveAttribute("aria-sort", "none");

    await held.locator("button").click();
    expect(await visibleRows(page)).toEqual(["orphan", "jane-laptop", "john", "jane-phone", "norec"]);
    await expect(held).toHaveClass(/asc/);
    await expect(held).toHaveAttribute("aria-sort", "ascending");

    await inactive.locator("button").click();
    expect(await visibleRows(page)).toEqual(["john", "jane-phone", "jane-laptop", "norec", "orphan"]);
    await expect(inactive).toHaveClass(/sorted/);
    await expect(inactive).toHaveAttribute("aria-sort", "descending");
    await expect(held).not.toHaveClass(/sorted/);
    await expect(held).toHaveAttribute("aria-sort", "none");

    // A sorted table still filters, and the filter keeps the sort order.
    await page.click('button[data-band="dormancy"]');
    expect(await visibleRows(page)).toEqual(["jane-phone", "jane-laptop", "orphan"]);

    // Headers without data-sort are not sortable: clicking one changes nothing.
    await page.click("th:has-text('Member')");
    await expect(inactive).toHaveClass(/sorted/);

    expect(seen.urls.length).toBe(requestsAfterLoad);
    expect(seen.errors).toEqual([]);
  });
});
