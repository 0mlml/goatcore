// SPDX-License-Identifier: Apache-2.0
//
// grading.rs — THE comparison, Rust execution.
//
// The SECOND of the two implementations ADR 1110 D2 requires, and it is a
// deliberate mirror of ops/consistency/python/goat_consistency/grading.py
// rather than an independent design: a projection may choose which records it
// can see and how it renders; it may NOT grade. Both implementations execute
// ops/consistency/grammar.yaml, and neither defines a grade.
//
// What keeps that honest is not this comment — it is
// ops/consistency/rust/goat-consistency/tests/corpus.rs, which reads the SAME
// bytes out of ops/consistency/testdata/ as the Python runner and asserts the
// SAME expectations, including generated `why` sentences and remediation
// pointers. A behaviour that drifts between the two languages fails there.
//
// Verdicts are computed at READ TIME and stored nowhere. A node knows what it
// is running; it does not know whether that is current, because that is a
// comparison against a tree it may not have.
//
// Canon: docs/design/consistency-foundation.md §4 (ADR 1110 D2/D4).

use std::collections::BTreeMap;

use serde_json::{Map, Value};

use crate::grammar::{Grammar, GrammarError, Remediation};
use crate::intent::{parse_utc_stamp, HISTORY_DEPTH};

/// The comparable stand-in for a member that deliberately does not run a
/// component. Two absent members agree; absent-vs-present is a difference, and
/// since absence is only ever reachable through a declared deviation, that
/// difference lands as `declared` rather than `SKEW`.
pub const ABSENT_KEY: &str = "<absent>";

/// Lifting a cell STATE into a grade. A `resolved` cell is compared by value
/// and an `absent` cell compares as the sentinel above; every other state is
/// already an answer and lifts straight to its grade. Kept explicit because the
/// whole honesty grammar rests on `unset` and `unreadable` staying apart —
/// nothing to read, versus read it and could not evaluate it.
///
/// `behind` lifts to SKEW on its own rather than by comparing values, and that
/// is the point: three nodes that agree with each other on a SUPERSEDED
/// generation are not `match`. The reference they are behind is the artifact,
/// not each other.
fn state_grade(state: &str) -> Option<&'static str> {
    match state {
        "unreadable" => Some("unreadable"),
        "unset" => Some("indeterminate"),
        "unreported" => Some("indeterminate"),
        "unaccountable" => Some("unaccountable"),
        "behind" => Some("SKEW"),
        _ => None,
    }
}

/// One row of the comparison, in the one shape every caller gets (§4.4).
///
/// `grounding` is not decoration: ADR 1109 P15 requires every fleet reading to
/// carry its n, window and vantage, because "100% compliant" that is really
/// "0 nodes reported" is the specific failure it exists to make impossible.
#[derive(Debug, Clone, PartialEq)]
pub struct GradedRow {
    pub subject: String,
    pub name: String,
    pub goatnet: Option<String>,
    pub columns: Vec<String>,
    pub cells: BTreeMap<String, Value>,
    pub grade: String,
    pub why: String,
    pub remediation: Option<Remediation>,
    pub n: usize,
    pub vantage: String,
}

impl GradedRow {
    /// The row as the corpus's `expected` JSON, so a cross-language comparison
    /// is a value comparison rather than a field-by-field ritual.
    pub fn as_corpus_json(&self) -> Value {
        let mut scope = Map::new();
        scope.insert(
            "goatnet".into(),
            self.goatnet.clone().map(Value::String).unwrap_or(Value::Null),
        );
        scope.insert(
            "columns".into(),
            Value::Array(self.columns.iter().cloned().map(Value::String).collect()),
        );
        let mut grounding = Map::new();
        grounding.insert("n".into(), Value::from(self.n));
        grounding.insert("vantage".into(), Value::String(self.vantage.clone()));
        grounding.insert("window_seconds".into(), Value::Null);
        let mut out = Map::new();
        out.insert("subject".into(), Value::String(self.subject.clone()));
        out.insert("name".into(), Value::String(self.name.clone()));
        out.insert("grade".into(), Value::String(self.grade.clone()));
        out.insert("why".into(), Value::String(self.why.clone()));
        out.insert(
            "remediation".into(),
            match &self.remediation {
                None => Value::Null,
                Some(r) => serde_json::to_value(r).unwrap_or(Value::Null),
            },
        );
        out.insert("grounding".into(), Value::Object(grounding));
        let _ = scope;
        Value::Object(out)
    }
}

fn cell_state(cell: &Value) -> &str {
    cell.get("state").and_then(|v| v.as_str()).unwrap_or("")
}

