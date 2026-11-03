// goat-zenoh-portal — Data Access screen (ADR 1075 discover->request->approve
// + preemptive grants). Vanilla ES module; talks to the /api/access/* surface
// behind the portal API key (app.js handles the bearer).

import { api, escapeHTML, hydrateAuth } from "/app.js";

const $ = (id) => document.getElementById(id);

// A short honest badge for a producer's effective access lane: a restricted
// lane (org|controlled) draws the warn pill so the catalog reads at a glance.
function accessBadge(eff) {
  const cls = eff === "community" ? "pill" : "pill pill-warn";
  return `<span class="${cls}">${escapeHTML(eff || "community")}</span>`;
}

async function loadCatalog() {
  const el = $("catalog");
  let orgs = [];
  try {
    const resp = await api("/api/orgs");
    orgs = resp.orgs || resp.vendors || [];
  } catch (e) {
    el.innerHTML = `<p class="warn">${escapeHTML(e.message)}</p>`;
    return;
  }
  if (!orgs.length) {
    el.innerHTML = `<p class="hint">No producers registered yet.</p>`;
    return;
  }
  const rows = orgs.map((v) => {
    const eff = v.effective_access || "community";
    const restricted = eff !== "community";
    const labels = [v.fidelity, v.context].filter(Boolean).map((x) => `<span class="pill">${escapeHTML(x)}</span>`).join(" ");
    const reqBtn = restricted
      ? `<button data-request="${escapeHTML(v.prefix)}">Request access</button>`
      : `<span class="hint">open to all</span>`;
    return `<tr>
      <td><code>${escapeHTML(v.prefix)}</code></td>
      <td>${accessBadge(eff)} ${labels}</td>
      <td>${reqBtn}</td>
    </tr>`;
  }).join("");
  el.innerHTML = `<table><thead><tr><th>Producer</th><th>Labels</th><th></th></tr></thead><tbody>${rows}</tbody></table>
    <p id="request-status" class="hint" role="status"></p><div id="request-form"></div>`;
  el.querySelectorAll("[data-request]").forEach((b) =>
    b.addEventListener("click", () => showRequestForm(b.getAttribute("data-request"))));
}

// Inline request form — kept minimal: a grantee (org or your identity UUID) + a
// justification, then POST. Refreshes the inbox on success.
function showRequestForm(producer) {
  const el = $("request-form");
  $("request-status").textContent = "";
  el.innerHTML = `
    <form id="rq">
      <p>Request access to <code>${escapeHTML(producer)}</code></p>
      <label>Grantee scope
        <select id="rq-kind"><option value="org">org</option><option value="principal">person (identity UUID)</option></select>
      </label>
      <label>Grantee <input id="rq-grantee" type="text" placeholder="org id or your identity UUID" list="directory-list" required></label>
      <label>Justification <input id="rq-justification" type="text" placeholder="why you need it"></label>
      <button type="submit">Submit request</button>
      <span id="rq-msg" class="hint"></span>
    </form>`;
  $("rq").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    try {
      await api("/api/access/requests", {
        method: "POST",
        body: JSON.stringify({
          producer,
          grantee_kind: $("rq-kind").value,
          grantee_id: $("rq-grantee").value.trim(),
          justification: $("rq-justification").value.trim(),
        }),
      });
      $("request-status").textContent = "Requested — the producer's custodian will decide.";
      el.innerHTML = "";
      loadInbox();
    } catch (e) {
      $("rq-msg").textContent = e.message;
    }
  });
}

async function loadInbox() {
  const el = $("inbox");
  let rows = [];
  try {
    rows = (await api("/api/access/requests?status=pending")).requests || [];
  } catch (e) {
    el.innerHTML = `<p class="warn">${escapeHTML(e.message)}</p>`;
    return;
  }
  if (!rows.length) {
    el.innerHTML = `<p class="hint">No pending requests.</p>`;
    return;
  }
  el.innerHTML = `<table><thead><tr><th>Producer</th><th>Requester</th><th>Grantee</th><th>Why</th><th></th></tr></thead><tbody>${
    rows.map((r) => `<tr>
      <td><code>${escapeHTML(r.producer)}</code></td>
      <td>${escapeHTML(r.requester_label || r.requester_id)}</td>
      <td>${escapeHTML(r.grantee_kind)}:${escapeHTML(r.grantee_id)}</td>
      <td>${escapeHTML(r.justification || "")}</td>
      <td>
        <button data-approve="${escapeHTML(r.id)}">Approve</button>
        <button data-decline="${escapeHTML(r.id)}">Decline</button>
      </td>
    </tr>`).join("")
  }</tbody></table>`;
  el.querySelectorAll("[data-approve]").forEach((b) =>
    b.addEventListener("click", () => decide(b.getAttribute("data-approve"), "approve")));
  el.querySelectorAll("[data-decline]").forEach((b) =>
    b.addEventListener("click", () => decide(b.getAttribute("data-decline"), "decline")));
}

