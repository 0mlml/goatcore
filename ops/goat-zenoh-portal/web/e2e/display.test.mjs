import { test } from "node:test";
import assert from "node:assert/strict";
import { formatUptime, schemaDownloadLabel, compareDisplayNames } from "../static/display.js";

test("Go durations display completed minutes at boundaries", () => {
  for (const [input, expected] of [
    ["0", "0 min"], ["0s", "0 min"], ["59.999999999s", "0 min"], ["60s", "1 min"],
    ["1h0m0s", "1 h 0 min"], ["24h0m0s", "1 d 0 h 0 min"],
    ["187h44m28.847029146s", "7 d 19 h 44 min"], ["1.5h", "1 h 30 min"],
    ["1ms", "0 min"], ["1µs", "0 min"], ["1ns", "0 min"],
  ]) assert.equal(formatUptime(input), expected, input);
  for (const input of [undefined, null, "", "bad", "-1m", "1m trailing", "NaN", "Infinity", "1d", 60]) {
    assert.equal(formatUptime(input), "unavailable", String(input));
  }
});

test("download labels identify persisted schema bytes, with an honest fallback", () => {
  assert.equal(schemaDownloadLabel({ schema: { format_storage: "xsd" }, source_lang: "xsd" }), "Download .xsd");
  assert.equal(schemaDownloadLabel({ schema: { format_storage: "protobuf-fds" }, source_lang: "proto" }), "Download .fds");
  assert.equal(schemaDownloadLabel({ source_lang: "xsd" }), "Download .xsd");
  assert.equal(schemaDownloadLabel({ source_lang: "proto" }), "Download .fds");
  assert.equal(schemaDownloadLabel({ schema: { format_storage: "future" }, source_lang: "proto" }), "Download schema");
  assert.equal(schemaDownloadLabel({}), "Download schema");
});

test("ordering uses names, blank-name fallback, and stable anchor tie-breaks", () => {
  const rows = [
    { prefix: "0000", display_name: "Zulu" }, { prefix: "ffff", display_name: "Alpha" },
    { prefix: "cccc", display_name: "alpha" }, { prefix: "bbbb", display_name: "  " },
  ];
  assert.deepEqual([...rows].sort(compareDisplayNames).map((r) => r.prefix), ["cccc", "ffff", "bbbb", "0000"]);
  assert.equal(rows[0].prefix, "0000");
});
