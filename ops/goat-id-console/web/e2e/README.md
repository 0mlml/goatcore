# goat-id-console web e2e — the candidate intake form

Playwright drives `web/static/onboard.html`, the page a candidate opens from
their invite email, in a real Chromium. It exists because on 3 Sep 2026 a markup
rename (#569) left the page's submit handler reading a control that no longer
existed; the handler threw, "Submit for review" did nothing, and every Go test
was green (#601). Only a browser running the page's script against the page's
markup catches that class, so every spec here fails on any uncaught page error.

## Mocked mode (CI, `id-console-web-e2e.yml`)

```
npm ci && npx playwright install --with-deps chromium && npm test
```

`serve.mjs` serves `web/static/`; the spec stubs `/api/onboard/preview` and
`/api/onboard/submit` with `page.route`, so no console binary, DB or Kratos is
needed. Assertions are about what the page sends: the renamed controls,
nationality required for a person and not for a system, the error path.

## The review page's scripts (`tests/onboarding-find.spec.ts`)

`/onboarding` is a Go template, so the spec serves a fixture holding only the
markup its two scripts read (the ids and classes the Go template test pins from
the other side) and loads the real `onboard-queue.js` and `onboard-find.js`
from `web/static/`. With the clock faked it drives the poll past a count that
moves mid-search, and pins that the reload waits for the find box to clear and
then fires once; and that the find term lives in the URL fragment and never
reaches a request. Mocked mode only; live mode skips it.

## The Archived card's Restore button (`tests/onboarding-restore.spec.ts`)

`/onboarding` is a Go template and Restore is a redirect, a CSRF check and a
guarded store write, so this spec drives a REAL console: `playwright.config.ts`
builds `../../cmd/console` with `go run` and starts it against throwaway
SQLite files with `DEV_BYPASS_AUTH` (no Kratos, Hydra, mailer or goatnet); the
admin session is the oauth2-proxy header pair the console reads. Each test
seeds its own archived person through the operator API the runner uses, then
pins the confirm's facts (destination email, goatnet, old key dead), that
cancel changes nothing, that confirming lands on the same row in Approved with
the banner and the "Restored by" mark, and that a stale click on a row somebody
else restored meanwhile is refused with a banner naming its current stage
(goat#1037). `CONSOLE_URL` + `CONSOLE_API_KEY` point it at a console already
running; `CONSOLE_E2E=0` skips it; `CONSOLE_E2E_LOG=1` shows the console's log
when it will not come up. Live mode skips it.

## Group persistence (`tests/groups-save.spec.ts`)

The identity fixture enforces Kratos v1.3.1's supported group-patch shape. The
spec submits the real user-detail form with session revocation selected, follows
the redirect, and verifies that the changed groups came back from the persisted
fixture identity rather than merely observing a successful form submission.

## Live mode (a real goatnet)

```
INTAKE_URL='https://intake.<goatnet>/onboard.html?t=oti_…' npm test
```

No stubs; the goatnet must answer 201. Do not run this by hand — use
`ops/scripts/onboard-intake-smoke.sh`, which mints the invite, runs this spec,
and rejects the proposal it creates so nothing is left in the queue.
