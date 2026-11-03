// Playwright E2E + perf harness for the Block 21D.1 netbird-dashboard
// wire-up. Drives the full OAuth dance via headless Chromium, asserts
// the dashboard renders past the loading spinner, and captures a
// per-phase latency waterfall to JSONL so we can attribute slowness to
// upstream-netbird vs our-engineering.
//
// Runs against any reachable dashboard URL: live vega-A via SSH tunnel,
// the local fixture (../docker-compose.yml), or any staged dashboard.
//
// Required env vars:
//   DASHBOARD_BASE_URL      e.g. https://dashboard.vega.netbird-prod.goat.example
//   E2E_OPERATOR_EMAIL      Kratos identity email
//   E2E_OPERATOR_PASSWORD   Kratos identity password
//
// Optional env vars:
//   E2E_HEADED              "true" to launch headed (manual debugging)
//   E2E_OUT_JSONL           override output path (default: /tmp/dashboard-e2e-<ts>.jsonl)
//   E2E_TRACE_ON_FAIL       "false" to skip Playwright trace capture
//   E2E_TIMEOUT_TOTAL_MS    fail the run if total flow exceeds this (default 20000)
//   E2E_IGNORE_CERT_ERRORS  "false" to require valid certs (default true; set false in
//                           CI runs where the fixture provisions a real-CA-signed leaf)
//
// Exit codes:
//   0  all-pass + every phase under budget
//   1  hard-fail (dashboard didn't render OR total budget exceeded OR auth chain broke)
//   2  soft-fail (rendered + total under budget, but one or more per-phase budgets exceeded)
//
// Soft-fail is intentional: per-phase budgets are calibration targets,
// not strict invariants. Trip is signal worth investigating; not every
// trip is a regression.

import { chromium } from 'playwright';
import { writeFileSync, appendFileSync } from 'node:fs';

const env = (name, def) => {
  const v = process.env[name];
  return v === undefined ? def : v;
};
const envBool = (name, def) => {
  const v = env(name, def);
  return v === true || v === 'true' || v === '1';
};

const BASE_URL = env('DASHBOARD_BASE_URL');
if (!BASE_URL) {
  console.error('FATAL: DASHBOARD_BASE_URL env var required');
  process.exit(1);
}
const EMAIL = env('E2E_OPERATOR_EMAIL');
const PASSWORD = env('E2E_OPERATOR_PASSWORD');
if (!EMAIL || !PASSWORD) {
  console.error('FATAL: E2E_OPERATOR_EMAIL + E2E_OPERATOR_PASSWORD env vars required');
  process.exit(1);
}

const TIMESTAMP = new Date().toISOString().replace(/[:.]/g, '-');
const OUT_JSONL = env('E2E_OUT_JSONL', `/tmp/dashboard-e2e-${TIMESTAMP}.jsonl`);
const TRACE_PATH = `/tmp/dashboard-e2e-${TIMESTAMP}.trace.zip`;
const HEADED = envBool('E2E_HEADED', false);
const IGNORE_CERTS = envBool('E2E_IGNORE_CERT_ERRORS', true);
const TOTAL_BUDGET_MS = parseInt(env('E2E_TIMEOUT_TOTAL_MS', '20000'), 10);
const TRACE_ON_FAIL = envBool('E2E_TRACE_ON_FAIL', true);

// Per-phase budgets in ms. Calibrated to vega-A dogfood
// observation (~10-12s end-to-end real-browser); refine as we accumulate
// CI-runner-shape measurements. Each budget is a signal threshold, not
// a strict gate: exceeded budget = soft-fail (exit 2).
const PHASE_BUDGETS = {
  'p1_initial_get': 1500,           // GET / → 302 → /access-control + JS chunks
  'p2_oidc_to_login': 1500,         // /access-control mount → OAuth redirect → Kratos /login form
  'p3_kratos_session_create': 1500, // POST login → Kratos session + redirect chain
  'p4_token_exchange': 1500,        // /peers?code= → POST /oauth2/token success
  'p5_callback_to_render': 5000,    // window.location to /access-control → React hydration → first render
};

// JSONL emit helper.
writeFileSync(OUT_JSONL, '');
const emit = (event) => {
  const line = JSON.stringify({ ts: new Date().toISOString(), ...event });
  console.log(line);
  appendFileSync(OUT_JSONL, line + '\n');
};

// ----------------------------------------------------------------------
// Run