/// A cell's comparable value, or None when there is nothing to compare. A cell
/// that claims `resolved` with no value is not readable, whatever it claims —
/// and `None` is what makes the caller fail toward the ungrounded state rather
/// than toward agreement.
fn cell_key(cell: &Value) -> Option<String> {
    if cell_state(cell) == "absent" {
        return Some(ABSENT_KEY.to_string());
    }
    match cell.get("value") {
        None | Some(Value::Null) => None,
        Some(v) => Some(v.to_string()),
    }
}

/// Python's `str()` over a JSON scalar, so the generated `why` sentence reads
/// identically from both implementations.
fn python_str(v: &Value) -> String {
    match v {
        Value::String(s) => s.clone(),
        Value::Null => "None".to_string(),
        Value::Bool(true) => "True".to_string(),
        Value::Bool(false) => "False".to_string(),
        other => other.to_string(),
    }
}

fn cell_text(cell: &Value) -> String {
    match cell_state(cell) {
        "resolved" => cell.get("value").map(python_str).unwrap_or_else(|| "None".into()),
        "absent" => "absent".to_string(),
        state => match cell.get("detail") {
            Some(d) if !matches!(d, Value::Null) => format!("{} ({})", state, python_str(d)),
            _ => state.to_string(),
        },
    }
}

/// The bare verdict for a set of member/node cells.
///
/// Cells are IntentV1-shaped. Passing the artifact's own cells is deliberate —
/// the repo-side renderer and a reader holding only the artifact run the
/// identical comparison rather than two that agree until they don't.
pub fn grade_cells<'a, I>(cells: I, declared: bool, g: &Grammar) -> Result<String, GrammarError>
where
    I: IntoIterator<Item = &'a Value>,
{
    let cells: Vec<&Value> = cells.into_iter().collect();
    if cells.is_empty() {
        // Nothing was observed. Absence of signal is never green.
        return Ok("indeterminate".to_string());
    }
    let lifted: Vec<&str> = cells.iter().filter_map(|c| state_grade(cell_state(c))).collect();
    let mut verdict = if !lifted.is_empty() {
        g.worst(&lifted)?
    } else {
        let keys: Vec<Option<String>> = cells.iter().map(|c| cell_key(c)).collect();
        if keys.iter().any(|k| k.is_none()) {
            "unreadable".to_string()
        } else {
            let distinct: std::collections::BTreeSet<&String> =
                keys.iter().map(|k| k.as_ref().unwrap()).collect();
            if distinct.len() <= 1 { "match".into() } else { "SKEW".into() }
        }
    };
    // A declaration turns a difference the tree cannot decide into a difference
    // somebody decided. It cannot rescue an ungrounded row: a `reason:` explains
    // a difference, it does not make an unreadable value readable.
    if declared && (verdict == "SKEW" || verdict == "indeterminate") {
        verdict = "declared".to_string();
    }
    Ok(verdict)
}

/// One generated sentence naming what differs and where. Generated, never
/// composed by a renderer, so every surface says the same thing.
fn why_sentence(verdict: &str, cells: &BTreeMap<String, Value>) -> String {
    let listing = cells
        .iter()
        .map(|(scope, cell)| format!("{}={}", scope, cell_text(cell)))
        .collect::<Vec<_>>()
        .join(", ");
    if listing.is_empty() {
        return "nothing reported".to_string();
    }
    let clause = match verdict {
        "match" => "all agree",
        "declared" => "the difference is declared with a reason",
        "SKEW" => "they differ and nothing declares why",
        "unaccountable" => "a reported value is explained by no generation in the tree",
        "unreadable" => "read, not evaluable",
        "indeterminate" => "there was nothing to read",
        other => other,
    };
    format!("{listing} — {clause}")
}

/// The scope a remediation pointer should name: the first cell, in scope order,
/// that is not part of the agreement.
fn offender(verdict: &str, cells: &BTreeMap<String, Value>) -> Option<String> {
    for (scope, cell) in cells {
        if state_grade(cell_state(cell)).is_some() {
            return Some(scope.clone());
        }
    }
    if matches!(verdict, "SKEW" | "declared" | "unaccountable") {
        let keys: Vec<(&String, Option<String>)> =
            cells.iter().map(|(s, c)| (s, cell_key(c))).collect();
        let mut counts: BTreeMap<&Option<String>, usize> = BTreeMap::new();
        for (_, k) in &keys {
            *counts.entry(k).or_insert(0) += 1;
        }
        let majority = counts.values().copied().max().unwrap_or(0);
        for (scope, k) in &keys {
            if counts[k] < majority {
                return Some((*scope).clone());
            }
        }
    }
    cells.keys().next().cloned()
}

