// SPDX-License-Identifier: Apache-2.0
//
// Playwright config for the goat-zenoh-portal static web UI. Backend is
// NOT required — specs mock /api/* via page.route() against the static
// files served by serve.mjs. See tests/topic-form.spec.ts.

import { defineConfig, devices } from "@playwright/test";

const PORT = Number(process.env.PORT) || 8788;
const BASE_URL = `http://127.0.0.1:${PORT}`;

export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: "list",
  use: {
    baseURL: BASE_URL,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "node serve.mjs",
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
