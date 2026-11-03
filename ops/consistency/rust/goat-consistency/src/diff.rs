// SPDX-License-Identifier: Apache-2.0
//
// diff.rs — the cross-goatnet intent diff (consistency-foundation.md §7.8,
// the aggregator-product change).
//
// NOT A SECOND COMPARISON. The §8 non-goal ("a surface that needs a verdict
// the library does not emit files a corpus case, not a local function") is
// why this lives HERE and not in a renderer: the cross-env verdict is the
// existing `grade_row` applied to a wider column set — one column per
// (goatnet, member-group) pair, cells copied VERBATIM from each goatnet's
// committed, verified IntentV1. No new grade, no new sentence generator, no
// new remediation kind: the six grammar grades and the repo-vantage dispatch
// cover the cross-env question because fleet-pins.yaml is already ONE table
// across goatnets — a cross-env difference is declared (a `deviations:`
// stanza) or edited there, exactly like a within-goatnet one.
//
// What the merge itself decides — the only semantics this file owns, stated
// so they can be reviewed:
//
//   1. Row identity is (subject, name): components join by `name`,
//      capabilities by `var` — the same keys grade_intent uses.
//   2. Column identity is `<env>:<member>`, so the generated why sentence
//      names which goatnet's which member carries each value.
//   3. A subject PRESENT in one artifact and MISSING from another gets a
//      synthetic `unset` cell for the missing env ("a key with no entry and
//      no default" — the grammar's own definition of indeterminate). The
//      absence of an entry is a fact the artifact carries; the cell states
//      it, it does not invent a value.
//   4. A declaration covers the COLUMNS IT NAMES, and the row is `declared`
//      only when the columns nothing declares still AGREE. This is the rule
//      that keeps a declaration honest across envs, and it replaces a weaker
//      one.
//
//      The weaker rule was `declared_anywhere && present_everywhere`: any
//      marker on any member of any env promoted the whole row. That is safe
//      inside ONE goatnet, where a deviation is scoped to the members being
//      compared — but across goatnets it lets a declaration about env A's own
//      standby excuse a difference between env A and env B that nobody
//      declared. A goatnet's single-homed surfaces each carry an `absent`
//      deviation for their standby (ADR 1084 §D3), so under that rule a portal
//      image genuinely diverged between two goatnets would render `declared` on
//      the strength of a sentence about a standby.
//
//      Under the new rule the remainder decides: strip the declared columns,
//      grade what is left, and promote only if that agrees (or if there is
//      nothing left, i.e. every column is declared). The present-vs-missing
//      case the old rule handled falls out for free — a synthetic `unset`
//      cell is never covered by a declaration, so it stays in the remainder
//      and grades `indeterminate`, which is not agreement.
//
//   4a. A declaration carrying an `until:` date STOPS COVERING its column
//      once that date has passed, and the row falls back to the verdict its
//      values earn. `until:` has been in the deviation schema since the
//      parity report and was rendered but never compared to a clock, which
//      made `declared` a terminal state and the declared bucket monotonic:
//      the one thing a grading surface must not become is a place where
//      differences go to be excused. Expiry is evaluated only when the caller
//      supplies today's date — a pure library does not read a clock, and a
//      caller that cannot date the comparison gets the un-expired answer
//      rather than a guessed one.
//
//      BOUNDARY, stated rather than left to be discovered: expiry lives HERE,
//      in the cross-env diff, and not in `grade_cells`. Extending it to the
//      within-goatnet grid means changing the grade_cells contract in BOTH
//      grammar executions plus the shared corpus (ADR 1110 D2), which is a
//      bigger change than the one this rule needs — and no within-goatnet
//      deviation carries an `until:` today. When one does, that is the change
//      to make, and it belongs in the corpus, not in a second local rule.
//   5. `node_shape` is not compared, for the same ADR 1084 reason
//      grade_intent does not grade it; `retired` and `ungoverned` are
//      per-goatnet assertions and are not compared either. Named here so the
//      exclusions are decisions, not omissions.
//
// The result is computed at read time from the verified artifacts, stored
// nowhere, and read by nothing but a page and an API — the aggregator stays
// read-only and never load-bearing.

use std::collections::BTreeMap;

use serde_json::Value;

use crate::grading::{grade_cells, grade_row, GradedRow};
use crate::grammar::{Grammar, GrammarError};

/// One goatnet's verified committed artifact, under the display name the
/// aggregator knows it by. Callers pass ONLY intents that passed
/// verify-at-read — an unverified artifact is refused upstream and its env is
/// reported as not compared, never silently included.
pub struct EnvIntent<'a> {
    pub env: &'a str,
    pub intent: &'a Value,
}