/// Grade one row and attach its remediation pointer.
///
/// `vantage` is `repo` (members compared inside the committed tree) or `live`
/// (an intent generation compared against node records). The same grade
/// dispatches to a different pointer from each, because a difference in the
/// tree is declared or edited while a difference against a node is
/// re-converged.
#[allow(clippy::too_many_arguments)]
pub fn grade_row(
    subject: &str,
    name: &str,
    cells: BTreeMap<String, Value>,
    goatnet: Option<&str>,
    vantage: &str,
    declared: bool,
    artifact_type: Option<&str>,
    g: &Grammar,
) -> Result<GradedRow, GrammarError> {
    let verdict = grade_cells(cells.values(), declared, g)?;

    let mut remediation = None;
    if g.needs_remediation(&verdict)? {
        let Some(kind) = g.remediation_kind(&verdict, vantage, artifact_type)? else {
            // Fail-closed, the same shape as "a deviation without a reason: is a
            // schema error". A grade that needs a pointer and has none must not
            // ship — a surface would have to invent the sentence, and inventing
            // it per surface is exactly what remediation-as-data prevents.
            return Err(GrammarError(format!(
                "grade '{verdict}' needs a remediation pointer but ops/consistency/grammar.yaml \
                 declares none for vantage '{vantage}'{}",
                artifact_type
                    .map(|t| format!(" / artifact_type '{t}'"))
                    .unwrap_or_default()
            )));
        };
        let member = offender(&verdict, &cells).unwrap_or_default();
        let env = goatnet.unwrap_or("").replace('_', "-");
        remediation = Some(g.pointer(&kind, &[("name", name), ("member", &member), ("env", &env)])?);
    }

    Ok(GradedRow {
        subject: subject.to_string(),
        name: name.to_string(),
        goatnet: goatnet.map(|s| s.to_string()),
        columns: cells.keys().cloned().collect(),
        why: why_sentence(&verdict, &cells),
        grade: verdict,
        n: cells.len(),
        vantage: vantage.to_string(),
        cells,
        remediation,
    })
}

// ---------------------------------------------------------------------------
// Generation accountability — the §3.3a computation
// ---------------------------------------------------------------------------

fn age_seconds(generated_at: Option<&str>, now: i64) -> Option<i64> {
    let stamp = parse_utc_stamp(generated_at?)?;
    let seconds = now - stamp;
    // A reader whose clock sits BEFORE the generation reports no age rather
    // than a negative one — ADR 1037 makes UTC confidence a thing to state,
    // not assume.
    if seconds >= 0 {
        Some(seconds)
    } else {
        None
    }
}

/// §5.3's sentence: "converged against 9-day-old intent".
fn age_phrase(seconds: Option<i64>) -> String {
    match seconds {
        None => "intent age unstatable".to_string(),
        Some(s) if s >= 86_400 => format!("{}-day-old intent", s / 86_400),
        Some(s) if s >= 3_600 => format!("{}-hour-old intent", s / 3_600),
        Some(s) => format!("{}-minute-old intent", s / 60),
    }
}

fn generation_block(serial: &Value, generated_at: &Value, behind_by: usize, now: i64) -> Value {
    let seconds = age_seconds(generated_at.as_str(), now);
    serde_json::json!({
        "serial": serial,
        "generated_at": generated_at,
        "behind_by": behind_by,
        "intent_age_seconds": seconds,
    })
}