const browserSetupStartedAt = Date.now();
const browser = await chromium.launch({
  headless: !HEADED,
  channel: 'chrome',
  args: IGNORE_CERTS
    ? ['--ignore-certificate-errors', '--allow-insecure-localhost']
    : [],
});
const ctx = await browser.newContext({ ignoreHTTPSErrors: IGNORE_CERTS });

if (TRACE_ON_FAIL) await ctx.tracing.start({ screenshots: true, snapshots: true, sources: true });

const page = await ctx.newPage();
const browserSetupMs = Date.now() - browserSetupStartedAt;
// The dashboard budget starts with a ready browser, immediately before
// navigation. Cold browser startup is runner overhead, reported separately.
const t0 = Date.now();

// Network-event capture for waterfall analysis.
const allRequests = [];
const allResponses = [];
const allFailures = [];
page.on('request', req => allRequests.push({
  ts: Date.now() - t0, method: req.method(), url: req.url(),
}));
page.on('response', resp => allResponses.push({
  ts: Date.now() - t0, status: resp.status(), url: resp.url(),
  request_method: resp.request().method(),
}));
page.on('requestfailed', req => allFailures.push({
  ts: Date.now() - t0, url: req.url(), failure: req.failure()?.errorText,
}));
page.on('console', msg => {
  if (msg.type() === 'error' || msg.type() === 'warning') {
    emit({ event: 'browser_log', level: msg.type(), text: msg.text() });
  }
});
page.on('pageerror', err => emit({ event: 'browser_pageerror', text: err.toString() }));

emit({ event: 'run_start', dashboard_base_url: BASE_URL, headed: HEADED, ignore_certs: IGNORE_CERTS,
  browser_setup_ms: browserSetupMs });

const phaseLog = {};
const phaseStart = (name) => { phaseLog[name] = { start: Date.now() }; };
const phaseEnd = (name) => {
  if (!phaseLog[name]) throw new Error(`phase '${name}' was not started`);
  phaseLog[name].end = Date.now();
  phaseLog[name].duration_ms = phaseLog[name].end - phaseLog[name].start;
  emit({ event: 'phase_end', phase: name, duration_ms: phaseLog[name].duration_ms,
    budget_ms: PHASE_BUDGETS[name], over_budget: phaseLog[name].duration_ms > (PHASE_BUDGETS[name] || Infinity) });
};

let hardFail = null;

try {
  // ===== Phase 1: GET / → 302 → /access-control + chunks load =====
  phaseStart('p1_initial_get');
  await page.goto(BASE_URL + '/', { waitUntil: 'domcontentloaded', timeout: 30000 });
  phaseEnd('p1_initial_get');

  // ===== Phase 2: OIDC redirect → Kratos /login form =====
  phaseStart('p2_oidc_to_login');
  await page.waitForURL(/\/login\?flow=/, { timeout: 30000 });
  // Wait for input to be present (form ready, not just URL change).
  await page.waitForSelector('input[name="identifier"]', { timeout: 10000 });
  phaseEnd('p2_oidc_to_login');

  // ===== Phase 3: submit login → Kratos session create + redirect chain to /peers?code= =====
  phaseStart('p3_kratos_session_create');
  await page.fill('input[name="identifier"]', EMAIL);
  await page.fill('input[name="password"]', PASSWORD);
  await Promise.all([
    page.waitForURL(u => !u.toString().includes('/login?flow=') && !u.toString().includes('/consent'), { timeout: 30000 }),
    page.click('button[type="submit"]'),
  ]);
  phaseEnd('p3_kratos_session_create');

  // ===== Phase 4: token exchange (POST /oauth2/token from /peers?code=) =====
  phaseStart('p4_token_exchange');
  // The token exchange happens client-side after the dashboard JS mounts on
  // /peers and detects the code in the URL. We wait for the OIDC library
  // to either store tokens in sessionStorage OR navigate to callbackPath.
  await page.waitForFunction(() => {
    const oidc = sessionStorage.getItem('oidc.default');
    if (!oidc) return false;
    try {
      const parsed = JSON.parse(oidc);
      return !!(parsed.tokens && parsed.tokens.accessToken);
    } catch { return false; }
  }, { timeout: 30000 });
  phaseEnd('p4_token_exchange');

  // ===== Phase 5: callback to fully-rendered dashboard =====
  phaseStart('p5_callback_to_render');
  // Wait for the dashboard's React tree to hydrate (non-empty body) AND
  // the loading spinner to be gone.
  await page.waitForFunction(() => {
    const spinner = document.querySelector('svg.fill-netbird');
    const len = document.body.innerText.length;
    return !spinner && len > 200;
  }, { timeout: 30000 });
  phaseEnd('p5_callback_to_render');

  emit({ event: 'render_state', body_text_len: await page.evaluate(() => document.body.innerText.length),
    final_url: page.url(), title: await page.title() });

  // Token characteristics — useful for upstream/ours attribution.
  const tokenAnalysis = await page.evaluate(() => {
    const oidc = JSON.parse(sessionStorage.getItem('oidc.default') || '{}');
    const at = oidc.tokens?.accessToken || '';
    const isJWT = at.split('.').length === 3 && !at.startsWith('ory_at_');
    return {
      access_token_is_jwt: isJWT,
      has_id_token: !!oidc.tokens?.idToken,
      has_refresh_token: !!oidc.tokens?.refreshToken,
      scope: oidc.tokens?.scope ?? null,
      expires_in: oidc.tokens?.expiresIn ?? null,
    };
  });
  emit({ event: 'token_analysis', ...tokenAnalysis });

  // Navigation timing (browser-side). Captures DNS, connect, TTFB, etc.
  const navTiming = await page.evaluate(() => {
    const t = performance.getEntriesByType('navigation')[0];
    if (!t) return null;
    return {
      dns_ms: t.domainLookupEnd - t.domainLookupStart,
      connect_ms: t.connectEnd - t.connectStart,
      tls_ms: t.connectEnd - t.secureConnectionStart,
      ttfb_ms: t.responseStart - t.requestStart,
      response_download_ms: t.responseEnd - t.responseStart,
      dom_interactive_ms: t.domInteractive - t.responseEnd,
      dom_content_loaded_ms: t.domContentLoadedEventEnd - t.domContentLoadedEventStart,
      load_complete_ms: t.loadEventEnd - t.loadEventStart,
    };
  });
  if (navTiming) emit({ event: 'final_page_nav_timing', ...navTiming });

} catch (e) {
  hardFail = e;
  emit({ event: 'hard_fail', error: e.message, stack: e.stack });
}