/// The cross-env report: the graded rows plus their honest denominators.
#[derive(Debug)]
pub struct CrossEnvReport {
    /// The envs actually compared, in caller order.
    pub envs: Vec<String>,
    /// One row per union subject, graded by `grade_row` (vantage `repo`).
    pub rows: Vec<GradedRow>,
    /// Row count per grade name — counts with a denominator (`rows.len()`),
    /// never a percentage.
    pub by_grade: BTreeMap<String, usize>,
}

impl CrossEnvReport {
    /// The drift signal: rows whose difference nothing declares. `SKEW` is
    /// the grammar's name for exactly that; counted by colour-of-record
    /// rather than re-deriving semantics here.
    pub fn undeclared(&self) -> usize {
        self.by_grade.get("SKEW").copied().unwrap_or(0)
    }
}

fn member_cells(row: &Value) -> BTreeMap<String, Value> {
    row.get("members")
        .and_then(|v| v.as_object())
        .map(|o| o.iter().map(|(k, v)| (k.clone(), v.clone())).collect())
        .unwrap_or_default()
}

/// Find `section` rows keyed by `key_field`, in artifact order.
fn section_rows<'a>(intent: &'a Value, section: &str, key_field: &str) -> Vec<(&'a str, &'a Value)> {
    intent
        .get(section)
        .and_then(|v| v.as_array())
        .map(|a| {
            a.iter()
                .filter_map(|row| {
                    row.get(key_field).and_then(|n| n.as_str()).map(|n| (n, row))
                })
                .collect()
        })
        .unwrap_or_default()
}

/// The union of subject names across envs, first-seen order — deterministic,
/// so the render is a pure function of the artifacts.
fn union_names<'a>(per_env: &[Vec<(&'a str, &'a Value)>]) -> Vec<&'a str> {
    let mut names: Vec<&str> = Vec::new();
    for rows in per_env {
        for (name, _) in rows {
            if !names.contains(name) {
                names.push(name);
            }
        }
    }
    names
}

/// The `until:` date a declaration carries, if it carries one. Both shapes are
/// read: the per-cell `declared` block a component member gets, and the
/// row-level `declared` map a capability gets, keyed by the scope that covers
/// this column.
fn until_of(declared: &Value) -> Option<&str> {
    declared.get("until").and_then(|v| v.as_str())
}

/// Whether a declaration has expired as of `today` (both `YYYY-MM-DD`, so a
/// lexical compare IS a date compare). No `until:` never expires. A malformed
/// date does NOT silently expire — an unparseable value is reported by the
/// schema, and guessing here would flip a row on a typo.
fn expired(until: &str, today: Option<&str>) -> bool {
    let Some(today) = today else { return false };
    until.len() == 10 && today.len() == 10 && until < today
}

/// Whether a declaration covers one column, and what it said. A column is
/// `<env>:<member>`; it is covered by the cell's own `declared` block, or by a
/// row-level `declared` entry keyed by that member or by the env's goatnet —
/// the same scopes `declared_scopes` accepts, evaluated per column instead of
/// per row so a declaration cannot reach a column it never named.
fn column_declaration<'a>(
    row: &'a Value,
    cell: &'a Value,
    member: &str,
    goatnet: Option<&str>,
) -> Option<&'a Value> {
    if let Some(d) = cell.get("declared") {
        return Some(d);
    }
    let map = row.get("declared").and_then(|v| v.as_object())?;
    map.iter()
        .find(|(k, _)| k.as_str() == member || Some(k.as_str()) == goatnet)
        .map(|(_, v)| v)
}