/// Classify ONE node's converged generation against the artifact ALONE.
///
/// This is the computation §3.3a's history tail exists for, and the reason that
/// tail lives inside the hashed body. A reader that holds one generation and no
/// git tree — this rollup meter, the CF-6 console, an ADR 1109 FM-2 node — can
/// otherwise say only "this is not the current hash", which reads identically
/// for a node one bump behind and a node running something nobody ever
/// committed. §4.2 gives those two OPPOSITE first actions: re-run the refresh
/// play, versus do not re-run it because that erases the evidence.
///
/// Returns an IntentV1-shaped cell, so the answer flows into the same
/// `grade_row` every other row uses instead of into a second comparison.
/// `observed_serial` is OPTIONAL and changes no grade. It separates one case the
/// reader could not otherwise name: a node reporting a serial NEWER than this
/// artifact's current generation is not a node nobody can explain, it is a node
/// THIS READER cannot explain because the reader's own copy is stale. Both are
/// `unaccountable` — nothing in this tree explains the value, which is the
/// grade's literal meaning — but the operator action inverts: refresh the
/// reader, do not touch the node.
///
/// Mirrors goat_consistency.grading.account_for in the Python twin; the shared
/// corpus is what keeps the two honest.
pub fn account_for(
    intent: &Value,
    observed_content_hash: &str,
    now: i64,
    observed_serial: Option<i64>,
) -> Value {
    let current = intent.get("content_hash").and_then(|v| v.as_str()).unwrap_or("");
    let serial = intent.get("serial").cloned().unwrap_or(Value::Null);
    let empty = Vec::new();
    let tail: Vec<&Value> = intent
        .get("history")
        .and_then(|v| v.as_array())
        .unwrap_or(&empty)
        .iter()
        .filter(|h| h.is_object())
        .collect();

    if observed_content_hash.is_empty() {
        return serde_json::json!({"state": "unreported", "detail": "no applied-intent stamp"});
    }

    if observed_content_hash == current {
        let gen = generation_block(
            &serial,
            intent.get("generated_at").unwrap_or(&Value::Null),
            0,
            now,
        );
        return serde_json::json!({
            "state": "resolved",
            "value": observed_content_hash,
            "generation": gen,
        });
    }

    for (index, entry) in tail.iter().enumerate() {
        if entry.get("content_hash").and_then(|v| v.as_str()) != Some(observed_content_hash) {
            continue;
        }
        let behind_by = index + 1;
        let gen = generation_block(
            entry.get("serial").unwrap_or(&Value::Null),
            entry.get("generated_at").unwrap_or(&Value::Null),
            behind_by,
            now,
        );
        let age = gen.get("intent_age_seconds").and_then(|v| v.as_i64());
        let detail = format!(
            "converged against serial {} — {} generation{} behind, {}",
            python_str(entry.get("serial").unwrap_or(&Value::Null)),
            behind_by,
            if behind_by == 1 { "" } else { "s" },
            age_phrase(age),
        );
        return serde_json::json!({
            "state": "behind",
            "value": observed_content_hash,
            "detail": detail,
            "generation": gen,
        });
    }

    // Nowhere in the artifact. Whether it fell off the tail or never existed is
    // a distinction this reader cannot make and must not fake — both are
    // `unaccountable`, and the tail's own reach is stated so an operator can see
    // which one they are looking at.
    let mut serials: Vec<i64> = tail
        .iter()
        .filter_map(|h| h.get("serial").and_then(|v| v.as_i64()))
        .collect();
    serials.sort_unstable();
    let (where_, note) = if serials.is_empty() {
        ("nor a history tail (this generation carries none)".to_string(), String::new())
    } else {
        let lo = serials[0];
        let hi = serials[serials.len() - 1];
        let note = if lo > 1 {
            format!(
                "; a generation older than serial {lo} has fallen off the {HISTORY_DEPTH}-deep \
                 tail, which is unaccountable rather than lost"
            )
        } else {
            String::new()
        };
        (format!("nor its history tail (serials {lo}-{hi})"), note)
    };
    // The reader-is-stale case, stated rather than left to be inferred. Same
    // grade (nothing in THIS tree explains the value) but the opposite first
    // action, so it must not render as the node's fault.
    if let (Some(obs), Some(cur)) = (observed_serial, serial.as_i64()) {
        if obs > cur {
            return serde_json::json!({
                "state": "unaccountable",
                "value": observed_content_hash,
                "detail": format!(
                    "node reports serial {obs}, NEWER than this artifact's current \
                     generation {cur} — THIS READER'S COPY OF THE INTENT ARTIFACT IS \
                     STALE, the node is ahead of it. Refresh the reader (re-run the \
                     consistency-health / consistency-console role so it carries the \
                     current ops/consistency/intent/<goatnet>/intent.json); do NOT \
                     re-converge the node against an older generation"
                ),
            });
        }
    }

    serde_json::json!({
        "state": "unaccountable",
        "value": observed_content_hash,
        "detail": format!(
            "matches neither serial {} {} — no generation in this artifact explains it{}",
            python_str(&serial), where_, note
        ),
    })
}

// ---------------------------------------------------------------------------
// Render accountability — the CF-4 computation
// ---------------------------------------------------------------------------

/// A hash abbreviated for a generated sentence: the digest half, first 12 hex
/// chars. Deterministic, because the sentence is pinned by the corpus.
fn short_hash(value: &str) -> String {
    value.replace("sha256:", "").chars().take(12).collect()
}

