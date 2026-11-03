// Documented, stable exit codes per docs/design/goat-cli.md
// "Error UX — informative, structural, ZTOL-aware" §Exit codes.
// CI scripts grep on these; changing a value is a breaking change.
//
// Module-level allow: this is the canonical table — every code is here
// even if Phase B doesn't emit it yet. EXIT_USAGE comes from clap's
// own exit path; EXIT_TIMEOUT/EXIT_SIGINT land in Phase D's pub/sub
// + signal-handler code.
#![allow(dead_code)]

pub const EXIT_SUCCESS: i32 = 0;
pub const EXIT_GENERIC: i32 = 1;
pub const EXIT_USAGE: i32 = 2;
pub const EXIT_PROFILE: i32 = 3;
pub const EXIT_PORTAL: i32 = 4;
pub const EXIT_OFF_MESH: i32 = 5;
pub const EXIT_CERT: i32 = 6;
/// The portal answered HTTP 404: the thing named is not registered (#2087).
pub const EXIT_PORTAL_NOT_FOUND: i32 = 7;
/// The portal answered HTTP 429: rate-limited; retrying later can succeed.
pub const EXIT_PORTAL_RATE_LIMITED: i32 = 8;
/// The portal answered HTTP 5xx: it is up and failed on its own side.
pub const EXIT_PORTAL_SERVER_ERROR: i32 = 9;

// `goat federate` (docs/design/federation-package-spec.md). Four of these are
// OUTCOMES rather than faults, and they carry distinct codes so a wrapper can
// tell them apart without parsing prose: a composition refusal is the
// mechanism working, and a malformed package is a different finding from a
// pair that refuses. Conflating them is the specific way this kind of tooling
// goes quietly wrong.
/// Composition refused — one of §6's seven rules fired. No agreement, no digest.
pub const EXIT_FED_REFUSED: i32 = 20;
/// One package is malformed by §3/§4 — refused before composition begins.
pub const EXIT_FED_MALFORMED: i32 = 21;
/// Two agreements disagree, by field or by bytes.
pub const EXIT_FED_MISMATCH: i32 = 22;
/// A value could not be derived from the running system, and is never guessed.
pub const EXIT_FED_UNDERIVED: i32 = 23;
/// The agreement would not be valid on our own systems — nothing attested.
pub const EXIT_FED_NOT_CONFORMANT: i32 = 24;

pub const EXIT_TIMEOUT: i32 = 124;
pub const EXIT_SIGINT: i32 = 130;