#[allow(clippy::type_complexity)]
fn diff_section(
    envs: &[EnvIntent],
    section: &str,
    key_field: &str,
    subject: &str,
    g: &Grammar,
    today: Option<&str>,
    rows_out: &mut Vec<GradedRow>,
) -> Result<(), GrammarError> {
    let per_env: Vec<Vec<(&str, &Value)>> =
        envs.iter().map(|e| section_rows(e.intent, section, key_field)).collect();
    for name in union_names(&per_env) {
        let mut cells: BTreeMap<String, Value> = BTreeMap::new();
        // The columns nothing (still-valid) declares. Rule 4: these decide.
        let mut undeclared: BTreeMap<String, Value> = BTreeMap::new();
        let mut artifact_type: Option<&str> = None;
        let mut expiries: Vec<String> = Vec::new();
        for (env, env_rows) in envs.iter().zip(&per_env) {
            match env_rows.iter().find(|(n, _)| *n == name) {
                Some((_, row)) => {
                    let goatnet = env.intent.get("goatnet").and_then(|v| v.as_str());
                    for (member, cell) in member_cells(row) {
                        // Column identity: which goatnet's which member.
                        let col = format!("{}:{member}", env.env);
                        let covered = match column_declaration(row, &cell, &member, goatnet) {
                            None => false,
                            Some(d) => match until_of(d) {
                                Some(u) if expired(u, today) => {
                                    // Rule 4a: past its date, it covers nothing,
                                    // and the row says so rather than quietly
                                    // dropping back to amber.
                                    expiries.push(format!("{col} (declared until {u})"));
                                    false
                                }
                                _ => true,
                            },
                        };
                        if !covered {
                            undeclared.insert(col.clone(), cell.clone());
                        }
                        cells.insert(col, cell);
                    }
                    if artifact_type.is_none() {
                        artifact_type = row.get("artifact_type").and_then(|v| v.as_str());
                    }
                }
                None => {
                    // A synthetic absence is never covered: an artifact that
                    // omits a subject carries no declaration about it.
                    let cell = serde_json::json!({
                        "state": "unset",
                        "detail": format!(
                            "not governed in {}'s intent artifact — no entry to compare",
                            env.env
                        ),
                    });
                    undeclared.insert(env.env.to_string(), cell.clone());
                    cells.insert(env.env.to_string(), cell);
                }
            }
        }
        // Rule 4: promote only when the undeclared remainder agrees. An empty
        // remainder means every column is declared, which is agreement by
        // exhaustion; anything the remainder cannot settle (`unreadable`,
        // `indeterminate`) is not agreement, so a reason never rescues an
        // ungrounded row — the same limit grade_cells already enforces.
        let any_declared = undeclared.len() < cells.len();
        let declared = any_declared
            && (undeclared.is_empty()
                || grade_cells(undeclared.values(), false, g)? == "match");
        let mut graded =
            grade_row(subject, name, cells, None, "repo", declared, artifact_type, g)?;
        if !expiries.is_empty() {
            // Generated, not composed by a renderer — every surface says the
            // same sentence, the §4.4 rule for `why`.
            graded.why.push_str(&format!(
                " — a declaration has EXPIRED and no longer covers {}",
                expiries.join(", ")
            ));
        }
        rows_out.push(graded);
    }
    Ok(())
}

