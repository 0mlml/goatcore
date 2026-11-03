import { test, expect, type Page } from "@playwright/test";

async function mockShell(page: Page) {
  await page.addInitScript(() => localStorage.setItem("portalApiKey", "vp_test"));
  await page.route("**/api/branding", (r) => r.fulfill({ json: { product_name: "Test Fabric", org: "slot", orgs: "slots" } }));
  await page.route("**/api/whoami", (r) => r.fulfill({ json: { kind: "vendor", visible_orgs: 4 } }));
}

test("Home formats wire uptime and orders names with prefix fallback and tie-break", async ({ page }) => {
  await mockShell(page);
  await page.route("**/api/health", (r) => r.fulfill({ json: { version: "0.2.0", uptime: "187h44m28.847029146s" } }));
  await page.route("**/api/orgs", (r) => r.fulfill({ json: { orgs: [
    { prefix: "0000", display_name: "Zulu", created_at: 0 },
    { prefix: "ffff", display_name: "Alpha", created_at: 0 },
    { prefix: "cccc", display_name: "Alpha", created_at: 0 },
    { prefix: "bbbb", display_name: "", created_at: 0 },
  ] } }));
  await page.goto("/index.html");
  await expect(page.locator("#health")).toHaveText("portal v0.2.0 • uptime 7 d 19 h 44 min");
  await expect(page.locator("#vendors-table tbody tr td:first-child code")).toHaveText(["cccc", "ffff", "bbbb", "0000"]);
});

test("unknown uptime is explicit and health metadata remains text", async ({ page }) => {
  await mockShell(page);
  await page.route("**/api/health", (r) => r.fulfill({ json: { version: "<b>example</b>", uptime: "bad duration" } }));
  await page.route("**/api/orgs", (r) => r.fulfill({ json: { orgs: [] } }));
  await page.goto("/index.html");
  await expect(page.locator("#health")).toHaveText("portal v<b>example</b> • uptime unavailable");
  await expect(page.locator("#health b")).toHaveCount(0);
});

test("schema download follows the selected schema and version", async ({ page }) => {
  await mockShell(page);
  let storage = "xsd";
  let language = "xsd";
  await page.route("**/api/schemas/render/**", (r) => r.fulfill({ json: {
    key: "slot/topic/v1", format: storage === "xsd" ? "xml:Track" : "protobuf:Track",
    vendor: "slot", schema: { version: "v2", format_storage: storage, all_versions: ["v1", "v2"] },
    source_lang: language, rendered: { files: [], messages: [], enums: [] }, proto_source: {}, examples: [],
  } }));
  await page.goto("/schema.html?key=slot/topic/v1");
  await expect(page.locator("#m-download")).toHaveText("Download .xsd");
  await expect(page.locator("#m-download")).toHaveAttribute("href", "/api/schemas/slot/topic/v1?version=v2");
  storage = "protobuf-fds";
  language = "proto";
  await page.click("#load-schema");
  await expect(page.locator("#m-download")).toHaveText("Download .fds");
  storage = "future-format";
  await page.click("#load-schema");
  await expect(page.locator("#m-download")).toHaveText("Download schema");
});

test("shared copy button confirms without an exclamation and resets", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", { value: { writeText: async () => {} } });
  });
  await page.route("**/api/branding", (r) => r.fulfill({ json: {} }));
  await page.route("**/api/health", (r) => r.fulfill({ json: { uptime: "0s", version: "test" } }));
  await page.goto("/docs.html");
  // Exercise the same data-copy-target contract used by the Home success card.
  await page.evaluate(async () => {
    const source = document.createElement("code"); source.id = "copy-fixture"; source.textContent = "example";
    const button = document.createElement("button"); button.textContent = "Copy";
    button.className = "copy-btn"; button.dataset.copyTarget = source.id;
    document.body.append(source, button);
    const { wireCopyButtons } = await import("/app.js");
    wireCopyButtons();
  });
  const button = page.locator('button[data-copy-target="copy-fixture"]');
  await button.click();
  await expect(button).toHaveText("Copied");
  await expect(button).toBeDisabled();
  await expect(button).toHaveText("Copy");
  await expect(button).toBeEnabled();
});
