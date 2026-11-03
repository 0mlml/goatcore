// Parent-org live feedback on the onboarding review panel (ADR 1112).
// As the admin types a parent org id, the field reports whether the value
// resolves in the org registry (directly or via alias) and offers registered
// orgs that look like it. Advisory only: submit is never blocked here — the
// server still refuses an unknown parent fail-closed at create time.
function initOrgParentSuggest(input, state, menu) {
  if (!input || !state || !menu) return;
  state.setAttribute("role", "status");
  var timer = null;
  var active = -1; // keyboard-highlighted menu row

  function closeMenu() {
    menu.hidden = true;
    menu.innerHTML = "";
    active = -1;
  }

  function setState(cls, text) {
    state.className = "ob-parent-state" + (cls ? " " + cls : "");
    state.textContent = text || "";
  }

  function pick(id) {
    input.value = id;
    closeMenu();
    check();
    input.focus();
  }

  function renderMenu(candidates) {
    closeMenu();
    if (!candidates || !candidates.length) return;
    candidates.forEach(function (c) {
      var b = document.createElement("button");
      b.type = "button";
      var label = document.createElement("code");
      label.textContent = c.id;
      b.appendChild(label);
      var facts = [];
      if (c.display_name) facts.push(c.display_name);
      if (c.nation) facts.push(c.nation);
      if (c.parent) facts.push("under " + c.parent);
      if (c.via && c.via.indexOf("alias:") === 0) facts.push(c.via);
      if (facts.length) {
        var s = document.createElement("small");
        s.textContent = " — " + facts.join(" · ");
        b.appendChild(s);
      }
      // mousedown, not click: the input's blur fires before click and would
      // close the menu under the pointer.
      b.addEventListener("mousedown", function (e) {
        e.preventDefault();
        pick(c.id);
      });
      menu.appendChild(b);
    });
    menu.hidden = false;
  }

  var requestNumber = 0;
  function check() {
    var request = ++requestNumber;
    var q = input.value.trim();
    setState('', '');
    closeMenu();
    if (!q) {
      setState("", "");
      closeMenu();
      return;
    }
    fetch("/onboarding/org-suggest?q=" + encodeURIComponent(q), { credentials: "same-origin" })
      .then(function (r) {
        if (r.status === 401 || r.redirected) throw new Error('Your session expired. Sign in again to check the organization.');
        if (!r.ok) throw new Error('Could not check the organization. Try again.');
        return r.json();
      })
      .then(function (d) {
        if (request !== requestNumber || input.value.trim() !== q) return;
        if (!d || typeof d.resolves !== "boolean") throw new Error("malformed response");
        if (d.resolves) {
          var canonical = d.canonical || q;
          var msg = "✓ matches registered org " + canonical +
            (canonical !== q ? " (via alias)" : "");
          if (d.ancestors && d.ancestors.length) {
            msg += " — under " + d.ancestors.join(" → ");
          }
          setState("ok", msg);
          closeMenu(); // it resolves — nothing to pick
        } else {
          setState("miss", "✗ not a registered org — pick a suggestion, or clear the field");
          renderMenu(d.candidates);
        }
      })
      .catch(function (err) {
        if (request !== requestNumber || input.value.trim() !== q) return;
        closeMenu();
        setState("error", err.message.indexOf('Your session expired.') === 0 ? err.message : 'Could not check the organization. Try again.');
      });
  }

  input.addEventListener("input", function () {
    clearTimeout(timer);
    timer = setTimeout(check, 200);
  });

  input.addEventListener("keydown", function (e) {
    var rows = menu.querySelectorAll("button");
    if (menu.hidden || !rows.length) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      active += e.key === "ArrowDown" ? 1 : -1;
      active = (active + rows.length) % rows.length;
      rows.forEach(function (r, i) { r.classList.toggle("active", i === active); });
    } else if (e.key === "Enter" && active >= 0) {
      e.preventDefault(); // select, don't submit the create form
      pick(rows[active].querySelector("code").textContent);
    } else if (e.key === "Escape") {
      closeMenu();
    }
  });

  input.addEventListener("blur", function () {
    // Delay so a mousedown on a menu row lands first.
    setTimeout(closeMenu, 150);
  });
}
