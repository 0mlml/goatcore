import { showRequestError } from "/app.js";
// goat-zenoh-portal — topic inspector screen.
//
// Decode dispatch — pick the format from the dropdown override (if any),
// otherwise from the topic's registry entry, otherwise default to JSON.
// Format decoders:
//
//   json     — JSON.parse + pretty stringify
//   cbor     — dynamic-import("/decoders.js") cborDecode (vendored CBOR)
//   msgpack  — dynamic-import("/decoders.js") msgpackDecode (vendored MessagePack)
//   text     — UTF-8 decode + line-break preservation
//   raw      — hex view + length
//   protobuf:<MsgName> — dispatches to GET /api/topics/decoded which loads
//                       the .fds via the schema store and decodes with dynamicpb
//
// The vendored decoders are intentionally tiny — full-fidelity CBOR /
// MessagePack libs are out of scope for trial; we cover the common
// shapes producers use.

import { api, escapeHTML } from "/app.js";
import { wireStream } from "/inspector-stream.js";

async function initInspector() {
  const params = new URLSearchParams(location.search);
  const initialKey = params.get("key") || "";
  document.getElementById("key-expr").value = initialKey;
  document.getElementById("fetch-latest").addEventListener("click", fetchLatest);
  wireStream(decodeSample);
  document.getElementById("browse-vendor").addEventListener("change", onVendorChange);
  document.getElementById("browse-topic").addEventListener("change", onTopicChange);
  populateVendors().catch((e) => showRequestError(e));
  if (initialKey) fetchLatest();
}

async function populateVendors() {
  const select = document.getElementById("browse-vendor");
  const resp = await api("/api/orgs");
  // ADR 1052 Phase 3: prefer the `orgs` listing key, fall back to `vendors`.
  const vendors = (resp.orgs || resp.vendors || []).slice().sort((a, b) => a.prefix.localeCompare(b.prefix));
  for (const v of vendors) {
    const opt = document.createElement("option");
    opt.value = v.prefix;
    const tag = v.synthetic ? " (synthetic)" : (v.approved === false ? " (pending)" : "");
    opt.textContent = v.prefix + tag;
    select.appendChild(opt);
  }
}

async function onVendorChange(e) {
  const vendor = e.target.value;
  const topicSel = document.getElementById("browse-topic");
  topicSel.innerHTML = '<option value="">— topic —</option>';
  topicSel.disabled = true;
  if (!vendor) return;
  try {
    const data = await api(`/api/orgs/${encodeURIComponent(vendor)}/topics`);
    const topics = (data.topics || []).slice().sort((a, b) => a.key_pattern.localeCompare(b.key_pattern));
    for (const t of topics) {
      const opt = document.createElement("option");
      opt.value = t.key_pattern;
      opt.dataset.format = t.format || "";
      opt.textContent = `${t.key_pattern}  [${t.format || "?"}]`;
      topicSel.appendChild(opt);
    }
    topicSel.disabled = topics.length === 0;
  } catch (err) {
    const opt = document.createElement("option");
    opt.value = "";
    opt.textContent = `(error: ${err.message})`;
    topicSel.appendChild(opt);
  }
}

function onTopicChange(e) {
  const sel = e.target;
  const key = sel.value;
  if (!key) return;
  document.getElementById("key-expr").value = key;
  // Stash the picked topic's registered format on the key input so
  // fetchLatest can honor "(use registered)" — without this, the dropdown
  // default silently falls back to JSON and protobuf topics never reach
  // the /api/topics/decoded path.
  const opt = sel.options[sel.selectedIndex];
  document.getElementById("key-expr").dataset.registeredFormat = (opt && opt.dataset.format) || "";
}

async function fetchLatest() {
  const keyEl = document.getElementById("key-expr");
  const key = keyEl.value.trim();
  const out = document.getElementById("latest");
  out.textContent = "Fetching...";
  if (!key) { out.textContent = "no key"; return; }
  const override = document.getElementById("format-override").value;
  const registered = keyEl.dataset.registeredFormat || "";
  const fmt = override || registered || "json";

  // Block 85 PR-C — protobuf topics dispatch to the server-side decoded
  // endpoint, which fetches the .fds via the schemastore + decodes with
  // dynamicpb. Browser doesn't bundle a protobuf library. xml / xml:<Root>
  // topics dispatch there too: the server projects the XML document to
  // JSON (root checked against the declared element).
  if (fmt.startsWith("protobuf:") || fmt.startsWith("xml:") || fmt === "xml") {
    try {
      const resp = await api(`/api/topics/decoded?key=${encodeURIComponent(key)}`);
      out.textContent = renderDecodedResponse(resp);
    } catch (e) {
      out.textContent = "Error: " + e.message;
    }
    return;
  }

  try {
    const resp = await api(`/api/topics/latest?key=${encodeURIComponent(key)}`);
    if (!resp.samples || resp.samples.length === 0) {
      out.textContent = "(no samples)";
      return;
    }
    const lines = [];
    for (const s of resp.samples) {
      const decoded = await decodeSample(s, fmt);
      lines.push(`# ${s.key} @ ${s.timestamp || "?"} (encoding=${s.encoding || "?"}, source=${s.source || "?"})`);
      lines.push(decoded);
      lines.push("");
    }
    out.textContent = lines.join("\n");
  } catch (e) {
    out.textContent = "Error: " + e.message;
  }
}

