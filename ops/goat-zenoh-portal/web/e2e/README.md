# goat-zenoh-portal — web e2e

Playwright e2e for the portal's static web UI (`../static/`). The portal
serves those files directly from the Go binary with no build step, so
these tests run against a tiny stdlib static server (`serve.mjs`) and
mock `/api/*` via `page.route` — **no Go backend, DB, or Zenoh fabric
required**.

First spec (`tests/topic-form.spec.ts`) guards the topic-registration
protobuf footgun: the format dropdown must not let
the literal `protobuf:MyMessage` placeholder be submitted, and a real
message name must be spliced into `protobuf:<name>`.

## Run locally

```bash
cd ops/goat-zenoh-portal/web/e2e
npm ci
npx playwright install chromium
npm test
```

`npm test` starts `serve.mjs` automatically (Playwright `webServer`) and
tears it down after. To poke the page by hand: `npm run serve` then open
http://127.0.0.1:8788/topics.html.

## CI

`.github/workflows/portal-web-e2e.yml` runs this on PRs touching
`ops/goat-zenoh-portal/web/**`. Mirrors the panoscope-frontend gate.

## Adding tests

Mock only what the page fetches on init (`/api/orgs`, the topics
GET) plus whatever the interaction under test calls. Keep specs backend-
free so the gate stays a pure frontend regression check.