const totalDurationMs = Date.now() - t0;
emit({ event: 'phase_summary',
  phases: Object.fromEntries(Object.entries(phaseLog).map(([k, v]) => [k, { duration_ms: v.duration_ms, budget_ms: PHASE_BUDGETS[k], over_budget: v.duration_ms > (PHASE_BUDGETS[k] || Infinity) }])),
  total_duration_ms: totalDurationMs,
  browser_setup_ms: browserSetupMs,
  total_budget_ms: TOTAL_BUDGET_MS,
});

// Auth-flow network requests (for upstream-vs-ours waterfall analysis).
const authFlow = allResponses.filter(r =>
  /\/oauth2\/(auth|token)|\/self-service\/login|\/consent|\/api\/instance|\/api\/users|\/api\/peers|\/.well-known\/(openid-configuration|jwks\.json)/.test(r.url)
);
emit({ event: 'auth_flow_requests', count: authFlow.length, requests: authFlow });

if (allFailures.length > 0) {
  emit({ event: 'request_failures', count: allFailures.length, failures: allFailures });
}

// Decide outcome.
if (TRACE_ON_FAIL && (hardFail || totalDurationMs > TOTAL_BUDGET_MS)) {
  await ctx.tracing.stop({ path: TRACE_PATH });
  emit({ event: 'trace_saved', path: TRACE_PATH });
}
await browser.close();

if (hardFail) {
  emit({ event: 'verdict', result: 'HARD_FAIL', reason: hardFail.message });
  process.exit(1);
}
if (totalDurationMs > TOTAL_BUDGET_MS) {
  emit({ event: 'verdict', result: 'HARD_FAIL_TOTAL_BUDGET', total_ms: totalDurationMs, budget_ms: TOTAL_BUDGET_MS });
  process.exit(1);
}

const overBudgetPhases = Object.entries(phaseLog).filter(([k, v]) => v.duration_ms > (PHASE_BUDGETS[k] || Infinity));
if (overBudgetPhases.length > 0) {
  emit({ event: 'verdict', result: 'SOFT_FAIL_PHASE_BUDGETS', phases_over: overBudgetPhases.map(([k, v]) => ({ phase: k, duration_ms: v.duration_ms, budget_ms: PHASE_BUDGETS[k] })) });
  process.exit(2);
}

emit({ event: 'verdict', result: 'PASS', total_ms: totalDurationMs });
process.exit(0);