/// Grade the cross-env intent diff over two or more verified artifacts.
///
/// `today` is `YYYY-MM-DD` in UTC, or `None`. It exists for rule 4a and
/// nothing else: the library does not read a clock (that would make the same
/// artifacts grade differently in two processes for a reason neither could
/// show), so the caller that HAS a clock dates the comparison. `None` means
/// "undated" and no declaration expires — an honest under-report, never a
/// guess.
///
/// Refuses (loudly, not by rendering less) on fewer than two envs — a
/// comparison of one thing is not a comparison, and the caller owns saying
/// so on its surface.
pub fn diff_intents(
    envs: &[EnvIntent],
    g: &Grammar,
    today: Option<&str>,
) -> Result<CrossEnvReport, GrammarError> {
    if envs.len() < 2 {
        return Err(GrammarError(format!(
            "cross-env diff needs at least two verified intents, got {}",
            envs.len()
        )));
    }
    let mut rows = Vec::new();
    diff_section(envs, "components", "name", "component", g, today, &mut rows)?;
    diff_section(envs, "capabilities", "var", "capability", g, today, &mut rows)?;
    let mut by_grade: BTreeMap<String, usize> = BTreeMap::new();
    for row in &rows {
        *by_grade.entry(row.grade.clone()).or_insert(0) += 1;
    }
    Ok(CrossEnvReport {
        envs: envs.iter().map(|e| e.env.to_string()).collect(),
        rows,
        by_grade,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn grammar() -> Grammar {
        Grammar::load().unwrap()
    }

    /// Two minimal artifacts exercising every verdict class the drift view
    /// renders: identical, differing-declared, differing-undeclared, and
    /// present-in-one-artifact-only.
    fn artifact_a() -> Value {
        serde_json::json!({
            "goatnet": "alpha",
            "members": ["alpha_hosts"],
            "components": [
                {"name": "same-image", "artifact_type": "image", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/same@sha256:aaaa"}}},
                {"name": "drifted-image", "artifact_type": "image", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/thing@sha256:new1"}}},
                {"name": "declared-split", "artifact_type": "image", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/split@sha256:mine",
                                     "declared": {"reason": "alpha runs the fork on purpose", "scope": "alpha_hosts"}}}},
                {"name": "only-in-alpha", "artifact_type": "image", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/only@sha256:solo"}}}
            ],
            "capabilities": [
                {"var": "cap_same", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "true"}}},
                {"var": "cap_posture_differs", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "true"}}}
            ]
        })
    }

    fn artifact_b() -> Value {
        serde_json::json!({
            "goatnet": "bravo",
            "members": ["bravo_hosts"],
            "components": [
                {"name": "same-image", "artifact_type": "image", "members": {
                    "bravo_hosts": {"state": "resolved", "value": "ghcr.io/x/same@sha256:aaaa"}}},
                {"name": "drifted-image", "artifact_type": "image", "members": {
                    "bravo_hosts": {"state": "resolved", "value": "ghcr.io/x/thing@sha256:old9"}}},
                {"name": "declared-split", "artifact_type": "image", "members": {
                    "bravo_hosts": {"state": "resolved", "value": "ghcr.io/x/split@sha256:theirs"}}}
            ],
            "capabilities": [
                {"var": "cap_same", "members": {
                    "bravo_hosts": {"state": "resolved", "value": "true"}}},
                {"var": "cap_posture_differs", "members": {
                    "bravo_hosts": {"state": "resolved", "value": "false"}}}
            ]
        })
    }

    fn report() -> CrossEnvReport {
        let (a, b) = (artifact_a(), artifact_b());
        diff_intents(
            &[EnvIntent { env: "alpha", intent: &a }, EnvIntent { env: "bravo", intent: &b }],
            &grammar(),
            None,
        )
        .unwrap()
    }

    fn report_on(a: &Value, b: &Value, today: Option<&str>) -> CrossEnvReport {
        diff_intents(
            &[EnvIntent { env: "alpha", intent: a }, EnvIntent { env: "bravo", intent: b }],
            &grammar(),
            today,
        )
        .unwrap()
    }

    fn row<'a>(r: &'a CrossEnvReport, name: &str) -> &'a GradedRow {
        r.rows.iter().find(|x| x.name == name).unwrap()
    }

    #[test]
    fn identical_values_across_envs_grade_match() {
        let r = report();
        assert_eq!(row(&r, "same-image").grade, "match");
        assert_eq!(row(&r, "cap_same").grade, "match");
        // The why sentence names both envs' columns.
        assert!(row(&r, "same-image").why.contains("alpha:alpha_hosts="));
        assert!(row(&r, "same-image").why.contains("bravo:bravo_hosts="));
    }

    #[test]
    fn an_undeclared_cross_env_difference_grades_skew_the_drift_signal() {
        let r = report();
        let drifted = row(&r, "drifted-image");
        assert_eq!(drifted.grade, "SKEW");
        assert!(drifted.why.contains("they differ and nothing declares why"));
        // Remediation arrives from the grammar (repo dispatch), verbatim data.
        assert_eq!(drifted.remediation.as_ref().unwrap().kind, "declare");
        // Capability posture drift is the same signal.
        assert_eq!(row(&r, "cap_posture_differs").grade, "SKEW");
        assert_eq!(r.undeclared(), 2);
    }

    #[test]
    fn a_declared_difference_grades_declared_not_drift() {
        let r = report();
        let d = row(&r, "declared-split");
        assert_eq!(d.grade, "declared");
        assert!(d.why.contains("the difference is declared with a reason"));
    }

    #[test]
    fn present_in_one_artifact_only_grades_indeterminate_with_the_absence_stated() {
        let r = report();
        let solo = row(&r, "only-in-alpha");
        assert_eq!(solo.grade, "indeterminate");
        assert!(
            solo.why.contains("not governed in bravo's intent artifact"),
            "the missing env's column states the absence: {}",
            solo.why
        );
        // The synthetic cell is a column, not a blank.
        assert!(solo.columns.contains(&"bravo".to_string()));
    }

    #[test]
    fn a_declared_marker_cannot_launder_a_missing_subject() {
        // `only-in-alpha` with a declared marker on alpha's own member must
        // still grade indeterminate: bravo's artifact carries no declaration
        // about a subject it does not carry.
        let mut a = artifact_a();
        a["components"][3]["members"]["alpha_hosts"]["declared"] =
            serde_json::json!({"reason": "within-alpha reason", "scope": "alpha_hosts"});
        let r = report_on(&a, &artifact_b(), None);
        assert_eq!(row(&r, "only-in-alpha").grade, "indeterminate");
    }

    /// Rule 4, the case the old `declared_anywhere` rule got wrong. Alpha
    /// gains a second member that is declared ABSENT — the common shape,
    /// where six single-homed surfaces carry an `absent` deviation for the
    /// dormant standby. That sentence must not reach the alpha-vs-bravo
    /// difference, which nothing declares.
    #[test]
    fn a_declaration_about_one_member_cannot_excuse_a_difference_between_the_others() {
        let mut a = artifact_a();
        a["members"] = serde_json::json!(["alpha_hosts", "alpha_standby"]);
        a["components"][1]["members"]["alpha_standby"] = serde_json::json!({
            "state": "absent",
            "declared": {"reason": "single-homed on alpha_hosts", "scope": "alpha_standby"},
        });
        let r = report_on(&a, &artifact_b(), None);
        let drifted = row(&r, "drifted-image");
        assert_eq!(
            drifted.grade, "SKEW",
            "the standby's absence declaration says nothing about alpha-vs-bravo: {}",
            drifted.why
        );
        // And the same shape where the remainder DOES agree is still declared:
        // every column but one is declared, one column cannot disagree with
        // itself, so the row is covered.
        let mut a2 = artifact_a();
        a2["members"] = serde_json::json!(["alpha_hosts", "alpha_standby"]);
        a2["components"][2]["members"]["alpha_standby"] = serde_json::json!({
            "state": "absent",
            "declared": {"reason": "single-homed on alpha_hosts", "scope": "alpha_standby"},
        });
        assert_eq!(row(&report_on(&a2, &artifact_b(), None), "declared-split").grade, "declared");
    }

    /// Rule 4a. The same artifacts grade `declared` before the date and SKEW
    /// after it, and the row states the expiry rather than silently going
    /// amber.
    #[test]
    fn a_declaration_expires_on_its_until_date_and_the_row_says_so() {
        let mut a = artifact_a();
        a["components"][2]["members"]["alpha_hosts"]["declared"] = serde_json::json!({
            "reason": "alpha runs the fork on purpose",
            "scope": "alpha_hosts",
            "until": "2026-09-30",
        });
        let b = artifact_b();

        let before = report_on(&a, &b, Some("2026-08-12"));
        assert_eq!(row(&before, "declared-split").grade, "declared");
        assert!(!row(&before, "declared-split").why.contains("EXPIRED"));

        let after = report_on(&a, &b, Some("2026-10-01"));
        let expired = row(&after, "declared-split");
        assert_eq!(expired.grade, "SKEW", "past its date it covers nothing");
        assert!(
            expired.why.contains("EXPIRED")
                && expired.why.contains("alpha:alpha_hosts")
                && expired.why.contains("2026-09-30"),
            "the row names the column and the date it lapsed: {}",
            expired.why
        );
        // The remediation is the grammar's, unchanged — an expired row is an
        // ordinary undeclared difference, not a new kind of thing.
        assert_eq!(after.undeclared(), 3);

        // Undated callers expire nothing: an honest under-report beats a guess.
        assert_eq!(row(&report_on(&a, &b, None), "declared-split").grade, "declared");
        // And ON the date is not PAST it.
        assert_eq!(row(&report_on(&a, &b, Some("2026-09-30")), "declared-split").grade, "declared");
    }

    #[test]
    fn counts_carry_their_denominator_and_fewer_than_two_envs_refuse() {
        let r = report();
        let total: usize = r.by_grade.values().sum();
        assert_eq!(total, r.rows.len(), "every row is counted exactly once");
        assert_eq!(r.rows.len(), 6, "4 component + 2 capability union subjects");

        let a = artifact_a();
        assert!(diff_intents(&[EnvIntent { env: "alpha", intent: &a }], &grammar(), None).is_err());
    }

    /// The COMMITTED artifacts diff cleanly — structural pins only (row set =
    /// union, counts sum), never a pinned verdict: the verdicts are the
    /// artifacts' business and change with every legitimate pin bump.
    #[test]
    fn the_committed_artifacts_are_comparable() {
        let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../intent");
        let load = |g: &str| -> Value {
            serde_json::from_str(
                &std::fs::read_to_string(dir.join(g).join("intent.json")).unwrap(),
            )
            .unwrap()
        };
        let (zagros, prod) = (load("zagros"), load("zagros_prod"));
        let r = diff_intents(
            &[EnvIntent { env: "zagros", intent: &zagros }, EnvIntent { env: "zagros-prod", intent: &prod }],
            &grammar(),
            None,
        )
        .unwrap();
        assert!(!r.rows.is_empty());
        assert_eq!(r.by_grade.values().sum::<usize>(), r.rows.len());
        // Every row's grade is a grammar grade with a colour — nothing local.
        let g = grammar();
        for row in &r.rows {
            g.color(&row.grade, false).expect("every diff grade is a grammar grade");
            assert!(!row.why.is_empty(), "every grade carries its why");
        }
    }
}
