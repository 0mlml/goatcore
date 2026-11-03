// goat docs — make the install/docs hosts match the goatnet you're on.
//
// The docs are built once and served on every site (docs.nova, docs.zagros, …),
// so the snippets can't hardcode a host — and nothing nova-specific
// belongs in the served prose. The source uses goatnet-neutral placeholders;
// at runtime we rewrite them to the real hosts derived from THIS page's host
// (docs.<site>.<zone>):
//   `get.<your-goatnet>`  -> get.<site>.<zone>   (the download surface)
//   `docs.<your-goatnet>` -> docs.<site>.<zone>  (this docs site itself)
// Works on every site and in every language (it operates on the rendered DOM,
// not the source prose), so no per-language churn.
(function () {
  "use strict";
  var host = window.location.host; // e.g. docs.zagros.netbird.zagros.example
  // Only rewrite when actually served from a docs.<site> host. Locally
  // (localhost/file) leave the placeholders so they're obviously templates.
  if (!/^docs\./.test(host)) return;
  var getHost = "get." + host.replace(/^docs\./, "");

  // Each pair is [neutral placeholder, real host on this goatnet].
  var SUBS = [
    ["get.<your-goatnet>", getHost],
    ["docs.<your-goatnet>", host],
  ];
  function rewrite(node) {
    if (node.nodeType === 3) { // text node
      var v = node.nodeValue;
      for (var s = 0; s < SUBS.length; s++) {
        if (v.indexOf(SUBS[s][0]) !== -1) {
          v = v.split(SUBS[s][0]).join(SUBS[s][1]);
        }
      }
      if (v !== node.nodeValue) node.nodeValue = v;
      return;
    }
    for (var i = 0; i < node.childNodes.length; i++) rewrite(node.childNodes[i]);
  }

  function run() {
    // Install commands live in code blocks + inline code.
    document.querySelectorAll("code, pre").forEach(function (el) { rewrite(el); });
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", run);
  } else {
    run();
  }
})();
