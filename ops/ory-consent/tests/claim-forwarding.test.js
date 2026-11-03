// ops/ory-consent/tests/claim-forwarding.test.js
//
// Behavioural test of patches 1 and 4 (groups and aff forwarding into the JWT
// id_token) after the F-572 / ADR 1188 move to admin-managed metadata_public.
// Runs on bare node, no image build, so it runs on every PR.
//
// Run:  node ops/ory-consent/tests/claim-forwarding.test.js
//
// The injected code is READ OUT OF apply-patch.sh (its awk `print indent "..."`
// lines), not copied here, so the test runs what the image runs.
//
// The property that matters: once metadata_public has a groups key, the
// identity is migrated and self-editable traits are never forwarded, whatever
// that key holds. A null there used to fall through to traits.groups, so a
// holder demoted to no groups could self-grant PLATFORM-ADMIN in settings.
//
// It also holds the shape verify-patched.sh greps for in the built image: the
// id_token assignment within two lines of each marker. That check only runs on
// a main push, after merge, so a layout that breaks it is caught here instead.
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const src = fs.readFileSync(path.join(__dirname, "..", "apply-patch.sh"), "utf8");

// The awk body of one patch: the `print indent ...` lines between its MARKER
// assignment and the next patch's, with the awk string concatenation undone.
function injected(marker, nextMarkerVar) {
  const start = src.indexOf(`='${marker}'`);
  const end = src.indexOf(`${nextMarkerVar}=`, start);
  if (start < 0 || end < 0) throw new Error(`patch block for ${marker} not found in apply-patch.sh`);
  return src
    .slice(start, end)
    .split("\n")
    .map((l) => l.match(/^\s*print indent (.*)$/))
    .filter(Boolean)
    .map((m) => m[1].replace(/" marker "/g, marker))
    .map((s) => s.replace(/^"|"$/g, "").replace(/\\"/g, '"').replace(/\\\\/g, "\\"));
}

const groupsLines = injected("goat-maintainer-groups-forwarding-patch", "MARKER4");
const affLines = injected("goat-maintainer-aff-forwarding-patch", "MARKER2");

// Both patches land at the same anchor inside one function, groups first.
const run = vm.runInNewContext(
  `(function (identity) { var session = { id_token: {} };\n${groupsLines.join("\n")}\n${affLines.join("\n")}\nreturn session.id_token; })`,
);

let ok = true;
const check = (name, pass) => {
  console.log(`  ${pass ? "PASS" : "FAIL"}  ${name}`);
  if (!pass) ok = false;
};
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

const selfGrant = { email: "u@example.test", groups: ["PLATFORM-ADMIN"], aff: { id: "evil", kind: "vendor" } };
const orgAff = { id: "acme", kind: "vendor" };

console.log("groups claim");
check("not migrated: traits.groups forwarded", same(run({ traits: selfGrant }).groups, ["PLATFORM-ADMIN"]));
check("migrated: metadata_public.groups forwarded, traits ignored",
  same(run({ traits: selfGrant, metadata_public: { groups: ["IAM-ADMIN"] } }).groups, ["IAM-ADMIN"]));
check("migrated with []: no claim, traits ignored",
  run({ traits: selfGrant, metadata_public: { groups: [] } }).groups === undefined);
check("migrated with null: no claim, traits ignored",
  run({ traits: selfGrant, metadata_public: { groups: null } }).groups === undefined);
check("metadata_public without a groups key: traits (not migrated)",
  same(run({ traits: selfGrant, metadata_public: { org: {} } }).groups, ["PLATFORM-ADMIN"]));

console.log("aff claim");
check("not migrated: traits.aff forwarded", same(run({ traits: selfGrant }).aff, selfGrant.aff));
check("migrated: metadata_public.aff forwarded, traits ignored",
  same(run({ traits: selfGrant, metadata_public: { groups: [], aff: orgAff } }).aff, orgAff));
check("migrated with null groups and no aff: traits.aff ignored",
  run({ traits: selfGrant, metadata_public: { groups: null } }).aff === undefined);

console.log("verify-patched.sh window (marker + 2 lines)");
const windowHas = (lines, marker, needle) => {
  const i = lines.findIndex((l) => l.includes(marker));
  return i >= 0 && lines.slice(i, i + 3).some((l) => l.includes(needle));
};
check("groups marker is followed by session.id_token.groups",
  windowHas(groupsLines, "goat-maintainer-groups-forwarding-patch", "session.id_token.groups"));
check("aff marker is followed by session.id_token.aff",
  windowHas(affLines, "goat-maintainer-aff-forwarding-patch", "session.id_token.aff"));

console.log(ok ? "claim forwarding: OK" : "claim forwarding: BROKEN");
process.exit(ok ? 0 : 1);
