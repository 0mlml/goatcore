// Header-nav dropdown discipline for every portal page.
// Native <details> elements toggle independently, so users can pop
// multiple groups open at once and the menus overlap. We enforce
// single-open: opening one nav-group closes any other; clicking
// outside the nav closes everything; Escape closes the active group.
(function () {
  function init() {
    var groups = document.querySelectorAll("nav.nav-grouped details.nav-group");
    if (!groups.length) return;
    groups.forEach(function (d) {
      d.addEventListener("toggle", function () {
        if (!d.open) return;
        groups.forEach(function (o) { if (o !== d && o.open) o.open = false; });
      });
    });
    document.addEventListener("click", function (e) {
      var inside = false;
      groups.forEach(function (d) { if (d.contains(e.target)) inside = true; });
      if (inside) return;
      groups.forEach(function (d) { if (d.open) d.open = false; });
    });
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Escape") return;
      groups.forEach(function (d) {
        if (!d.open) return;
        var hadFocus = d.contains(document.activeElement);
        d.open = false;
        if (hadFocus) d.querySelector("summary").focus();
      });
    });
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
