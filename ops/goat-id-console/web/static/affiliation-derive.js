// affiliation-derive.js — live, read-only derivation for any nation input on
// the review page (ADR 1151).
//
// Alliance standing and ours-vs-foreign are DERIVED, never stored, so the form
// shows them as the operator types rather than revealing them after the record
// is committed. The answers come from /onboarding/affiliation-derive, which
// calls the same functions the create path uses — the panel cannot promise one
// value and record another.
//
// ONE implementation, two callers. The create-new-org panel and the
// correct-a-field form both edit a nation, and the first version of this lived
// inside the create-new panel only. That panel suppresses itself the moment the
// org resolves in the registry, so the moment the data got GOOD the operator
// lost the derived fields — which is exactly backwards.
function initAffiliationDerive(nationInput, box, out, sectorInput) {
  if (!nationInput || !box) return;
  var timer = null, last = null, requestNumber = 0;

  function paint(d) {
    box.hidden = false;
    if (out.nation) out.nation.textContent = d.nation_label || d.nation || "—";
    if (out.standing) out.standing.textContent = d.alliance_standing || "—";
    if (out.standingWhy) out.standingWhy.textContent = d.alliance_basis || "";
    if (out.organic) {
      out.organic.textContent = d.organic_known ? (d.organic_label || "—") : "unknown";
    }
    if (out.organicWhy) out.organicWhy.textContent = d.organic_basis || "";
  }

  function refresh() {
    var n = (nationInput.value || "").trim();
    var sector = sectorInput ? sectorInput.value : "";
    var key = n + "|" + sector;
    if (key === last) return;
    last = key;
    var request = ++requestNumber;
    box.hidden = true;
    if (!n) { box.hidden = true; return; }
    fetch("/onboarding/affiliation-derive?nation=" + encodeURIComponent(n) + "&sector=" + encodeURIComponent(sector),
          { credentials: "same-origin" })
      .then(function (r) {
        if (r.status === 401 || r.redirected) throw new Error('Your session expired. Sign in again.');
        if (!r.ok) throw new Error('Could not load affiliation details. Try again.');
        return r.json();
      })
      .then(function (d) {
        if (request !== requestNumber || nationInput.value.trim() !== n || (sectorInput && sectorInput.value !== sector)) return;
        if (!d || typeof d !== 'object' || Array.isArray(d) || typeof d.organic_known !== 'boolean' || typeof d.nation !== 'string') throw new Error('malformed response');
        paint(d);
      })
      .catch(function (err) {
        if (request !== requestNumber || nationInput.value.trim() !== n || (sectorInput && sectorInput.value !== sector)) return;
        last = null; // Permit retrying the same value after a failed read.
        paint({nation: n, alliance_standing: 'Unavailable', alliance_basis:
          err.message.indexOf('Your session expired.') === 0 ? err.message : 'Could not load affiliation details. Try again.',
          organic_known: false, organic_basis: ''});
      });
  }

  nationInput.addEventListener("input", function () {
    clearTimeout(timer);
    timer = setTimeout(refresh, 200);
  });
  nationInput.addEventListener("change", refresh);
  if (sectorInput) sectorInput.addEventListener("change", refresh);
  refresh();
}
