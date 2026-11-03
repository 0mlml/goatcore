// SPDX-License-Identifier: Apache-2.0
//
// E2E spec for the Archived card's "Restore and resend key" button on
// /onboarding (goat#1037), driven against a REAL console: the Go templates,
// the SQLite store, the CSRF check and the redirect are the things under
// test, and none of them exist in a static fixture. playwright.config.ts
// builds and starts the console (DEV_BYPASS_AUTH, throwaway DBs, no Kratos);
// the admin session is the oauth2-proxy header pair the console reads.
//
// Each test seeds its own person through the operator API the runner uses —
// invite → submit → sponsor-approve → approve → claim → result(onboarded) →
// archive — so it starts from the state the dormancy sweep leaves behind.
//
// Pinned: the confirm names the destination email and the goatnet and says
// the old key is dead and a fresh one will be emailed; cancelling changes
// nothing; confirming lands on the same row in Approved with the banner and
// the "Restored by" mark; a stale click on a row somebody else restored
// meanwhile is refused with a banner naming its current stage.
//
// Mocked mode only (it needs the local console); live mode skips it.

import { test, expect, type APIRequestContext, type Page } from "@playwright/test";
import { CONSOLE_URL, CONSOLE_API_KEY } from "../playwright.config";

const LIVE = !!process.env.INTAKE_URL;
const SKIP = LIVE || process.env.CONSOLE_E2E === "0";

const ADMIN = { "X-Forwarded-Groups": "PLATFORM-ADMIN", "X-Forwarded-Email": "admin@example.test" };
const RUNNER = { Authorization: `Bearer ${CONSOLE_API_KEY}`, "Content-Type": "application/json" };

test.use({ baseURL: CONSOLE_URL, extraHTTPHeaders: ADMIN, viewport: { width: 1366, height: 768 } });
test.skip(SKIP, "needs the local console (mocked mode only)");

type Person = { id: string; email: string; handle: string };