async function decide(id, action) {
  try {
    await api(`/api/access/requests/${id}/${action}`, { method: "POST", body: "{}" });
    loadInbox();
    loadGrants();
  } catch (e) {
    alert(e.message);
  }
}

async function loadGrants() {
  const el = $("grants");
  let rows = [];
  try {
    rows = (await api("/api/access/grants")).grants || [];
  } catch (e) {
    el.innerHTML = `<p class="warn">${escapeHTML(e.message)}</p>`;
    return;
  }
  if (!rows.length) {
    el.innerHTML = `<p class="hint">No grants yet.</p>`;
    return;
  }
  el.innerHTML = `<table><thead><tr><th>Producer</th><th>Grantee</th><th>By</th><th>Expires</th><th>Status</th><th></th></tr></thead><tbody>${
    rows.map((g) => {
      const exp = g.effective_to ? new Date(g.effective_to * 1000).toISOString().slice(0, 10) : "—";
      const revoke = g.status === "active"
        ? `<button data-revoke="${escapeHTML(g.id)}">Revoke</button>`
        : "";
      return `<tr>
        <td><code>${escapeHTML(g.producer)}</code></td>
        <td>${escapeHTML(g.grantee_kind)}:${escapeHTML(g.grantee_id)}</td>
        <td>${escapeHTML(g.granted_by)}</td>
        <td>${exp}</td>
        <td><span class="pill${g.status === "active" ? "" : " pill-warn"}">${escapeHTML(g.status)}</span></td>
        <td>${revoke}</td>
      </tr>`;
    }).join("")
  }</tbody></table>`;
  el.querySelectorAll("[data-revoke]").forEach((b) =>
    b.addEventListener("click", async () => {
      if (b.disabled) return;
      const id = b.getAttribute("data-revoke");
      if (!confirm(`Revoke grant ${id}? Access through this grant will end.`)) return;
      b.disabled = true;
      b.parentElement.querySelector('[role="alert"]')?.remove();
      try {
        await api(`/api/access/grants/${encodeURIComponent(id)}/revoke`, { method: "POST", body: "{}" });
        await loadGrants();
      } catch (e) {
        const error = document.createElement("span");
        error.className = "warn revoke-error";
        error.setAttribute("role", "alert");
        error.textContent = e.message;
        b.after(error);
      } finally {
        b.disabled = false;
      }
    }));
}

async function loadDirectory() {
  const list = $("directory-list");
  try {
    const dir = await api("/api/access/directory");
    const opts = [];
    (dir.orgs || []).forEach((o) => opts.push(`<option value="${escapeHTML(o.id)}">org: ${escapeHTML(o.id)}</option>`));
    (dir.people || []).forEach((p) => opts.push(`<option value="${escapeHTML(p.id)}">${escapeHTML(p.name || p.email || p.id)}</option>`));
    list.innerHTML = opts.join("");
  } catch (e) {
    // Directory is a convenience; a failure never blocks a manual grantee entry.
  }
}

function wirePreempt() {
  const form = $("preempt-form");
  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const body = {
      producer: $("p-producer").value.trim(),
      grantee_kind: $("p-kind").value,
      grantee_id: $("p-grantee").value.trim(),
      justification: $("p-justification").value.trim(),
    };
    const exp = $("p-expiry").value;
    if (exp) body.effective_to = Math.floor(new Date(exp).getTime() / 1000);
    try {
      await api("/api/access/grants", { method: "POST", body: JSON.stringify(body) });
      $("preempt-msg").textContent = "Granted.";
      form.reset();
      loadGrants();
    } catch (e) {
      $("preempt-msg").textContent = e.message;
    }
  });
}

async function init() {
  await hydrateAuth();
  wirePreempt();
  loadCatalog();
  loadInbox();
  loadGrants();
  loadDirectory();
}

init();
