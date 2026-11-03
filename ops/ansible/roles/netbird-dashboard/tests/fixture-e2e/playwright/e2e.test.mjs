import assert from 'node:assert/strict';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';

// Execute the real harness with a deterministic browser double. This checks
// timing and verdicts; the Docker fixture remains the OAuth integration test.
const playwrightDouble = `
let now = 0;
Date.now = () => now;
const advance = ms => { now += ms; };
let urlWaits = 0, functionWaits = 0;
const listeners = {};
globalThis.document = { body: { innerText: 'x'.repeat(420) }, querySelector: () => null };
globalThis.sessionStorage = { getItem: () => JSON.stringify({ tokens: { accessToken: 'fixture.jwt.token' } }) };
const page = {
  on: (name, fn) => { listeners[name] = fn; },
  goto: async url => {
    if (process.env.TEST_FAIL === 'navigation') throw new Error('fixture navigation failed');
    advance(100);
    listeners.request({ method: () => 'GET', url: () => url });
    listeners.response({ status: () => 200, url: () => new URL('/api/instance', url).href, request: () => ({ method: () => 'GET' }) });
  },
  waitForURL: async () => { advance(++urlWaits === 1 ? 100 : 300); },
  waitForSelector: async () => { advance(100); },
  fill: async () => {}, click: async () => {},
  waitForFunction: async fn => {
    if (++functionWaits === 2 && process.env.TEST_FAIL === 'render') throw new Error('fixture render failed');
    advance(functionWaits === 1 ? Number(process.env.TEST_TOKEN_MS || 200) : 700);
    if (!fn()) throw new Error('fixture predicate failed');
  },
  evaluate: async fn => fn(),
  url: () => 'https://dashboard.localhost.test/access-control',
  title: async () => 'Fixture dashboard',
};
export const chromium = { launch: async () => {
  advance(Number(process.env.TEST_SETUP_MS || 0));
  if (process.env.TEST_FAIL === 'startup') throw new Error('fixture browser startup failed');
  return {
    newContext: async () => ({
      tracing: { start: async () => {}, stop: async () => {} },
      newPage: async () => page,
    }),
    close: async () => {},
  };
} };
`;

function runHarness(overrides = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'dashboard-timing-test-'));
  try {
    copyFileSync(new URL('./e2e.mjs', import.meta.url), join(dir, 'e2e.mjs'));
    const dependency = join(dir, 'node_modules/playwright');
    mkdirSync(dependency, { recursive: true });
    writeFileSync(join(dependency, 'package.json'), JSON.stringify({ type: 'module', exports: './index.mjs' }));
    writeFileSync(join(dependency, 'index.mjs'), playwrightDouble);
    const output = join(dir, 'timing.jsonl');
    const result = spawnSync(process.execPath, [join(dir, 'e2e.mjs')], {
      env: {
        PATH: process.env.PATH,
        DASHBOARD_BASE_URL: 'https://dashboard.localhost.test',
        E2E_OPERATOR_EMAIL: 'fixture@example.test', E2E_OPERATOR_PASSWORD: 'fixture-password',
        E2E_OUT_JSONL: output, ...overrides,
      },
      encoding: 'utf8', timeout: 10000,
    });
    assert.ifError(result.error);
    const events = readFileSync(output, 'utf8').trim().split('\n').filter(Boolean).map(JSON.parse);
    return { status: result.status, stderr: result.stderr, events,
      summary: events.find(e => e.event === 'phase_summary'),
      verdict: events.find(e => e.event === 'verdict') };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test('cold browser setup is reported separately from dashboard flow', () => {
  const result = runHarness({ TEST_SETUP_MS: '26000' });
  assert.equal(result.status, 0);
  assert.equal(result.verdict.result, 'PASS');
  assert.equal(result.summary.browser_setup_ms, 26000);
  assert.equal(result.summary.total_duration_ms, 1500);
  const start = result.events.find(e => e.event === 'run_start');
  assert.equal(start.browser_setup_ms, 26000);
  const requests = result.events.find(e => e.event === 'auth_flow_requests').requests;
  assert.equal(requests[0].ts, 100);
});

test('dashboard flow exceeding the total budget still fails and saves a trace', () => {
  const result = runHarness({ TEST_TOKEN_MS: '21000' });
  assert.equal(result.status, 1);
  assert.equal(result.verdict.result, 'HARD_FAIL_TOTAL_BUDGET');
  assert.ok(result.summary.total_duration_ms > result.summary.total_budget_ms);
  assert.ok(result.events.some(e => e.event === 'trace_saved'));
});

test('total budget override remains enforced', () => {
  const result = runHarness({ E2E_TIMEOUT_TOTAL_MS: '1000' });
  assert.equal(result.status, 1);
  assert.equal(result.verdict.result, 'HARD_FAIL_TOTAL_BUDGET');
  assert.equal(result.summary.total_budget_ms, 1000);
});

test('a slow phase within the total budget still returns soft failure', () => {
  const result = runHarness({ TEST_TOKEN_MS: '2000' });
  assert.equal(result.status, 2);
  assert.equal(result.verdict.result, 'SOFT_FAIL_PHASE_BUDGETS');
  assert.deepEqual(result.verdict.phases_over.map(p => p.phase), ['p4_token_exchange']);
});

for (const stage of ['navigation', 'render']) {
  test(stage + ' failure still returns hard failure and saves a trace', () => {
    const result = runHarness({ TEST_FAIL: stage });
    assert.equal(result.status, 1);
    assert.equal(result.verdict.result, 'HARD_FAIL');
    assert.ok(result.events.some(e => e.event === 'trace_saved'));
  });
}

test('browser startup failure remains a failed run', () => {
  const result = runHarness({ TEST_FAIL: 'startup' });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /fixture browser startup failed/);
  assert.equal(result.verdict, undefined);
});
