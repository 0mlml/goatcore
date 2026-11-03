// goat docs — language picker (docs/design/goat-cli-hosted-docs.md, LP2).
//
// An elegant header dropdown, on every page, that switches the docs
// language at any time. Each row is FLAG + AUTONYM (the language's own
// name). The flag is a *visual aid only*; the autonym is the real
// identifier — so we never lean on the flag-is-not-a-language anti-pattern.
//
// Behaviour:
//   - Keeps your place:  /fr/<page> ⇄ /de/<page>  (default English at root).
//   - Persists the choice in localStorage so navigation stays in-language.
//   - Respects the browser's Accept-Language on the very first visit.
//   - Only offers languages that were actually built (window.GOAT_DOCS_LANGS,
//     injected at build time), so it can never link to a 404. Until LP3
//     fills the translations in, that's just ["en"].
(function () {
  "use strict";

  // code -> [flag emoji, autonym]. These are the NATO-member languages +
  // Ukrainian (each maps cleanly to a member nation) + the opt-in Klingon
  // easter-egg locale. Judgement calls, easy to retune here:
  //   - English defaults to the UK flag (vs US); both are NATO.
  //   - Klingon has no nation -> the Vulcan salute as a friendly stand-in.
  var LANGS = [
    ["en", "🇬🇧", "English"],
    ["fr", "🇫🇷", "Français"],
    ["es", "🇪🇸", "Español"],
    ["it", "🇮🇹", "Italiano"],
    ["pt", "🇵🇹", "Português"],
    ["ro", "🇷🇴", "Română"],
    ["de", "🇩🇪", "Deutsch"],
    ["nl", "🇳🇱", "Nederlands"],
    ["da", "🇩🇰", "Dansk"],
    ["sv", "🇸🇪", "Svenska"],
    ["nb", "🇳🇴", "Norsk"],
    ["is", "🇮🇸", "Íslenska"],
    ["lb", "🇱🇺", "Lëtzebuergesch"],
    ["cs", "🇨🇿", "Čeština"],
    ["sk", "🇸🇰", "Slovenčina"],
    ["sl", "🇸🇮", "Slovenščina"],
    ["hr", "🇭🇷", "Hrvatski"],
    ["bg", "🇧🇬", "Български"],
    ["mk", "🇲🇰", "Македонски"],
    ["cnr", "🇲🇪", "Crnogorski"],
    ["uk", "🇺🇦", "Українська"],
    ["pl", "🇵🇱", "Polski"],
    ["lt", "🇱🇹", "Lietuvių"],
    ["lv", "🇱🇻", "Latviešu"],
    ["et", "🇪🇪", "Eesti"],
    ["fi", "🇫🇮", "Suomi"],
    ["hu", "🇭🇺", "Magyar"],
    ["el", "🇬🇷", "Ελληνικά"],
    ["sq", "🇦🇱", "Shqip"],
    ["tr", "🇹🇷", "Türkçe"],
    ["tlh", "🖖", "tlhIngan Hol"]
  ];

  var DEFAULT = "en";
  var STORE_KEY = "goat-docs-lang";
  var CODES = LANGS.map(function (l) { return l[0]; });
  var META = {};
  LANGS.forEach(function (l) { META[l[0]] = { flag: l[1], name: l[2] }; });

  // Languages actually built (injected by the build as window.GOAT_DOCS_LANGS).
  // Default to English-only so we never offer a 404 before LP3 lands.
  var built = (window.GOAT_DOCS_LANGS && window.GOAT_DOCS_LANGS.length)
    ? window.GOAT_DOCS_LANGS.filter(function (c) { return META[c]; })
    : [DEFAULT];

  // The site is served at the host root (docs.nova/...), so absolute paths are
  // robust regardless of how deep the current page is.
  function currentLang() {
    var seg = window.location.pathname.split("/").filter(Boolean)[0];
    return (seg && CODES.indexOf(seg) !== -1) ? seg : DEFAULT;
  }
  function pagePath() {
    var parts = window.location.pathname.split("/").filter(Boolean);
    if (parts.length && CODES.indexOf(parts[0]) !== -1) parts = parts.slice(1);
    return parts.join("/");
  }
  function urlFor(code) {
    var base = (code === DEFAULT) ? "/" : "/" + code + "/";
    return base + pagePath() + window.location.search + window.location.hash;
  }

  function switchTo(code) {
    try { localStorage.setItem(STORE_KEY, code); } catch (e) {}
    if (code !== currentLang()) window.location.assign(urlFor(code));
  }

  // First-visit Accept-Language: if the reader hasn't chosen, is on the
  // default, and their browser prefers a built non-default language, send
  // them there once. Never overrides an explicit choice.
  function maybeAutoRedirect() {
    try {
      if (localStorage.getItem(STORE_KEY)) return;
    } catch (e) { return; }
    if (currentLang() !== DEFAULT) return;
    var prefs = (navigator.languages || [navigator.language || ""]);
    for (var i = 0; i < prefs.length; i++) {
      var primary = String(prefs[i]).toLowerCase().split("-")[0];
      if (primary !== DEFAULT && built.indexOf(primary) !== -1) {
        switchTo(primary);
        return;
      }
    }
  }

  function build() {
    // mdBook renamed #menu-bar -> #mdbook-menu-bar in 0.5.x but kept the
    // .right-buttons class — select by class first so we survive id renames.
    var bar = document.querySelector(".right-buttons") ||
              document.querySelector("#mdbook-menu-bar .right-buttons") ||
              document.querySelector(".menu-bar") ||
              document.querySelector("#mdbook-menu-bar") ||
              document.querySelector("#menu-bar");
    if (!bar) return;

    var cur = currentLang();
    var wrap = document.createElement("div");
    wrap.className = "goat-lang-picker";

    var btn = document.createElement("button");
    btn.className = "icon-button goat-lang-button";
    btn.type = "button";
    btn.setAttribute("aria-haspopup", "true");
    btn.setAttribute("aria-expanded", "false");
    btn.title = "Language — " + (META[cur] ? META[cur].name : cur);
    btn.innerHTML = "<span class=\"goat-lang-flag\">" +
      (META[cur] ? META[cur].flag : "🌐") + "</span>" +
      "<span class=\"goat-lang-caret\">▾</span>";

    var menu = document.createElement("ul");
    menu.className = "goat-lang-menu";
    menu.setAttribute("role", "menu");
    menu.style.display = "none";

    built.forEach(function (code) {
      var m = META[code]; if (!m) return;
      var li = document.createElement("li");
      li.setAttribute("role", "none");
      var a = document.createElement("a");
      a.setAttribute("role", "menuitem");
      a.href = urlFor(code);
      a.className = "goat-lang-item" + (code === cur ? " active" : "");
      a.innerHTML = "<span class=\"goat-lang-flag\">" + m.flag + "</span>" +
                    "<span class=\"goat-lang-name\">" + m.name + "</span>";
      a.addEventListener("click", function (ev) { ev.preventDefault(); switchTo(code); });
      li.appendChild(a);
      menu.appendChild(li);
    });

    function close() { menu.style.display = "none"; btn.setAttribute("aria-expanded", "false"); }
    function open() { menu.style.display = "block"; btn.setAttribute("aria-expanded", "true"); }
    btn.addEventListener("click", function (ev) {
      ev.stopPropagation();
      (menu.style.display === "none") ? open() : close();
    });
    document.addEventListener("click", close);
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") close(); });

    wrap.appendChild(btn);
    wrap.appendChild(menu);
    // Put it first among the right buttons (before the theme toggle) so it
    // sits in a predictable, prominent spot.
    bar.insertBefore(wrap, bar.firstChild);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { maybeAutoRedirect(); build(); });
  } else {
    maybeAutoRedirect();
    build();
  }
})();
