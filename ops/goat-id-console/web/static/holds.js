// SPDX-License-Identifier: Apache-2.0
//
// Client-side behaviour of the Mesh holds page (/holds, goat#1042). No
// network calls: the page is a snapshot and a reload re-reads. Reads the
// markup contract holds.html renders and nothing else:
//   #holds-summary button.holds-band[data-band]   quick filters
//   #holds-filter                                  text filter
//   table.holds tbody.hold-row[data-band][data-kind][data-attention][data-filter][data-held][data-inactive]
//   #holds-miss                                    "no held peer matches"
//   table.holds th.sortable[data-sort] > button    Held since / Inactive sorts
// Each held peer is its own <tbody>, so its details line hides and moves
// with it. The Go template test pins the same ids from the other side.
(function () {
  "use strict";

  var band = "all";
  var sort = { key: "", dir: "desc" };

  // The quick filter (band) and the text filter compose: a row is shown
  // when both admit it. Kind and State are two facts: the dormancy /
  // security / unknown buttons read data-kind, Needs attention reads
  // data-attention, so a quarantined peer that also needs attention
  // answers to both buttons (data-band is the sort order, not the filter).
  function bandAdmits(r) {
    if (band === "all") return true;
    if (band === "attention") return r.getAttribute("data-attention") === "1";
    return r.getAttribute("data-kind") === band;
  }
  function filterHolds() {
    var input = document.getElementById("holds-filter");
    var needle = input ? input.value.trim().toLowerCase() : "";
    var rows = document.querySelectorAll("table.holds tbody.hold-row");
    captureHeaderWidths();
    var shown = 0;
    for (var i = 0; i < rows.length; i++) {
      var r = rows[i];
      var bandHit = bandAdmits(r);
      var textHit = needle === "" || (r.getAttribute("data-filter") || "").indexOf(needle) !== -1;
      var hit = bandHit && textHit;
      r.hidden = !hit;
      if (hit) shown++;
    }
    var isMiss = rows.length > 0 && shown === 0;
    var miss = document.getElementById("holds-miss");
    if (miss) miss.hidden = !isMiss;
    pinHeaderWidths(band !== "all" || needle !== "");
  }

  // The table is auto-layout, so the visible rows size the columns: hide
  // some and the rest respread, hide them all and the header alone sizes
  // them under the miss line. Pin each header cell to the width it had on
  // the full table while any filter narrows it; release when the filters
  // clear, so a resize with nothing filtered lays the table out afresh.
  var headerWidths = null;
  // Read the widths before any row is hidden; once pinned, keep the first
  // measurement until the filters clear.
  function captureHeaderWidths() {
    if (headerWidths !== null) return;
    var ths = document.querySelectorAll("table.holds thead th");
    if (!ths.length) return;
    headerWidths = [];
    for (var k = 0; k < ths.length; k++) headerWidths.push(ths[k].getBoundingClientRect().width);
  }
  function pinHeaderWidths(pin) {
    var ths = document.querySelectorAll("table.holds thead th");
    for (var m = 0; m < ths.length; m++) {
      ths[m].style.width = pin && headerWidths ? headerWidths[m] + "px" : "";
    }
    if (!pin) headerWidths = null;
  }

  function selectBand(button, buttons) {
    band = button.getAttribute("data-band");
    for (var j = 0; j < buttons.length; j++) {
      var on = buttons[j] === button;
      buttons[j].classList.toggle("active", on);
      buttons[j].setAttribute("aria-pressed", on ? "true" : "false");
    }
    filterHolds();
  }

  // A row with no value for the sorted column (no record ⇒ no held-since;
  // mesh down or never seen ⇒ no inactive days) sorts last either way.
  function sortKey(row, attr, key) {
    var v = row.getAttribute(attr);
    if (v === "" || v === null) return NaN;
    if (key === "held" && v === "0") return NaN;
    return parseFloat(v);
  }

  // Click-to-sort on Held since and Inactive only; toggles direction; the
  // sorted header is marked in the class and in aria-sort.
  function sortHolds(key) {
    var table = document.getElementById("holds-table");
    if (!table) return;
    sort.dir = sort.key === key && sort.dir === "desc" ? "asc" : "desc";
    sort.key = key;
    var attr = key === "held" ? "data-held" : "data-inactive";
    var rows = Array.prototype.slice.call(table.querySelectorAll("tbody.hold-row"));
    rows.sort(function (a, b) {
      var an = sortKey(a, attr, key);
      var bn = sortKey(b, attr, key);
      if (isNaN(an) && isNaN(bn)) return 0;
      if (isNaN(an)) return 1;
      if (isNaN(bn)) return -1;
      return sort.dir === "asc" ? an - bn : bn - an;
    });
    for (var i = 0; i < rows.length; i++) table.appendChild(rows[i]);
    var ths = table.querySelectorAll("th.sortable");
    for (var j = 0; j < ths.length; j++) {
      var mine = ths[j].getAttribute("data-sort") === key;
      ths[j].classList.toggle("sorted", mine);
      ths[j].classList.toggle("asc", mine && sort.dir === "asc");
      ths[j].setAttribute("aria-sort", mine ? (sort.dir === "asc" ? "ascending" : "descending") : "none");
    }
  }

  function wire() {
    var input = document.getElementById("holds-filter");
    if (input) input.addEventListener("input", filterHolds);

    // Queue deep-links carry only display filter text. The server still owns
    // every row and all classification; this just opens the relevant context.
    if (input) {
      var initial = new URLSearchParams(window.location.search).get("filter");
      if (initial) {
        input.value = initial;
        filterHolds();
      }
    }
    var group = document.getElementById("holds-summary");
    if (group) {
      var buttons = group.querySelectorAll("button.holds-band");
      for (var i = 0; i < buttons.length; i++) {
        buttons[i].addEventListener("click", function () { selectBand(this, buttons); });
      }
    }
    var sorters = document.querySelectorAll("table.holds th.sortable[data-sort] > button");
    for (var k = 0; k < sorters.length; k++) {
      sorters[k].addEventListener("click", function () { sortHolds(this.parentNode.getAttribute("data-sort")); });
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", wire);
  } else {
    wire();
  }
})();
