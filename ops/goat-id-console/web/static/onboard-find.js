// Cross-stage candidate find on /onboarding (#1306).
//
// The page renders every candidate the session may see, in every stage, as
// hidden rows under #ob-find (scoped on the server before a row exists). This
// script filters those rows by case-insensitive substring as the operator
// types, the way the users list filters its table: no network call, no
// reload. With a term of two or more characters the stage list steps aside
// and the index shows the matches; clear the box and the stage list is back.
//
// The term mirrors into the URL fragment (#q=) so a reload or a shared link
// lands on the same view, and /users can hand a missed term across. A
// fragment and not a query string, on purpose: the term is a person's email
// or name, and a fragment never leaves the browser, so it reaches neither the
// console's access log nor the proxy's. The server never sees the term; this
// script is the only reader and the only writer.
(function () {
  var MIN_TERM = 2, MAX_VISIBLE = 50;
  var input = document.getElementById('candidate-filter');
  var find = document.getElementById('ob-find');
  var stage = document.getElementById('ob-stage');
  if (!input || !find || !stage) return;
  var rows = find.querySelectorAll('a.row');
  var none = find.querySelector('.ob-find-none');
  var more = find.querySelector('.ob-find-more');

  function readTerm() {
    var h = window.location.hash;
    if (h.indexOf('#q=') !== 0) return '';
    try { return decodeURIComponent(h.slice(3)); } catch (e) { return ''; }
  }

  function writeTerm(term) {
    try {
      var url = new URL(window.location.href);
      url.hash = term ? 'q=' + encodeURIComponent(term) : '';
      history.replaceState(null, '', url);
    } catch (e) { /* an old browser keeps working without the address-bar mirror */ }
  }

  function apply(q) {
    var term = q.trim();
    var needle = term.toLowerCase();
    var on = needle.length >= MIN_TERM;
    stage.hidden = on;
    find.hidden = !on;
    document.body.classList.toggle('ob-finding', on);
    writeTerm(on ? term : '');
    if (!on) return;
    var matched = 0;
    for (var i = 0; i < rows.length; i++) {
      var hay = (rows[i].getAttribute('data-filter') || '').toLowerCase();
      var hit = hay.indexOf(needle) !== -1;
      if (hit) matched++;
      // .ob-list .row is display:block, so the hidden attribute alone would
      // not hide a row; toggle the inline style like the users filter does.
      rows[i].style.display = (hit && matched <= MAX_VISIBLE) ? '' : 'none';
    }
    if (none) none.hidden = matched !== 0;
    if (more) more.hidden = matched <= MAX_VISIBLE;
  }

  window.filterCandidates = apply;
  var q0 = readTerm();
  if (q0) { input.value = q0; apply(q0); }
})();