/// Classify ONE watched render — the CF-4 computation, from a node's own facts
/// alone. A deliberate mirror of the Python `account_for_render`; the corpus
/// (`testdata/cases/renders.yaml`) is what keeps the two identical.
///
/// A render's reference point is not a generation: it is what the LAST
/// CONVERGE on that node recorded after rendering the file
/// (`/var/lib/goat/<component>/render-manifest.json`, written after the
/// templates land — position, not reconciliation, makes it a post-render
/// fact). The intent artifact carries WHICH paths constitute a component's
/// render identity (`render_watch`); it cannot carry their hashes, because a
/// per-host render is a function of host vars and env lookups no repo-side
/// reader may evaluate.
///
/// Returns an IntentV1-shaped cell so the answer flows into the same
/// `grade_row` as every other row, instead of into a second comparison.
pub fn account_for_render(record: &Value) -> Value {
    let get = |k: &str| record.get(k).and_then(|v| v.as_str()).unwrap_or("");
    let recorded = get("recorded_sha256");
    let current = get("sha256");
    let note = get("note");
    let path = get("path");

    if recorded.is_empty() {
        let reason = if note.is_empty() { "no reason recorded" } else { note };
        return serde_json::json!({
            "state": "unreadable",
            "detail": format!(
                "no converge-time hash for {path} — the deploying role could not hash it when it rendered ({reason})"
            ),
        });
    }
    if current.is_empty() {
        if note == "missing" {
            return serde_json::json!({
                "state": "unaccountable",
                "detail": format!(
                    "{path} is gone — the last converge rendered it ({}) and no converge explains its absence",
                    short_hash(recorded)
                ),
            });
        }
        let reason = if note.is_empty() { "unreadable" } else { note };
        return serde_json::json!({
            "state": "unreadable",
            "detail": format!("{path} could not be read at collection time ({reason})"),
        });
    }
    if current == recorded {
        return serde_json::json!({"state": "resolved", "value": current});
    }
    serde_json::json!({
        "state": "unaccountable",
        "value": current,
        "detail": format!(
            "{path} hashes to {} but the last converge rendered {} — no converge explains the bytes on disk",
            short_hash(current),
            short_hash(recorded)
        ),
    })
}

/// Whether an IntentV1 row carries a declaration that covers this goatnet.
pub fn declared_scopes(row: &Value, members: &[String], goatnet: Option<&str>) -> bool {
    let Some(declared) = row.get("declared").and_then(|v| v.as_object()) else {
        return false;
    };
    declared
        .keys()
        .any(|k| members.iter().any(|m| m == k) || goatnet == Some(k.as_str()))
}

fn cells_of(value: Option<&Value>) -> BTreeMap<String, Value> {
    value
        .and_then(|v| v.as_object())
        .map(|o| o.iter().map(|(k, v)| (k.clone(), v.clone())).collect())
        .unwrap_or_default()
}

/// Grade a whole IntentV1 document — the entry point every consumer that holds
/// only the artifact uses. Components and capabilities are graded; `node_shape`
/// is NOT, because ADR 1084 makes a federated goatnet's nodes deliberately
/// unequal in role and comparing them would manufacture skew.
pub fn grade_intent(intent: &Value, vantage: &str, g: &Grammar) -> Result<Vec<GradedRow>, GrammarError> {
    let goatnet = intent.get("goatnet").and_then(|v| v.as_str());
    let members: Vec<String> = intent
        .get("members")
        .and_then(|v| v.as_array())
        .map(|a| a.iter().filter_map(|v| v.as_str().map(String::from)).collect())
        .unwrap_or_default();
    let mut rows = Vec::new();

    if let Some(comps) = intent.get("components").and_then(|v| v.as_array()) {
        for comp in comps {
            let cells = cells_of(comp.get("members"));
            let declared = cells.values().any(|c| c.get("declared").is_some());
            rows.push(grade_row(
                "component",
                comp.get("name").and_then(|v| v.as_str()).unwrap_or(""),
                cells,
                goatnet,
                vantage,
                declared,
                comp.get("artifact_type").and_then(|v| v.as_str()),
                g,
            )?);
        }
    }

    if let Some(caps) = intent.get("capabilities").and_then(|v| v.as_array()) {
        for cap in caps {
            rows.push(grade_row(
                "capability",
                cap.get("var").and_then(|v| v.as_str()).unwrap_or(""),
                cells_of(cap.get("members")),
                goatnet,
                vantage,
                declared_scopes(cap, &members, goatnet),
                None,
                g,
            )?);
        }
    }

    Ok(rows)
}
