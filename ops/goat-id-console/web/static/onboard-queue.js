// Onboarding queue: row ages + live gate badges + self-refresh.
//
// The queue was a page you had to remember to reload. Two consequences it
// fixes: a proposal that arrived after you opened the tab was invisible, and a
// row carried no timestamp, so a candidate who had waited a week looked exactly
// like one who arrived a minute ago.
//
// The refresh is a full reload, deliberately: the URL already carries ?status=
// and ?id=, so reloading preserves the stage AND the selected row without
// re-implementing the render client-side. It fires only when a gate count
// actually moved, so a reviewer reading a detail pane is not interrupted on a
// timer.

(function () {
  var POLL_MS = 20000;
  // A proposal sits in `approved` only until the onboard-runner claims it —
  // normally seconds. Ten minutes is well past any healthy mint.
  //
  // Measured from the ADMIN DECISION (data-decided), never from submission.
  // Keyed on the creation timestamp this fired on essentially every approval:
  // a proposal spends hours or days crossing its two human gates, so it was
  // already older than ten minutes at the instant it became approved, and the
  // badge lit before the runner had a chance to be late. A warning that is
  // always on is not a warning.
  var STALE_APPROVED_S = 600;

  function ago(seconds) {
    if (seconds < 60) return Math.max(seconds, 0) + 's';
    if (seconds < 3600) return Math.floor(seconds / 60) + 'm';
    if (seconds < 86400) return Math.floor(seconds / 3600) + 'h';
    return Math.floor(seconds / 86400) + 'd';
  }

  function renderAges() {
    var now = Math.floor(Date.now() / 1000);
    document.querySelectorAll('.ob-age').forEach(function (el) {
      var created = parseInt(el.dataset.created || '0', 10);
      if (!created) { el.textContent = ''; return; }
      var age = now - created;
      el.textContent = ago(age) + ' old';
      var stale = el.parentElement.querySelector('.ob-badge.stale');
      // A row with no decided_at cannot be judged late — say nothing rather
      // than guessing from another clock.
      var decided = parseInt(el.dataset.decided || '0', 10);
      var sinceDecision = decided ? now - decided : -1;
      // A queued cut is judged from the admin's click (data-queued),
      // the moment it was handed to the runner, the same way an approved
      // mint is judged from the decision.
      var queued = parseInt(el.dataset.queued || '0', 10);
      var sinceQueued = queued ? now - queued : -1;
      var lateMint = el.dataset.status === 'approved' && sinceDecision > STALE_APPROVED_S;
      var lateCut = el.dataset.status === 'offboard-requested' && sinceQueued > STALE_APPROVED_S;
      if (lateMint || lateCut) {
        if (!stale) {
          stale = document.createElement('span');
          stale.className = 'ob-badge stale';
          stale.textContent = 'runner?';
          stale.title = lateCut
            ? 'Cut queued ' + ago(sinceQueued) + ' ago and still not claimed. ' +
              'A healthy onboard-runner claims a queued cut within seconds; one built ' +
              'before 2026-09-26 never reads this queue. The notice at the top of the ' +
              'page says which it is and what fixes it.'
            : 'Approved ' + ago(sinceDecision) + ' ago and still not claimed. ' +
              'A healthy onboard-runner claims an approved proposal within seconds, so the ' +
              'runner is probably down or not scoped to this goatnet. Nothing is wrong with ' +
              'the proposal itself.';
          el.parentElement.appendChild(stale);
        }
      } else if (stale) {
        stale.remove();
      }
    });
  }

  function badge(stage, n) {
    var tab = document.querySelector('.ob-tabs a[data-stage="' + stage + '"]');
    if (!tab) return false;
    var c = tab.querySelector('.c');
    if (!c) return false;
    var was = parseInt(c.textContent || '0', 10) || 0;
    c.textContent = n;
    c.hidden = !n;
    return was !== n;
  }

  // A poll that saw a count move on the stage being looked at owes the page a
  // reload. The debt is kept here rather than re-derived on the next poll,
  // because badge() has already written the new count by then: the badge
  // equals the server, "moved" is false, and a reload held back for any
  // reason would never fire. That is how the first cut of the find (#1306)
  // lost arrivals: a candidate who turned up mid-search stayed out of the
  // hidden index until a navigation.
  var reloadDue = false;

  var formValues = new WeakMap();
  function values(form) {
    return JSON.stringify(Array.from(form.querySelectorAll('input:not([type="hidden"]), textarea, select'))
      .map(function (el) { return [el.name, el.value, el.checked]; }));
  }
  Array.from(document.forms).forEach(function (form) { formValues.set(form, values(form)); });
  document.addEventListener('focusin', function (e) {
    var form = e.target.form;
    if (form && !formValues.has(form)) formValues.set(form, values(form));
  });
  document.addEventListener('reset', function (e) {
    var form = e.target;
    setTimeout(function () { formValues.set(form, values(form)); }, 0);
  });
  function unsaved() {
    return Array.from(document.forms).some(function (form) {
      return formValues.has(form) && formValues.get(form) !== values(form);
    });
  }
  function pollNotice(message) {
    var el = document.getElementById('queue-read-error');
    if (!el) {
      el = document.createElement('p'); el.id = 'queue-read-error';
      el.setAttribute('role', 'status');
      var anchor = document.querySelector('.ob-tabs');
      if (anchor) anchor.insertAdjacentElement('afterend', el);
      else (document.querySelector('main') || document.body).prepend(el);
    }
    el.textContent = message; el.hidden = !message;
  }
  var pollRequest = 0;
  function poll() {
    var request = ++pollRequest;
    fetch('/api/onboard/proposals/counts', { credentials: 'same-origin' })
      .then(function (r) {
        if (r.status === 401 || r.redirected) throw new Error('Your session expired. Sign in again to refresh the queue.');
        if (!r.ok) throw new Error('Could not refresh the queue. Retrying shortly.');
        return r.json();
      })
      .then(function (counts) {
        if (request !== pollRequest) return;
        if (!counts || !['proposed', 'sponsor_approved'].every(function (key) { return Number.isSafeInteger(counts[key]) && counts[key] >= 0; })) throw new Error('Could not refresh the queue. Retrying shortly.');
        pollNotice('');
        var moved = false;
        moved = badge('proposed', counts.proposed || 0) || moved;
        moved = badge('sponsor_approved', counts.sponsor_approved || 0) || moved;
        // Only the stage being looked at is worth a reload — the badge alone
        // carries the news for the other one.
        var here = new URLSearchParams(window.location.search).get('status');
        if (moved && (here === 'proposed' || here === 'sponsor_approved' || here === null)) {
          reloadDue = true;
        }
        // A find in progress (#1306) is never reloaded out from under the
        // operator: the reload waits, and fires on the first poll after the
        // box is cleared.
        if (document.body.classList.contains('ob-finding') || unsaved()) return;
        if (reloadDue) window.location.reload();
      })
      .catch(function (err) { if (request !== pollRequest) return; pollNotice(err.message === 'Your session expired. Sign in again to refresh the queue.' ? err.message : 'Could not refresh the queue. Retrying shortly.'); });
  }

  renderAges();
  setInterval(renderAges, 30000);
  setInterval(poll, POLL_MS);
})();
