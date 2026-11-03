// Presentation only: wire durations, schema metadata, and namespace anchors
// remain unchanged. Kept independent of the DOM for boundary-case tests.
export function formatUptime(value) {
  if (value === "0") return "0 min";
  if (typeof value !== "string" || !value) return "unavailable";
  const units = { h: 3600, m: 60, s: 1, ms: 0.001, us: 0.000001, "µs": 0.000001, "μs": 0.000001, ns: 0.000000001 };
  const parts = [...value.matchAll(/(\d+(?:\.\d+)?)(h|ms|µs|μs|us|ns|m|s)/g)];
  if (!parts.length || parts.map((p) => p[0]).join("") !== value) return "unavailable";
  const seconds = parts.reduce((n, p) => n + Number(p[1]) * units[p[2]], 0);
  if (!Number.isFinite(seconds)) return "unavailable";
  const minutes = Math.floor(seconds / 60);
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor(minutes / 60) % 24;
  const remainder = minutes % 60;
  return [days ? `${days} d` : "", days || hours ? `${hours} h` : "", `${remainder} min`].filter(Boolean).join(" ");
}

export function schemaDownloadLabel(data) {
  const format = data.schema?.format_storage;
  if (format === "xsd" || (!format && data.source_lang === "xsd")) return "Download .xsd";
  if (format === "protobuf-fds" || (!format && data.source_lang === "proto")) return "Download .fds";
  return "Download schema";
}

export function compareDisplayNames(a, b) {
  const name = (row) => (row.display_name || "").trim() || row.prefix;
  return name(a).localeCompare(name(b), undefined, { sensitivity: "base", numeric: true })
    || a.prefix.localeCompare(b.prefix);
}
