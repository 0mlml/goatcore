# netbird-dashboard fixture-e2e

Reproducible end-to-end test harness for the per-site Block 21D.1
dashboard wire-up. Brings the full mgmt-host auth stack (postgres-spock
+ Kratos + Hydra + selfservice-ui-node + netbird-mgmt + Traefik +
dashboard) up in throwaway docker containers, drives the OAuth dance
via headless Chromium with Playwright, and captures per-phase latency
for upstream-vs-our-engineering attribution.

## Why this exists

Block 21D.1 first-dogfood surfaced
four upstream-behavior surprises (F-077
/ F-078
/ F-079
/ F-080)
and a ~10-12s end-to-end latency that needs root-cause attribution. A
real-system tunneled-from-Mac measurement is confounded by SSH-tunnel
RTT + operator-laptop noise; an in-container fixture removes both
sources of confound and produces comparable numbers across runs +
across CI.

## Requirements

- `docker compose v2` (built-in to recent Docker Desktop / Docker
  Engine ≥ 20.10).
- `node >= 20` for Playwright.
- ~3-4 GB free RAM during the run (one postgres + four Ory + one
  netbird-mgmt + one dashboard + Traefik + Playwright Chromium).
- ~5-8 minutes wall time on a cold-image cache; ~2-3 minutes warm.

No `aws-vault`, no `ansible-playbook`, no SSH tunnel. Runs equally
on macOS dev laptop, Linux CI runner, or Linux dev VM.

## How to run

```bash
bash ops/ansible/roles/netbird-dashboard/tests/fixture-e2e/run.sh local
```

Exit code 0 on all-green; non-zero on any failure. Per-phase timing
JSONL is emitted to `/tmp/dashboard-e2e-<timestamp>.jsonl` and a
Playwright trace zip to `/tmp/dashboard-e2e-<timestamp>.trace.zip`
on failure (for post-hoc inspection in `npx playwright show-trace`).

## Latency budget (initial; refines with measurement)

The harness measures dashboard flow after browser launch, context creation,
and trace setup. `browser_setup_ms` reports that setup cost separately in
`run_start` and `phase_summary`; `total_duration_ms` and the `perf` mode's
total percentiles measure the dashboard flow. Browser startup failures still
fail the run. Network event offsets also start from the ready browser, so they
align with the dashboard flow. A slow startup remains subject to the CI job timeout.

A phase over budget returns exit code 2; total flow over 20,000 ms returns
exit code 1. `E2E_TIMEOUT_TOTAL_MS` overrides the total budget. Budgets refine
as we accumulate runs.

| Phase | Budget (warm) | Source of latency | Upstream vs ours |
|---|---|---|---|
| Initial GET + redirect + chunks | 1500 ms | dashboard HTTP response + JS chunks | upstream (netbirdio/dashboard) |
| OIDC redirect → Kratos login form | 1500 ms | OAuth redirect + form readiness | upstream (Ory) |
| Login submit + session + redirect chain | 1500 ms | password verification + session creation | mostly upstream |
| Token exchange | 1500 ms | client-side OIDC + Hydra token endpoint | upstream |
| Callback → dashboard render | 5000 ms | callback navigation + React hydration | upstream (netbirdio/dashboard) |
| **Total flow** | **20000 ms** | includes waits and diagnostics after render | |

If the harness measures > budget on any row, that's signal: row
attribution → upstream finding (file F-NNN) or ours (fix). Initial
budgets above are educated guesses calibrated to the vega-A
measurement; first runs will refine.

The timing regression tests execute the harness with a deterministic browser
double. They cover cold setup, total and phase budget failures, the total
budget override, navigation/render failures, and startup failures. They do
not exercise OAuth; the Docker fixture provides that integration coverage.

```bash
node --test ops/ansible/roles/netbird-dashboard/tests/fixture-e2e/playwright/e2e.test.mjs
```

## What it does NOT cover

- Real-cert-trust + macOS-keychain step
  — runs Playwright with `ignoreHTTPSErrors: true`. The cert-trust
  flow is tested in the operator runbook, not the SPA-flow harness.
- Multi-site Spock replication of Kratos identity / Hydra token —
  out of scope for the SPA E2E. Covered separately by the 2-node
  Spock soak driver in `ops/ansible/roles/ory/tests/soak/`.
- Safari coverage — GHA macOS runners can't drive headless Safari
  reliably. Manual dogfood per operator runbook is the Safari-side
  validation today; CI covers Chromium + Firefox.
- Real-fleet-DNS resolution — uses `extra_hosts:` shim to map the
  per-site hostnames to the Traefik container internally. The
  mesh-DNS path is tested by mesh-coredns role's own tests.

## Tier in the test pyramid

Per docs/design/pr-governance.md
and ADR 1015:

- **Tier B** (per-module CI workflow). Triggered on PR touch of the
  netbird-dashboard role, the Ory role, or the dashboard compose
  overlay.
- Sister to the **Tier A** structural test at `../test-structure.py`
  (no docker, runs in <1s, asserts YAML structure).
- Composes with **Tier E** ADR 1015 block-completion review (the
  fixture's results are evidence in the review's Section B Row 4
  "Test coverage per category").
