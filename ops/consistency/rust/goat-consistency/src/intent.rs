// SPDX-License-Identifier: Apache-2.0
//
// intent.rs — reading IntentV1, Rust side.
//
// The Rust half never BUILDS an intent artifact: the generator is Python and
// stays Python (§3.6 — one resolver, two renderings). What this module does is
// read a committed generation and reproduce its `content_hash` from its own
// body, which is the cross-language contract the golden corpus pins.
//
// That contract is not decoration. CF-5 signs SHA-256 over the EXACT canonical
// bytes; if the Rust serialization differed from the Python one by a space, the
// two implementations would compute different hashes for the same document and
// every reader would grade every node `unaccountable`. So the canonical form is
// stated once here, mirrored from goat_consistency/intent.py, and the corpus
// asserts the reproduction on every committed fixture.
//
// Canon: docs/design/consistency-foundation.md §3.3 / §3.3a.

use serde_json::{Map, Value};
use sha2::{Digest, Sha256};

/// The three fields generation identity is computed WITHOUT (§3.3): a document
/// cannot hash its own hash, and `serial`/`generated_at` are the human-facing
/// NAME of a generation rather than part of what makes it one.
pub const UNHASHED: [&str; 3] = ["serial", "generated_at", "content_hash"];

/// How many predecessor generations the artifact carries identity triples for
/// (§3.3a). Mirrors `goat_consistency.intent.HISTORY_DEPTH`; it appears in a
/// reader-facing sentence, so the two must agree.
pub const HISTORY_DEPTH: usize = 10;

/// THE serialization contract: keys sorted, two-space indent, UTF-8, one
/// trailing newline — byte-identical to Python's
/// `json.dumps(obj, sort_keys=True, indent=2, ensure_ascii=False) + "\n"`.
///
/// `serde_json::Map` is a BTreeMap unless the `preserve_order` feature is on,
/// so keys sort; `to_string_pretty` emits two-space indent and `": "`. Any
/// change here invalidates every CF-5 signature and is a FORMAT change, not a
/// formatting one.
pub fn canonical_json(value: &Value) -> String {
    serde_json::to_string_pretty(value).unwrap_or_default() + "\n"
}

pub fn sha256_of(bytes: &[u8]) -> String {
    let mut h = Sha256::new();
    h.update(bytes);
    format!("sha256:{}", hex::encode(h.finalize()))
}

/// SHA-256 over the canonical body, excluding the generation-identity fields.
/// A deterministic function of the tree that both sides compute — which is why
/// it, and not the git commit, is the comparison key: two commits routinely
/// produce identical intent, and comparing on commit would grade every node
/// diverged after every unrelated merge (§3.3).
pub fn content_hash(body: &Value) -> String {
    let mut trimmed = Map::new();
    if let Some(obj) = body.as_object() {
        for (k, v) in obj {
            if !UNHASHED.contains(&k.as_str()) {
                trimmed.insert(k.clone(), v.clone());
            }
        }
    }
    sha256_of(canonical_json(&Value::Object(trimmed)).as_bytes())
}

/// Whether a committed document's `content_hash` is the hash of its own body.
/// One layer of the §3.4 integrity story: the CF-5 signature (verify.rs)
/// proves WHO produced the bytes, this proves the body still IS those bytes'
/// claim about itself, and the CI regenerate-diff proves the tree still
/// agrees. A reader checks the first two before grading against an artifact
/// it read off disk.
pub fn self_consistent(doc: &Value) -> bool {
    doc.get("content_hash").and_then(|v| v.as_str()) == Some(content_hash(doc).as_str())
}

/// Parse a `%Y-%m-%dT%H:%M:%SZ` stamp into unix seconds. The artifact writes
/// exactly one format; anything else returns None rather than a guess, which is
/// the same refusal `goat_consistency.grading._parse_stamp` makes.
pub fn parse_utc_stamp(text: &str) -> Option<i64> {
    let b = text.as_bytes();
    if b.len() != 20 || b[4] != b'-' || b[7] != b'-' || b[10] != b'T' || b[13] != b':'
        || b[16] != b':' || b[19] != b'Z'
    {
        return None;
    }
    let num = |from: usize, to: usize| -> Option<i64> { text[from..to].parse::<i64>().ok() };
    let (y, mo, d) = (num(0, 4)?, num(5, 7)?, num(8, 10)?);
    let (h, mi, s) = (num(11, 13)?, num(14, 16)?, num(17, 19)?);
    if !(1..=12).contains(&mo) || !(1..=31).contains(&d) || h > 23 || mi > 59 || s > 60 {
        return None;
    }
    Some(days_from_civil(y, mo, d) * 86_400 + h * 3600 + mi * 60 + s)
}

/// Days since 1970-01-01 (Howard Hinnant's civil-from-days, inverted). Written
/// out rather than pulled in as a dependency: this is the only date arithmetic
/// the crate needs, and a container that grades convergence should not carry a
/// timezone database to do it.
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let doy = (153 * (if m > 2 { m - 3 } else { m + 9 }) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

/// The forward direction (Hinnant's civil_from_days), for the one thing that
/// needs it: turning a caller's clock into the `YYYY-MM-DD` that
/// `diff_intents` compares an `until:` against. Same no-dependency rule as
/// above — a date, not a timezone database.
fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// A unix-millisecond clock as the UTC calendar date `YYYY-MM-DD`.
///
/// This is the ONLY place a wall clock enters a comparison, and it enters as a
/// caller-supplied value rather than a library read: the grading library must
/// give the same answer for the same artifacts in every process, so the
/// process that HAS a clock dates the comparison and the library stays pure.
/// See `diff_intents`' `today` parameter (rule 4a).
pub fn utc_date(unix_ms: i64) -> String {
    let days = unix_ms.div_euclid(86_400_000);
    let (y, m, d) = civil_from_days(days);
    format!("{y:04}-{m:02}-{d:02}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stamps_parse_to_the_same_instants_python_computes() {
        assert_eq!(parse_utc_stamp("1970-01-01T00:00:00Z"), Some(0));
        assert_eq!(parse_utc_stamp("2026-08-04T00:00:00Z"), Some(1_785_801_600));
        // Leap-year boundary — the class of bug hand-rolled date maths earns.
        assert_eq!(parse_utc_stamp("2024-03-01T00:00:00Z"), Some(1_709_251_200));
        // Anything that is not the artifact's one format is not guessed at.
        assert_eq!(parse_utc_stamp("2026-08-04 00:00:00"), None);
        assert_eq!(parse_utc_stamp(""), None);
    }

    #[test]
    fn canonical_json_sorts_keys_and_ends_with_one_newline() {
        let v: Value = serde_json::json!({"b": 1, "a": {"d": [], "c": {}}});
        assert_eq!(canonical_json(&v), "{\n  \"a\": {\n    \"c\": {},\n    \"d\": []\n  },\n  \"b\": 1\n}\n");
    }
}