// renderDecodedResponse formats the /api/topics/decoded response. The
// server returns either a decoded JSON object per sample or a per-sample
// error string when extract/decode fails.
function renderDecodedResponse(resp) {
  const lines = [];
  lines.push(`# format=${resp.format}  schema=${resp.schema_version}`);
  if (!resp.samples || resp.samples.length === 0) {
    lines.push("(no samples)");
    return lines.join("\n");
  }
  for (const s of resp.samples) {
    lines.push(`# ${s.timestamp || "?"} (encoding=${s.encoding || "?"}, source=${s.source || "?"})`);
    if (s.error) {
      lines.push(`(decode error: ${s.error})`);
    } else if (s.decoded) {
      lines.push(JSON.stringify(s.decoded, null, 2));
    } else {
      lines.push("(empty)");
    }
    lines.push("");
  }
  return lines.join("\n");
}

async function decodeSample(s, formatHint) {
  // s.Value is the raw JSON returned by zenohd, which carries the
  // payload. For a JSON-encoding sample the value IS the parsed JSON.
  // For binary encodings the value is base64-encoded or hex; we try a
  // few likely shapes.
  let value = s.value;
  if (typeof value === "object") {
    if (formatHint === "json") {
      return JSON.stringify(value, null, 2);
    }
    if (value && typeof value.b64 === "string") {
      value = base64ToBytes(value.b64);
    } else if (value && typeof value.hex === "string") {
      value = hexToBytes(value.hex);
    } else if (Array.isArray(value)) {
      value = new Uint8Array(value);
    } else {
      return JSON.stringify(value, null, 2);
    }
  } else if (typeof value === "string") {
    // string-value: heuristic decode.
    if (formatHint === "text") return value;
    if (formatHint === "raw") return hexDump(new TextEncoder().encode(value));
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch (_) {
      // fall through; treat as bytes
      value = new TextEncoder().encode(value);
    }
  }

  // value is now Uint8Array.
  if (!(value instanceof Uint8Array)) {
    return `(unable to decode; raw value: ${JSON.stringify(s.value).slice(0, 200)})`;
  }
  switch (formatHint) {
    case "text":
      return new TextDecoder().decode(value);
    case "raw":
      return hexDump(value);
    case "cbor": {
      const { cborDecode } = await import("/decoders.js");
      try {
        return JSON.stringify(cborDecode(value), null, 2);
      } catch (e) {
        return `(cbor decode failed: ${e.message})\n` + hexDump(value);
      }
    }
    case "msgpack": {
      const { msgpackDecode } = await import("/decoders.js");
      try {
        return JSON.stringify(msgpackDecode(value), null, 2);
      } catch (e) {
        return `(msgpack decode failed: ${e.message})\n` + hexDump(value);
      }
    }
    default:
      if (formatHint && formatHint.startsWith("protobuf:")) {
        // protobuf decode dispatches to /api/topics/decoded upstream in
        // fetchLatest; this branch only fires for the live-stream path,
        // which doesn't have a per-sample server-decode wired yet. Show
        // a clear pointer rather than empty hex.
        return `(protobuf decode lives on /api/topics/decoded; live-stream protobuf decode is a follow-on. Showing raw bytes:)\n` + hexDump(value);
      }
      if (formatHint === "xml" || formatHint.startsWith("xml:")) {
        // XML is self-describing text; render it directly on the
        // live-stream path (the server /api/topics/decoded projection is
        // used for the latest-sample path).
        return new TextDecoder().decode(value);
      }
      // Default json attempt.
      try {
        return JSON.stringify(JSON.parse(new TextDecoder().decode(value)), null, 2);
      } catch (_) {
        return hexDump(value);
      }
  }
}

function base64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function hexToBytes(hex) {
  const clean = hex.replace(/\s+/g, "");
  const out = new Uint8Array(clean.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16);
  return out;
}

function hexDump(bytes) {
  const lines = [];
  for (let i = 0; i < bytes.length; i += 16) {
    const chunk = bytes.slice(i, i + 16);
    const hex = Array.from(chunk).map((b) => b.toString(16).padStart(2, "0")).join(" ");
    const ascii = Array.from(chunk).map((b) => (b >= 32 && b < 127 ? String.fromCharCode(b) : ".")).join("");
    lines.push(i.toString(16).padStart(6, "0") + "  " + hex.padEnd(16 * 3 - 1, " ") + "  " + ascii);
  }
  return lines.join("\n");
}

// Re-export for completeness even though escapeHTML isn't used here directly.
void escapeHTML;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", initInspector);
} else {
  initInspector();
}
