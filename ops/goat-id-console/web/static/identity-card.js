// identity-card.js — shared "identity + authorities" renderer for the onboarding
// review surfaces (sponsor-review + admin queue). Turns the raw proposal manifest
// into a cold-readable card: WHO the person is, WHAT they'd be granted (mesh /
// data-fabric / services / machines), and the FLAGS a reviewer must not miss
// (escalated admin role, change-requests, incomplete draft). The raw manifest
// stays available, collapsed, for the operator who wants the bytes.
//
// Usage: renderIdentityCard(containerEl, proposal)   // proposal = the /api/onboard/proposals row
"use strict";
(function () {
  // Escapes ' as well as ": this file builds most attributes with single quotes.
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;" }[c]; }); }
  // #2209: escape, then offer a line break after @ . / - _ so a long email or
  // organisation wraps at a natural point instead of mid-word. The escaped
  // entities (&amp; &lt; &gt; &quot; &#39;) contain none of those characters.
  function escBreak(s) { return esc(s).replace(/([@./_-])/g, "$1<wbr>"); }
  function fmtTime(s) { if (!s) return "—"; try { return new Date(s * 1000).toLocaleString(); } catch (e) { return String(s); } }
  function arr(x) { return Array.isArray(x) ? x : []; }

  // provenanceRows renders the two-gate provenance: who recorded the sponsor
  // vouch (can be an admin acting at the sponsor gate — not necessarily the
  // sponsor) and who finally approved/rejected. decided_by is stamped at each
  // decision, so on a completed row it is the FINAL admin approver.
  function provenanceRows(p, dash) {
    var out = "";
    var when = function (t) { return t ? " <span style='color:var(--ic-mut);font-size:11px'>· " + esc(fmtTime(t)) + "</span>" : ""; };
    var past = { sponsor_approved: 1, approved: 1, onboarding: 1, onboarded: 1, failed: 1 };
    if (past[p.status] || p.sponsor_approved_by || p.retroactive) {
      out += "<b>sponsorship approved by</b><span>" + esc(p.sponsor_approved_by || dash) + when(p.sponsor_approved_at) + "</span>";
    }
    var finals = { approved: "onboarding approved by", onboarding: "onboarding approved by", onboarded: "onboarding approved by", failed: "onboarding approved by", rejected: "rejected by" };
    if (finals[p.status] || p.retroactive) {
      out += "<b>" + (finals[p.status] || "onboarding approved by") + "</b><span>" + esc(p.decided_by || dash) + when(p.decided_at) + "</span>";
    }
    // Every other console-mediated act on this candidate (org attach/create,
    // welcome resend, provisioning retry, …) is audited with its actor —
    // deep-link the trail pre-filtered. Admin realm only: /audit is
    // admin-gated, so don't show sponsors a link that 403s.
    if (p.id && window.GOAT_CONSOLE_ADMIN) {
      out += "<b>console actions</b><span><a href='/audit?target=" + encodeURIComponent(p.id) + "'>full audit trail for this candidate →</a></span>";
    }
    return out;
  }

  // Inject the card stylesheet once (self-contained so both pages match).
  function ensureStyle() {
    if (document.getElementById("idcard-style")) return;
    var css = `
    /* Palette follows the host page's theme variables (style.css :root and
       :root[data-theme="light"]) so the card is complete in BOTH themes; the
       literals are the original dark values, kept only as fallbacks for a
       host without those variables. Text colours are set explicitly rather
       than inherited: in light mode the page's body text is dark, and a card
       that inherited it while keeping a dark panel background rendered its
       values invisibly (goat#1037 review). */
    .idcard{--ic-ok:var(--green-mid,#46b46a);--ic-warn:var(--amber,#d9a441);--ic-err:var(--red,#e5534b);--ic-acc:var(--blue,#5b9dff);--ic-mut:var(--fog-2,#9aa4b2);--ic-bd:var(--line,#2a2f3a);--ic-card:var(--ink-3,#171a21);--ic-well:var(--ink-1,#0c0e12);--ic-txt:var(--fog-1,#b9c2bd);--ic-hi:var(--fog-0,#e8ece9);color:var(--ic-txt)}
    .idcard .hero{display:flex;align-items:flex-start;gap:14px;flex-wrap:wrap;margin-bottom:14px}
    .idcard .hero .who{min-width:0;overflow-wrap:anywhere}
    .idcard .hero .who h2{margin:0;font-size:22px;line-height:1.15;color:var(--ic-hi)}
    .idcard .hero .who .sub{color:var(--ic-mut);font-size:13px;margin-top:3px}
    .idcard .hero .spacer{flex:1}
    .idcard .tierchip{font-size:13px;font-weight:700;padding:6px 13px;border-radius:22px;border:1.5px solid var(--ic-acc);color:var(--ic-acc);white-space:nowrap}
    .idcard .tierchip.esc{color:#06101f;background:var(--ic-warn);border-color:var(--ic-warn)}
    .idcard .flags{display:flex;gap:7px;flex-wrap:wrap;margin:0 0 14px}
    .idcard .flag{font-size:11.5px;font-weight:700;padding:3px 10px;border-radius:20px;border:1px solid var(--ic-bd)}
    .idcard .flag.esc{color:#06101f;background:var(--ic-warn);border-color:var(--ic-warn)}
    .idcard .flag.pet{color:var(--ic-warn);border-color:var(--ic-warn)}
    .idcard .flag.draft{color:var(--ic-mut);border-style:dashed}
    /* #2109: 1fr tracks have an implicit min-width of auto (their content's
       intrinsic width), not 0, so a long value in one panel pushed .idcard
       .grid (and the .ob-wrap it sits in) wider than the viewport instead
       of wrapping. minmax(0,1fr) lets each column track shrink; .panel
       needs its own min-width: 0 for the same reason one level down; the
       kv value wraps mid-word rather than overflowing its fixed-width
       label column. */
    /* Each panel needs at least 320px to keep the label/value and authority
       rows readable. auto-fit makes that decision from the card's actual
       width (the onboarding detail pane is narrower than the viewport), and
       min(100%,320px) still lets a single panel shrink on small screens. */
    .idcard .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,320px),1fr));gap:14px}
    .idcard .panel{background:var(--ic-card);border:1px solid var(--ic-bd);border-radius:10px;padding:13px 15px;min-width:0}
    .idcard .panel h3{margin:0 0 10px;font-size:12px;color:var(--ic-mut);text-transform:uppercase;letter-spacing:.05em}
    /* #2209: the same 160px label column as .ob-kv on this page. .ob-kv
       collapses at a 640px window because it sits in the wide detail pane;
       a card panel is narrow at any window size, so it collapses on its own
       width instead (label above value) and the value gets the whole panel.
       overflow-wrap:anywhere stays as the last resort; escBreak() gives the
       value natural break points first. */
    .idcard .panel{container-type:inline-size}
    .idcard .kv{display:grid;grid-template-columns:160px minmax(0,1fr);gap:6px 10px;font-size:13.5px}
    .idcard .kv b{color:var(--ic-mut);font-weight:500}
    .idcard .kv span{color:var(--ic-hi);overflow-wrap:anywhere}
    @container (max-width: 440px){
      .idcard .kv{grid-template-columns:minmax(0,1fr);gap:1px 0}
      .idcard .kv span{margin-bottom:6px}
    }
    .idcard .auth{display:flex;gap:11px;align-items:flex-start;padding:8px 0;border-top:1px solid var(--ic-bd)}
    .idcard .auth:first-of-type{border-top:0}
    .idcard .auth .ico{font-size:16px;width:22px;text-align:center;flex:none;line-height:1.4}
    .idcard .auth .body{flex:1;min-width:0;overflow-wrap:anywhere}
    .idcard .auth .lab{font-size:11px;color:var(--ic-mut);text-transform:uppercase;letter-spacing:.04em}
    .idcard .auth .val{font-size:14px;font-weight:600;color:var(--ic-hi)}
    .idcard .auth .gloss{font-size:12.5px;color:var(--ic-mut)}
    .idcard .auth.warn .val{color:var(--ic-warn)}
    .idcard .auth.warn .ico{color:var(--ic-warn)}
    .idcard .auth.none .val{color:var(--ic-mut);font-weight:500}
    .idcard details.raw{margin-top:14px}
    .idcard details.raw summary{cursor:pointer;color:var(--ic-mut);font-size:12.5px}
    .idcard details.raw pre{background:var(--ic-well);color:var(--ic-txt);border:1px solid var(--ic-bd);border-radius:8px;padding:12px;overflow:auto;font:12px/1.5 ui-monospace,Menlo,monospace;max-height:340px;margin-top:8px}
    `;
    var st = document.createElement("style"); st.id = "idcard-style"; st.textContent = css;
    document.head.appendChild(st);
  }

  // The tier a proposal RECORDS, preferred over anything re-derived from its
  // manifest. The proposal carries `tier` as a first-class field (it is what the
  // invite pre-bound and what both human gates reviewed); inferring instead gave
  // the card a second, independently-drifting opinion.
  //
  // inferTier below matches only the LEGACY Ory group `ADMINS`, so after the
  // rename to `PLATFORM-ADMIN` (ADR 1043 §8; the Go side tracks both via
  // platformAdminGroup/legacyAdminGroup in handlers.go) inference would show a
  // platform-admin to reviewers as "Developer" beside an "ESCALATED ADMIN" flag,
  // and a reviewer vouching at the sponsor or admin gate would read the wrong tier
  // for the one class of onboarding the flow exists to scrutinise.
  var TIER_LABEL = {
    "platform-admin": { label: "Platform Admin", esc: true },
    "iam-admin": { label: "IAM Admin", esc: true },
    "developer": { label: "Developer", esc: false },
    "browse": { label: "Browse", esc: false }
  };
  function tierOf(p, m) {
    var t = TIER_LABEL[String(p && p.tier || "").trim().toLowerCase()];
    return t ? t : inferTier(m);
  }

  // Fallback only, for a row with no recorded tier (a draft, or a pre-tier
  // record). Accepts the canonical PLATFORM-ADMIN and the legacy ADMINS.
  function inferTier(m) {
    var og = arr(m.ory_site && m.ory_site.groups);
    if (og.indexOf("PLATFORM-ADMIN") >= 0 || og.indexOf("ADMINS") >= 0) return { label: "Platform Admin", esc: true };
    if (og.indexOf("IAM-ADMIN") >= 0) return { label: "IAM Admin", esc: true };
    var ag = arr(m.netbird && m.netbird.auto_groups);
    var zen = !!(m.zenoh && m.zenoh.enabled);
    if (zen || ag.indexOf("DEVELOPER") >= 0) return { label: "Developer", esc: false };
    return { label: "Browse", esc: false };
  }

  var MESH_GLOSS = { DEVELOPER: "publish + subscribe on the mesh", OBSERVER: "read-only mesh access", ADMIN: "mesh administration", MONITOR: "observability", OPERATOR: "tactical actor", OFFSITE: "off-site" };
  // ADMINS passes every admin fence (identity console, NetBird dashboard,
  // portal admin) — it subsumes IAM admin, so the reviewer sees that spelled
  // out instead of a bare group name (onboard-tier-role-model.md §4/§6).
  var SVC_ADMIN = { ADMINS: "identity console + mesh dashboard + portal admin — subsumes IAM admin", "OBS-ADMIN": "observability admin (Grafana; MinIO console where wired)", "IAM-ADMIN": "identity / access admin console", "GRAFANA-ADMIN": "Grafana admin" };

  // Row icons as inline SVG rather than emoji (the voice rules allow none,
  // docs/design/design-system.md). Paths from Lucide (ISC licence), stroke 1.5
  // per the design system's icon note; decorative, since each row's label says
  // the same thing.
  function svgIcon(body) {
    return "<svg class='icon' width='1em' height='1em' viewBox='0 0 24 24' fill='none' stroke='currentColor' " +
      "stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round' aria-hidden='true' focusable='false'>" + body + "</svg>";
  }
  var ICON = {
    key: svgIcon("<path d='M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z'/><circle cx='16.5' cy='7.5' r='.5' fill='currentColor'/>"),
    globe: svgIcon("<circle cx='12' cy='12' r='10'/><path d='M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20'/><path d='M2 12h20'/>"),
    radio: svgIcon("<path d='M4.9 19.1C1 15.2 1 8.8 4.9 4.9'/><path d='M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5'/><circle cx='12' cy='12' r='2'/><path d='M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5'/><path d='M19.1 4.9C23 8.8 23 15.1 19.1 19'/>"),
    shield: svgIcon("<path d='M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'/>"),
    monitor: svgIcon("<rect width='20' height='14' x='2' y='3' rx='2'/><line x1='8' x2='16' y1='21' y2='21'/><line x1='12' x2='12' y1='17' y2='21'/>"),
    laptop: svgIcon("<path d='M20 16V7a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v9m16 0H4m16 0 1.28 2.55a1 1 0 0 1-.9 1.45H3.62a1 1 0 0 1-.9-1.45L4 16'/>"),
    lock: svgIcon("<rect width='18' height='11' x='3' y='11' rx='2' ry='2'/><path d='M7 11V7a5 5 0 0 1 10 0v4'/>"),
    branch: svgIcon("<line x1='6' x2='6' y1='3' y2='15'/><circle cx='18' cy='6' r='3'/><circle cx='6' cy='18' r='3'/><path d='M18 9a9 9 0 0 1-9 9'/>")
  };

  function authRow(ico, lab, val, gloss, cls) {
    return "<div class='auth " + (cls || "") + "'><div class='ico'>" + ico + "</div><div class='body'>" +
      "<div class='lab'>" + esc(lab) + "</div><div class='val'>" + esc(val) + "</div>" +
      (gloss ? "<div class='gloss'>" + esc(gloss) + "</div>" : "") + "</div></div>";
  }

  function renderIdentityCard(container, p) {
    ensureStyle();
    var m = {}; try { m = JSON.parse(p.manifest_json || "{}"); } catch (e) { }
    var idn = m.identity || {}, aff = m.affiliation || {}, nb = m.netbird || {}, ory = m.ory_site || {};
    var org = (m.vendor && m.vendor.display_name) || (m.org && m.org.display_name) || "";
    var tier = tierOf(p, m);

    // Flags.
    var pets = []; try { pets = JSON.parse(p.petitions || "[]"); } catch (e) { }
    var flags = "";
    if (p.escalated || tier.esc) flags += "<span class='flag esc'>⚠ ESCALATED ADMIN</span>";
    if (arr(pets).length) flags += "<span class='flag pet'>change request</span>";
    if (p.complete === false) flags += "<span class='flag draft'>incomplete draft</span>";

    // WHO panel. "sponsored by" prefers the human sponsor label; p.sponsor is
    // the machine value (a participant sponsor's UUID org anchor), kept subdued
    // beside the name for provenance.
    var spn = (p.sponsor_name && p.sponsor_name !== p.sponsor) ? p.sponsor_name : "";
    // Retroactive rows predate the three-stage system: unrecorded provenance
    // renders as an explanation, never a bare dash that reads like a bug.
    var retroDash = p.retroactive ? "pre-buildout (not recorded)" : "—";
    var spNote = "";
    if (p.sponsor === "operator") spNote = "shared operator key (no per-person name)";
    else if (spn) spNote = "anchor " + String(p.sponsor).slice(0, 8) + "…";
    else if (/^[0-9a-f]{32}$/.test(p.sponsor || "")) spNote = "identity anchor — no display name recorded at mint";
    var uc = aff.unit_code;
    var ucText = uc ? (esc(uc.value || "") + " · " + esc(uc.scheme || "") + (uc.source ? " (" + esc(uc.source) + ")" : "")) : "—";
    var who = "<div class='panel'><h3>Who</h3><div class='kv'>" +
      "<b>name</b><span>" + esc(idn.name || "—") + "</span>" +
      "<b>email</b><span>" + escBreak(idn.email || p.email || "—") + "</span>" +
      "<b>identity</b><span>" + esc(idn.type || "person") + "</span>" +
      "<b>organization</b><span>" + escBreak(org || "—") + "</span>" +
      "<b>affiliation</b><span>" + esc(aff.kind || "—") +
        (aff.sector ? " · " + esc(aff.sector) : "") +
        (aff.gov_branch ? " / " + esc(aff.gov_branch) : "") + "</span>" +
      // ADR 1171: the two nations are SEPARATE rows, and the person's own comes
      // first because this is a card about a person. They used to be one row
      // reading "organic · CAN", where CAN was the candidate's own citizenship
      // sitting in the organisation's field and nothing said so.
      "<b>nationality</b><span>" + (idn.nationality
        ? esc(idn.nationality) + " <span style='color:var(--ic-mut);font-size:11px'>· the person's own</span>"
        : "<span style='color:var(--ic-mut)'>not declared</span>") + "</span>" +
      "<b>org nation</b><span>" + (aff.nation
        ? esc(aff.nation) + " <span style='color:var(--ic-mut);font-size:11px'>· the organisation's</span>"
        : "<span style='color:var(--ic-mut)'>not declared</span>") + "</span>" +
      // A mismatch is NORMAL — a liaison officer, or anyone posted to another
      // country's command — so it is labelled, never flagged as an error.
      ((idn.nationality && aff.nation && idn.nationality !== aff.nation)
        ? "<b>relationship</b><span>" + esc(aff.relationship || "not declared") +
          " <span style='color:var(--ic-mut);font-size:11px'>· their nationality differs from the org's, which is normal for a liaison or an internationally recruited member</span></span>"
        : (aff.relationship ? "<b>relationship</b><span>" + esc(aff.relationship) + "</span>" : "")) +
      (aff.alliance_standing || aff.coalition
        ? "<b>standing</b><span>" + esc(aff.alliance_standing || aff.coalition) +
          " <span style='color:var(--ic-mut);font-size:11px'>· derived from the ORG's nation</span></span>"
        : "") +
      "<b>unit code</b><span>" + ucText + "</span>" +
      "<b>goatnet</b><span>" + esc(p.env || "—") + "</span>" +
      "<b>sponsored by</b><span title='" + esc(p.sponsor || "") + "'>" + esc(spn || p.sponsor || retroDash) +
        (spNote ? " <span style='color:var(--ic-mut);font-size:11px'>· " + esc(spNote) + "</span>" : "") + "</span>" +
      "<b>submitted</b><span>" + esc(fmtTime(p.created_at)) + "</span>" +
      provenanceRows(p, retroDash) +
      (p.retroactive ? "<b>record</b><span style='color:var(--ic-mut)'>retroactive import — this person was " +
        "onboarded before the three-stage system existed; unrecorded fields predate the buildout</span>" : "") +
      "</div></div>";

    // AUTHORITIES panel — the highlighted cold-read.
    var a = "";
    var tierGloss = "";
    if (tier.label === "Platform Admin") {
      tierGloss = "Backend administrative access — includes IAM Admin (full identity / user administration). Scrutinize before vouching.";
    } else if (tier.label === "IAM Admin") {
      tierGloss = "Identity / access administration — scrutinize before vouching.";
    } else if (tier.esc) {
      tierGloss = "Backend administrative access — scrutinize before vouching.";
    }
    a += authRow(ICON.key, "Access tier", tier.label, tierGloss, tier.esc ? "warn" : "");
    // Mesh.
    var ag = arr(nb.auto_groups);
    if (nb.enabled !== false && ag.length) {
      var gl = ag.map(function (g) { return MESH_GLOSS[g] || g.toLowerCase(); }).join("; ");
      a += authRow(ICON.globe, "Mesh network", ag.join(", "), gl);
    } else {
      a += authRow(ICON.globe, "Mesh network", "none", "no mesh enrollment", "none");
    }
    // Data fabric.
    if (m.zenoh && m.zenoh.enabled) a += authRow(ICON.radio, "Data fabric", "Publisher", "can publish to the Zenoh fabric");
    else a += authRow(ICON.radio, "Data fabric", "none", "no data-fabric publish access", "none");
    // Web services / admin consoles.
    var og = arr(ory.groups);
    if (og.length) {
      var adminHit = og.filter(function (g) { return SVC_ADMIN[g]; });
      var gl2 = og.map(function (g) { return SVC_ADMIN[g] || g.toLowerCase(); }).join("; ");
      a += authRow(adminHit.length ? ICON.shield : ICON.monitor, "Web services", og.join(", "), gl2, adminHit.length ? "warn" : "");
    } else if (ory.enabled) {
      a += authRow(ICON.monitor, "Web services", "participant", "portal + panoscope; no admin console");
    }
    // Machines.
    if (nb.usage_limit) a += authRow(ICON.laptop, "Machines", "up to " + esc(nb.usage_limit), "devices this person may enrol");
    // Login.
    if (ory.enabled) {
      var cred = (ory.credentials && ory.credentials.passkey) ? "passkey" : "password";
      a += authRow(ICON.lock, "Login", "Portal OIDC", cred + " credential" + (nb.ttl_days ? " · " + esc(nb.ttl_days) + "-day validity" : ""));
    }
    // Repo access — an interactive panel populated after render from
    // GET /api/onboard/proposals/{id}/repo-access: the effective set (each repo
    // tagged policy/audible), an "add repo" input, and a remove per row that POST
    // the updated overrides (docs/design/onboarding-github-repo-grants.md §2/§6).
    // A placeholder shows the captured username until the fetch resolves.
    var gh = String(idn.github || "").trim();
    a += "<div class='auth' id='repoaccess-panel'><div class='ico'>" + ICON.branch + "</div><div class='body'>" +
      "<div class='lab'>Repo access</div>" +
      "<div class='val' id='repoaccess-summary'>" + (gh ? "@" + esc(gh) :
        "<span style='color:var(--ic-warn)'>no GitHub username: added to no repositories</span>") + "</div>" +
      "<div class='gloss' id='repoaccess-body'>loading effective repos…</div>" +
      "</div></div>";
    var auth = "<div class='panel'><h3>Access &amp; authorities</h3>" + a + "</div>";

    var html = "<div class='idcard'>" +
      "<div class='hero'><div class='who'><h2>" + esc(idn.name || p.handle || "candidate") + "</h2>" +
      "<div class='sub'>" + esc(p.handle || "") + (org ? " · " + esc(org) : "") + "</div></div>" +
      "<div class='spacer'></div><div class='tierchip " + (tier.esc ? "esc" : "") + "'>" + esc(tier.label) + "</div></div>" +
      (flags ? "<div class='flags'>" + flags + "</div>" : "") +
      "<div class='grid'>" + who + auth + "</div>" +
      "<details class='raw'><summary>Show raw manifest</summary><pre>" + esc(JSON.stringify(m, null, 2)) + "</pre></details>" +
      "</div>";
    container.innerHTML = html;
    // Populate + wire the repo-access panel from the live effective set.
    wireRepoAccess(container, p);
  }

  // --- repo-access audible panel (fetch + add/remove) ----------------------

  var PERMS = ["pull", "triage", "push", "maintain", "admin"];

  // wireRepoAccess fetches the effective repo-access set for the proposal and
  // renders it into #repoaccess-body: each repo tagged policy/audible with a
  // remove control, plus an "add repo" input. Fail-soft — a fetch error leaves a
  // short note, never breaks the card.
  function wireRepoAccess(container, p) {
    var body = container.querySelector("#repoaccess-body");
    var summary = container.querySelector("#repoaccess-summary");
    if (!body || !p || !p.id) return;
    // Already onboarded: a repo can still be added (the grant fires at once), but
    // removing one here would not take it away on GitHub, so no Remove control.
    var locked = p.status === "onboarded" || p.status === "enrolled";
    // The browser-session read. The /api/onboard/... route takes only a bearer
    // key, so from this page it always failed and the panel never named a repo.
    fetch("/onboarding/" + encodeURIComponent(p.id) + "/repo-access", {
      headers: { "Accept": "application/json" }, credentials: "same-origin"
    }).then(function (r) {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.json();
    }).then(function (v) {
      renderRepoBody(container, p, v, locked);
      if (summary && v.github) summary.textContent = "@" + v.github;
    }).catch(function () {
      body.textContent = "could not load the repo-access set for this person";
    });
  }

  // missingRepoWarning names what a person with no GitHub username is left out
  // of, and how that is fixed. Empty when nothing is missing.
  function missingRepoWarning(v) {
    var miss = arr(v.missing_without_github);
    if (v.github || !miss.length) return "";
    var names = miss.map(function (g) { return esc(g.owner) + "/" + esc(g.repo); }).join(", ");
    return "<div style='color:var(--ic-warn)'>No GitHub username, so not added to " + names +
      ". Add one in the GitHub box on this card, or the person adds it at /github; either re-runs the grant.</div>";
  }

  function renderRepoBody(container, p, v, locked) {
    var body = container.querySelector("#repoaccess-body");
    if (!body) return;
    var eff = arr(v.effective);
    var parts = [];
    if (!v.has_token) {
      parts.push("<div style='color:var(--ic-warn)'>no onboarding token — grants will be skipped</div>");
    }
    var warn = missingRepoWarning(v);
    if (warn) {
      parts.push(warn);
    } else if (!eff.length) {
      parts.push("<div>no repos (policy grants nothing; add one below)</div>");
    } else {
      var rows = eff.map(function (g) {
        var slug = esc(g.owner) + "/" + esc(g.repo);
        var tag = g.source === "audible"
          ? "<span style='color:var(--ic-acc)'>audible</span>"
          : "<span style='color:var(--ic-mut)'>policy</span>";
        var rm = locked ? "" :
          " <button type='button' data-rmrepo='" + slug + "' aria-label='Remove " + slug + "' style='color:var(--ic-err);padding:2px 6px'>Remove</button>";
        return "<div>" + slug + " <span style='color:var(--ic-mut)'>(" + esc(g.permission) + ")</span> " + tag + rm + "</div>";
      });
      parts.push(rows.join(""));
    }
    arr(v.grants).forEach(function (g) {
      if (g.Outcome === "failed") {
        parts.push("<div style='color:var(--ic-err)'>" + esc(g.Owner) + "/" + esc(g.Repo) + " was not granted" +
          (g.Failure === "repo_not_visible_to_token" ? ": the console's GitHub token cannot see this repo" : "") + "</div>");
      }
    });
    if (locked) {
      parts.push("<div style='color:var(--ic-mut)'>Already onboarded: adding a repo invites them now. To remove a repo, remove them on GitHub.</div>");
    }
    {
      var opts = PERMS.map(function (x) { return "<option value='" + x + "'>" + x + "</option>"; }).join("");
      parts.push(
        "<div style='margin-top:7px;display:flex;gap:5px;flex-wrap:wrap;align-items:center'>" +
        "<input id='repoaccess-add-repo' placeholder='owner/name' aria-label='Repository (owner/name)' style='flex:1 1 6em;min-width:5em;background:var(--ic-well);border:1px solid var(--ic-bd);border-radius:6px;color:inherit;padding:7px 10px'>" +
        "<select id='repoaccess-add-perm' aria-label='Permission' style='width:auto;flex:none;background:var(--ic-well);border:1px solid var(--ic-bd);border-radius:6px;color:inherit;padding:7px 8px'>" + opts + "</select>" +
        "<button id='repoaccess-add-btn' type='button' class='primary'>Add</button>" +
        "</div>"
      );
    }
    body.innerHTML = parts.join("");

    // Wire remove links.
    body.querySelectorAll("[data-rmrepo]").forEach(function (el) {
      el.addEventListener("click", function (ev) {
        ev.preventDefault();
        postRepoOverride(container, p, v, { removeRepo: el.getAttribute("data-rmrepo") });
      });
    });
    // Wire add.
    var btn = body.querySelector("#repoaccess-add-btn");
    if (btn) btn.addEventListener("click", function () {
      var repo = (body.querySelector("#repoaccess-add-repo").value || "").trim();
      var perm = body.querySelector("#repoaccess-add-perm").value || "pull";
      if (!repo) return;
      postRepoOverride(container, p, v, { addRepo: repo, addPerm: perm });
    });
  }

  // postRepoOverride merges one change into the current overrides and POSTs the
  // full desired set back (the API replaces the stored overrides), then re-renders.
  function postRepoOverride(container, p, v, change) {
    var ov = v.overrides || { add: [], remove: [] };
    var add = arr(ov.add).slice(), remove = arr(ov.remove).slice();
    var eqRepo = function (a, b) { return String(a).toLowerCase() === String(b).toLowerCase(); };
    if (change.addRepo) {
      add = add.filter(function (g) { return !eqRepo(g.repo, change.addRepo); });
      add.push({ repo: change.addRepo, permission: change.addPerm });
    }
    if (change.removeRepo) {
      add = add.filter(function (g) { return !eqRepo(g.repo, change.removeRepo); });
      if (!remove.some(function (g) { return eqRepo(g.repo, change.removeRepo); })) {
        remove.push({ repo: change.removeRepo });
      }
    }
    var body = container.querySelector("#repoaccess-body");
    if (body) body.style.opacity = "0.5";
    var csrf = document.querySelector("input[name=csrf]");
    fetch("/onboarding/" + encodeURIComponent(p.id) + "/repo-access", {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json", "Accept": "application/json", "X-CSRF-Token": csrf ? csrf.value : "" },
      body: JSON.stringify({ add: add, remove: remove })
    }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (j) {
        if (!r.ok) throw new Error((j.error && j.error.message) || ("HTTP " + r.status));
        return j;
      });
    }).then(function (nv) {
      renderRepoBody(container, p, nv, p.status === "onboarded" || p.status === "enrolled");
    }).catch(function (e) {
      if (body) { body.style.opacity = "1"; }
      var note = document.createElement("div");
      note.style.color = "var(--ic-err)"; note.textContent = "not saved: " + e.message;
      if (body) body.appendChild(note);
    });
  }

  window.renderIdentityCard = renderIdentityCard;
})();
