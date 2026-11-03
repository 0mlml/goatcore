// ops/ory-consent/tests/double-submit-guard.test.js
//
// Behavioural test of the F-341 double-submit guard. Runs on bare node — no
// browser, no jsdom, no network — so it is cheap enough to run on every PR.
//
// verify-patched.sh proves the guard is PRESENT in the built image by grepping
// for its markers. Presence is not correctness: the properties that actually
// matter cannot be grepped for.
//
// Run:  node ops/ory-consent/tests/double-submit-guard.test.js
//
// The guard under test is READ OUT OF apply-view-patch.sh, not copied here. It
// used to be a hand-kept copy, run against a `document` stub that remembered
// ONE listener — so the test could not see the defect it now pins:
//
//   The scripts partial is not included once per page. consent.hbs includes it
//   AND renders inside layouts/auth.hbs, which includes it again. With two
//   copies of the listener, both saw the same submit event: the first marked
//   the form, the second found the mark and cancelled the FIRST submission.
//   "Allow" on the consent page sent nothing: the consent page rendered 200
//   twice for one challenge and no POST /consent left the browser. It stays
//   unseen where every other client is first-party with skip_consent, since
//   the Allow page is never shown there.
//
// Each scenario checks the same four properties:
//   1. the FIRST submit goes through
//   2. the SECOND submit of the same form is prevented
//   3. the submit control is NOT disabled synchronously (which would drop
//      method=password from the form body and break every login)
//   4. the control IS disabled after the macrotask
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const src = fs.readFileSync(path.join(__dirname, "..", "apply-view-patch.sh"), "utf8");
const m = src.match(/<script>([\s\S]*?)<\/script>/);
if (!m) {
  console.log("  FAIL  could not find the guard <script> in apply-view-patch.sh");
  process.exit(1);
}
const guard = m[1];

// One page: a document that keeps EVERY capture listener, like a real one, and
// a dispatch that runs them in order and honours stopImmediatePropagation.
function page(includes) {
  const listeners = [];
  const timers = [];
  const button = { _a: {}, style: {}, setAttribute(n, v) { this._a[n] = v; } };
  const form = {
    tagName: "FORM",
    _attrs: {},
    hasAttribute(n) { return n in this._attrs; },
    setAttribute(n, v) { this._attrs[n] = v; },
    querySelectorAll() { return [button]; },
  };
  const window = { setTimeout: (fn) => timers.push(fn) };
  const document = { addEventListener: (t, fn) => { if (t === "submit") listeners.push(fn); } };
  const ctx = vm.createContext({ window, document });
  for (let i = 0; i < includes; i++) vm.runInContext(guard, ctx);

  const submit = () => {
    const ev = {
      target: form, prevented: false, stopped: false,
      preventDefault() { this.prevented = true; },
      stopImmediatePropagation() { this.stopped = true; },
    };
    for (const fn of listeners) { if (ev.stopped) break; fn(ev); }
    return ev;
  };
  return { submit, button, flush: () => timers.splice(0).forEach((fn) => fn()), listeners };
}

let ok = true;
const check = (name, pass) => { console.log(`  ${pass ? "PASS" : "FAIL"}  ${name}`); if (!pass) ok = false; };

for (const [label, includes] of [
  ["partial included once (login page)", 1],
  ["partial included twice (consent page: view + layout)", 2],
]) {
  console.log(label);
  const p = page(includes);
  const first = p.submit();
  const disabledSync = p.button._a["aria-disabled"] === "true";
  const second = p.submit();
  p.flush();
  const disabledAfter = p.button._a["aria-disabled"] === "true";
  check("one listener registered, however many includes", p.listeners.length === 1);
  check("first submit allowed through", first.prevented === false);
  check("second submit PREVENTED", second.prevented === true);
  check("control NOT disabled synchronously", disabledSync === false);
  check("control disabled after the macrotask", disabledAfter === true);
}

console.log(ok ? "guard behaviour: OK" : "guard behaviour: BROKEN");
process.exit(ok ? 0 : 1);
