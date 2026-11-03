import { test, expect, type APIRequestContext } from "@playwright/test";
import { CONSOLE_URL, CONSOLE_API_KEY } from "../playwright.config";

const LIVE = !!process.env.INTAKE_URL;
const SKIP = LIVE || process.env.CONSOLE_E2E === "0";
const RUNNER = { Authorization: `Bearer ${CONSOLE_API_KEY}`, "Content-Type": "application/json" };

async function api(request: APIRequestContext, method: "post" | "get", path: string, data?: unknown) {
  const res = await request[method](CONSOLE_URL + path, { headers: RUNNER, data });
  expect(res.ok(), `${method.toUpperCase()} ${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
  return res.json();
}

async function seedOnboarded(request: APIRequestContext) {
  const tag = `${Date.now()}`;
  const email = `identity.${tag}@partner.example`;
  const { token } = await api(request, "post", "/api/onboard/invite", { env: "zagros", tier: "developer", affiliation: "partner" });
  const submit = await request.post(CONSOLE_URL + "/public/onboard/submit", {
    data: { token, submission: { name: "Identity Test", email, organization: "Identity Org", nationality: "CAN" } },
  });
  expect(submit.status(), await submit.text()).toBe(201);
  const { proposal_id: id } = await submit.json();
  for (const action of ["sponsor-approve", "approve", "claim"]) {
    await api(request, "post", `/api/onboard/proposals/${id}/${action}`, {});
  }
  await api(request, "post", `/api/onboard/proposals/${id}/result`, { status: "onboarded", setup_key: "nbs-unused-test-key" });
  const row = await api(request, "get", `/api/onboard/proposals/${id}`);
  return { email, handle: row.handle as string };
}

test("the real identity page copies the exact canonical handle", async ({ page, request, context }) => {
  test.skip(SKIP, "needs the local console (mocked mode only)");
  const person = await seedOnboarded(request);
  await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: CONSOLE_URL });
  await page.setExtraHTTPHeaders({ "X-Forwarded-Email": person.email });
  await page.goto(CONSOLE_URL + "/identity");

  await expect(page.getByRole("heading", { name: "Your identity" })).toBeVisible();
  await expect(page.getByLabel("Your handle")).toHaveValue(person.handle);
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Copied");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(person.handle);
});

test("the neutral copy row reports clipboard refusal", async ({ page }) => {
  await page.goto("/onboard.html");
  await page.evaluate(() => {
    document.body.innerHTML = `
      <div class="copyrow" data-copy-row>
        <input id="identity-handle" readonly value="unit-casey.exact">
        <button type="button" data-copy-button data-copy-source="identity-handle">Copy</button>
        <span role="status" data-copy-status></span>
      </div>`;
    Object.defineProperty(navigator, "clipboard", { value: { writeText: async () => {
      throw new Error("denied");
    } } });
  });
  await page.addScriptTag({ url: "/copy-row.js" });
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Copy failed. Select the value and copy it manually.");
});