async function api(request: APIRequestContext, method: "post" | "get", path: string, data?: unknown) {
  const res = await request[method](CONSOLE_URL + path, { headers: RUNNER, data });
  expect(res.ok(), `${method.toUpperCase()} ${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
  return res.json();
}

// The state the ADR 1136 sweep leaves: onboarded, key never used, archived.
async function seedArchived(request: APIRequestContext, tag: string): Promise<Person> {
  const email = `pat.${tag}.${Date.now()}@partner.example`;
  const { token } = await api(request, "post", "/api/onboard/invite", { env: "zagros", tier: "developer", affiliation: "partner" });
  const submit = await request.post(CONSOLE_URL + "/public/onboard/submit", {
    data: { token, submission: { name: `Pat ${tag}`, email, organization: "Restore Org", nationality: "CAN" } },
  });
  expect(submit.status(), await submit.text()).toBe(201);
  const { proposal_id: id } = await submit.json();
  for (const a of ["sponsor-approve", "approve", "claim"]) await api(request, "post", `/api/onboard/proposals/${id}/${a}`, {});
  await api(request, "post", `/api/onboard/proposals/${id}/result`, { status: "onboarded", setup_key: "nbs-dead-key-never-shown" });
  await api(request, "post", `/api/onboard/proposals/${id}/archive`, { reason: "key expired unredeemed" });
  const row = await api(request, "get", `/api/onboard/proposals/${id}`);
  expect(row.status).toBe("archived");
  return { id, email, handle: row.handle };
}

async function statusOf(request: APIRequestContext, id: string): Promise<string> {
  return (await api(request, "get", `/api/onboard/proposals/${id}`)).status;
}

function restoreForm(page: Page, id: string) {
  return page.locator(`form[action="/onboarding/${id}/restore"]`);
}

test("the Archived card leads with Restore; the confirm names email + goatnet; cancel changes nothing", async ({ page, request }) => {
  const p = await seedArchived(request, "cancel");
  await page.goto(`/onboarding?status=archived&id=${p.id}`);

  await expect(page.locator("h3", { hasText: "Archived · key never used" })).toBeVisible();
  const restore = restoreForm(page, p.id).getByRole("button", { name: /restore .* and resend key/i });
  await expect(restore).toBeVisible();
  await expect(restore).toHaveClass(/primary/);
  // Restore is the first action; Mark offboarded is below it.
  const offboard = page.locator(`form[action="/onboarding/${p.id}/offboard"] button[type=submit]`);
  const [rBox, oBox] = [await restore.boundingBox(), await offboard.boundingBox()];
  expect(rBox && oBox && rBox.y < oBox.y, "Restore must sit above Mark offboarded").toBeTruthy();
  // The hand-written POST and the CLI command are gone from the note.
  const detail = await page.locator(".ob-detail").innerText();
  expect(detail).not.toContain("operator API");
  expect(detail).not.toContain("/api/onboard/proposals/");
  expect(detail).not.toContain("nbs-dead-key-never-shown");

  let confirmText = "";
  page.once("dialog", async (d) => {
    confirmText = d.message();
    await d.dismiss();
  });
  await restore.click();
  expect(confirmText).toContain(p.email);
  expect(confirmText).toContain("minted on zagros");
  expect(confirmText).toContain("The old setup key is dead");
  expect(confirmText).toContain("welcome package emailed to");
  expect(confirmText).not.toContain("nbs-dead-key-never-shown");

  // Cancelled: still on the Archived card, row untouched, button still live.
  await expect(page).toHaveURL(new RegExp(`status=archived&id=${p.id}$`));
  await expect(restore).toBeEnabled();
  expect(await statusOf(request, p.id)).toBe("archived");
});

test("confirming restores: lands on the row in Approved with the banner and the Restored-by mark", async ({ page, request }) => {
  const p = await seedArchived(request, "restore");
  await page.goto(`/onboarding?status=archived&id=${p.id}`);
  page.once("dialog", (d) => d.accept());
  await restoreForm(page, p.id).getByRole("button").click();

  await expect(page).toHaveURL(new RegExp(`/onboarding\\?flash=restored&id=${p.id}&status=approved$`));
  const banner = page.locator(".flash.success");
  await expect(banner).toContainText(`Restored. A fresh key will be minted and the welcome package sent to ${p.email}.`);
  await expect(banner).toContainText("Follow this row through Provisioning to Onboarded.");
  await expect(page.locator(".ob-tabs a.on")).toHaveText(/Approved/);
  await expect(page.locator(".ob-restored")).toContainText("Restored by admin@example.test at 20");
  expect(await statusOf(request, p.id)).toBe("approved");
  // The banner and the row never carry a key value.
  const body = await page.locator("body").innerText();
  expect(body).not.toContain("nbs-dead-key-never-shown");
});

test("a stale click on a row somebody else restored meanwhile is refused, naming its current stage", async ({ page, request }) => {
  const p = await seedArchived(request, "race");
  await page.goto(`/onboarding?status=archived&id=${p.id}`);
  // The other admin (here: the operator API) gets there first.
  await api(request, "post", `/api/onboard/proposals/${p.id}/restore`);
  page.once("dialog", (d) => d.accept());
  await restoreForm(page, p.id).getByRole("button").click();

  await expect(page).toHaveURL(new RegExp(`/onboarding\\?flash=restore_state&id=${p.id}&status=approved$`));
  await expect(page.locator(".flash.error")).toContainText("Not restored: this record is no longer archived — it is in Approved now");
  await expect(page.locator(".flash.error")).toContainText("Nothing was changed.");
  expect(await statusOf(request, p.id)).toBe("approved");
});

test("the Invited tab's archived section says dead links are not people and cannot be restored", async ({ page }) => {
  await page.goto("/onboarding?status=invited");
  const details = page.locator("details.ob-archive");
  await details.locator("summary").click();
  await expect(details.locator(".ob-archive-note")).toContainText("dead invite links, not people: a link here cannot be restored");
  await expect(details.locator(".ob-archive-note a[href='/onboarding?status=archived']")).toHaveText("Archived");
});
