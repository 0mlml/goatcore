// SPDX-License-Identifier: Apache-2.0
//
// project.rs — one node's convergence record × one intent generation.
//
// This is the LIVE-vantage half of the comparison, and it lives in the library
// for the same reason every other verdict does: a projection may choose which
// records it can see and how it renders, but it may not grade (§4.1). The
// rollup meter and the CF-6 console both call this; neither computes anything
// of its own.
//
// It takes NEUTRAL structs rather than protobuf, so the grading library stays
// free of prost and the corpus can drive it directly. The binary converts
// ConsistencyReportV1 <-> these on the way in.
//
// TWO THINGS THIS DELIBERATELY DOES NOT DO, each because the design forbids it:
//
//   1. It does not grade a component the artifact does not govern. IntentV1
//      covers governed pins and named exceptions (§3.7); a node's ledger also
//      carries dozens of components nothing claims authority over, and issuing
//      verdicts about those would be the mega-unification non-goal wearing a
//      grade. They ride in ConsistencyReportV1 as FACTS, visible in the drill.
//
//   2. It does not invent a verdict for a governed component the node cannot
//      report. Some governed components are deployed from a
//      playbook rather than a role, so they have no rollback-ledger coverage
//      and therefore no on-host tip to read. The projection reports that as a
//      COUNT WITH A DENOMINATOR — `components_observed` of `components_governed`
//      — because §7.4 is explicit: "The grid shows counts with denominators or
//      it shows nothing." A fabricated `SKEW` there would point an operator at
//      a refresh play that cannot fix it, and a fabricated `match` would be the
//      false green this whole surface exists to prevent.
//
// Canon: docs/design/consistency-foundation.md §5.2/§5.3, §4.4.

use std::collections::BTreeMap;

use serde_json::Value;

use crate::grading::{account_for, account_for_render, grade_row, GradedRow};
use crate::grammar::{Grammar, GrammarError, Remediation};

/// One component tip as the node reports it (a ConsistencyReportV1 row).
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ObservedComponent {
    pub component: String,
    pub artifact_type: String,
    pub reference: String,
    pub reconciled: String,
    pub live_ref: String,
}

/// One watched render as the node reports it (a RenderStateV1 row, CF-4):
/// the file's hash at collection time and the hash the last converge recorded
/// after rendering it. FACTS — the comparison happens here, at read time.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ObservedRender {
    pub component: String,
    pub path: String,
    pub sha256: String,
    pub recorded_sha256: String,
    pub note: String,
}

/// One node's self-published record, as the rollup received it.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ObservedNode {
    pub host_id: String,
    pub member_group: String,
    pub intent_content_hash: String,
    pub intent_serial: u64,
    pub intent_verified: bool,
    /// How old the record was when the rollup read it. Age is grounding, not a
    /// grade: it is reported always and drives `idle` only past the staleness
    /// window the caller applies before calling in.
    pub report_age_ms: i64,
    pub components: Vec<ObservedComponent>,
    pub renders: Vec<ObservedRender>,
}

/// One graded row of a node's projection, flattened for the wire.
#[derive(Debug, Clone, PartialEq)]
pub struct ProjectedRow {
    pub subject: String,
    pub name: String,
    pub artifact_type: String,
    pub grade: String,
    pub color: String,
    pub why: String,
    pub observed: String,
    pub expected: String,
    pub remediation: Option<Remediation>,
    /// ADR 1125: whether a comparison was POSSIBLE, orthogonal to the grade.
    /// `observed` on every row that carries a real comparison; the other three
    /// states are rows that used to be nothing at all — a `continue` in this
    /// file and a smaller numerator in `components_observed`.
    pub coverage: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct NodeProjection {
    pub host_id: String,
    pub member_group: String,
    pub color: String,
    pub grade: String,
    pub intent_serial: u64,
    pub intent_content_hash: String,
    pub behind_by: u32,
    pub intent_age_seconds: i64,
    pub report_age_ms: i64,
    pub intent_verified: bool,
    pub components_governed: usize,
    pub components_observed: usize,
    /// CF-4 grounding: how many watched paths this member's intent declares,
    /// and how many the node actually recorded. Counts with denominators, or
    /// nothing (§7.4) — a declared-but-unrecorded watch-set must be a number a
    /// renderer can show, not a silently shorter row list.
    pub renders_declared: usize,
    pub renders_observed: usize,
    /// ADR 1125: how many of this node's rows are coverage gaps rather than
    /// comparisons. Deliberately NOT folded into `grade` — see the exclusion
    /// at the collapse site for why.
    pub coverage_gaps: usize,
    pub rows: Vec<ProjectedRow>,
}

/// The digest half of an OCI reference — everything after `@`. This, not the
/// whole string, is what a reader can compare: IntentV1 carries
/// `ghcr.io/{{ ghcr_org }}/…@sha256:…` with the org left LITERAL on purpose
/// (§3.1), because the same committed string must resolve to a different org
/// depending on which checkout deploys. The digest is org-independent and is
/// the actual identity of the artifact.
fn digest_of(reference: &str) -> Option<&str> {
    reference.split_once('@').map(|(_, d)| d).filter(|d| !d.is_empty())
}

/// The IntentV1 cell for one component in one member column, reduced to
/// something a live comparison can use.
fn expected_cell(cell: Option<&Value>) -> Value {
    let Some(cell) = cell else {
        return serde_json::json!({"state": "unset", "detail": "this member column has no entry"});
    };
    match cell.get("state").and_then(|v| v.as_str()).unwrap_or("") {
        "absent" => serde_json::json!({"state": "absent"}),
        "resolved" => {
            let value = cell.get("value").and_then(|v| v.as_str()).unwrap_or("");
            match digest_of(value) {
                Some(d) => serde_json::json!({"state": "resolved", "value": d}),
                // A mutable tag is READ and cannot be EVALUATED against a
                // running ref — `unreadable`, which never reaches match. This is
                // the pin table's own rule surfacing as an observation rather
                // than as silence.
                None => serde_json::json!({
                    "state": "unreadable",
                    "detail": format!("intent pins `{value}` — a mutable tag carries no digest to compare a running ref against"),
                }),
            }
        }
        other => cell.get("detail").map_or_else(
            || serde_json::json!({"state": other}),
            |d| serde_json::json!({"state": other, "detail": d}),
        ),
    }
}

/// The node's side of the same comparison.
fn observed_cell(observed: &ObservedComponent) -> Value {
    // §5.2's inherited ordering caveat: the ledger row is written at role
    // ENTRY, so an unreconciled tip records what the run INTENDED, not what
    // succeeded. Reporting it as a converged fact would manufacture exactly the
    // false green this surface exists to prevent.
    match observed.reconciled.as_str() {
        "confirmed" => {}
        "diverged" => {
            return serde_json::json!({
                "state": "unreadable",
                "detail": format!(
                    "ledger tip and the running container disagree (running `{}`) — the ledger row is written at role entry, so this is a failed or half-applied deploy, not a converged fact",
                    observed.live_ref
                ),
            })
        }
        _ => {
            return serde_json::json!({
                "state": "unreadable",
                "detail": "ledger tip not reconciled against the running container — recorded intent, not an observed fact",
            })
        }
    }
    match digest_of(&observed.reference) {
        Some(d) => serde_json::json!({"state": "resolved", "value": d}),
        None => serde_json::json!({
            "state": "unreadable",
            "detail": format!("ledger tip `{}` carries no digest", observed.reference),
        }),
    }
}

fn flatten(row: &GradedRow, artifact_type: &str, observed: &str, expected: &str, g: &Grammar)
    -> Result<ProjectedRow, GrammarError>
{
    Ok(ProjectedRow {
        subject: row.subject.clone(),
        name: row.name.clone(),
        artifact_type: artifact_type.to_string(),
        color: g.color(&row.grade, false)?,
        grade: row.grade.clone(),
        why: row.why.clone(),
        observed: observed.to_string(),
        expected: expected.to_string(),
        remediation: row.remediation.clone(),
        coverage: COVERAGE_OBSERVED.to_string(),
    })
}

/// The four coverage states, named once. They are DECLARED in
/// ops/consistency/grammar.yaml — these constants are the join to it, and
/// `Grammar::coverage_remediation` refuses an unknown one, so a typo here
/// fails loud rather than producing a row with no pointer.
pub const COVERAGE_OBSERVED: &str = "observed";
pub const COVERAGE_NO_INSTRUMENT: &str = "no-instrument";
pub const COVERAGE_SILENT: &str = "silent";
pub const COVERAGE_UNREPORTED_SCOPE: &str = "unreported-scope";

/// A row for a governed fact NOTHING OBSERVED — the ADR 1125 change.
///
/// It grades `indeterminate` because that is what the grammar already calls
/// "nothing to read", and it must never be `match`. What is new is that it
/// EXISTS: `ledger_component: null` and "the node did not report
/// it" are both more than a `continue` or a smaller numerator in
/// `components_observed`. A number is something a reader skims; a row with a
/// pointer is something a reader fixes.
///
/// The pointer comes from the coverage state, NOT from the grade's live
/// dispatch, and the difference is the whole point: `indeterminate`/live
/// dispatches to `reconverge`, which is the wrong instruction for a component
/// no ledger covers — re-running the play cannot create coverage, and telling
/// an operator to re-run is how a permanent gap gets read as a transient.
struct Gap<'a> {
    subject: &'a str,
    name: &'a str,
    member_group: &'a str,
    host_id: &'a str,
    artifact_type: &'a str,
    coverage: &'a str,
    detail: String,
    goatnet: Option<&'a str>,
}

fn coverage_row(gap: Gap<'_>, g: &Grammar) -> Result<ProjectedRow, GrammarError> {
    let mut cells = BTreeMap::new();
    cells.insert(
        gap.host_id.to_string(),
        serde_json::json!({"state": "unreported", "detail": gap.detail}),
    );
    // `declared: false`: the grade's own pointer is discarded below in favour
    // of the coverage one.
    let row = grade_row(
        gap.subject, gap.name, cells, gap.goatnet, "live", false, Some(gap.artifact_type), g,
    )?;
    let mut flat = flatten(&row, gap.artifact_type, "", "", g)?;
    flat.coverage = gap.coverage.to_string();
    flat.remediation = match g.coverage_remediation(gap.coverage)? {
        Some(kind) => Some(g.pointer(
            kind,
            &[
                ("name", gap.name),
                ("member", gap.member_group),
                ("env", gap.goatnet.unwrap_or("")),
            ],
        )?),
        None => flat.remediation,
    };
    Ok(flat)
}

/// The declared join key between an IntentV1 component and the node's ledger
/// directory `/var/lib/goat/<component>/`.
///
/// DECLARED, never inferred. `portal_image` is deployed by the role
/// `goat-zenoh-portal`; a rule that guessed role names from the var name would
/// be right often enough to be trusted and wrong often enough to lie, which is
/// the worst of both.
///
/// Three states, and the third is the point:
///   - `ledger_component` declared     -> that key
///   - `ledger_component` absent       -> default to the component name
///   - `ledger_component: null`        -> declared NOT ledger-covered. The
///     component is still GOVERNED and still counted in the denominator; it
///     simply can never be observed through this surface, because it is
///     deployed from a playbook rather than from a role carrying the
///     rollback-ledger meta-dependency (ADR 1060's coverage gap). Counting it
///     is what makes that gap visible instead of invisible.
fn ledger_key(component: &Value) -> Option<&str> {
    match component.get("ledger_component") {
        Some(Value::Null) => None,
        Some(Value::String(s)) if !s.is_empty() => Some(s),
        _ => component.get("name").and_then(|v| v.as_str()),
    }
}

/// Project ONE node against ONE generation. `now` is unix seconds.
pub fn project_node(
    intent: &Value,
    node: &ObservedNode,
    now: i64,
    g: &Grammar,
) -> Result<NodeProjection, GrammarError> {
    let goatnet = intent.get("goatnet").and_then(|v| v.as_str());
    let mut rows: Vec<ProjectedRow> = Vec::new();

    // -- the generation row: the accountability unit (§3.3a) -----------------
    // This is the row that separates "behind" from "unexplained", and it is the
    // only row that can: the history tail carries identity triples, so a reader
    // holding one artifact can place a whole generation but not an individual
    // component ref.
    let gen_cell = if node.intent_content_hash.is_empty() {
        account_for(intent, "", now, None)
    } else if !node.intent_verified {
        // P14 fail-closed. An intent whose integrity check did not pass is
        // refused, never rendered as fact — `unreadable`, which can never reach
        // match however well the values agree.
        serde_json::json!({
            "state": "unreadable",
            "detail": "the applied-intent stamp reports that the artifact this node converged against FAILED its integrity check at apply time",
        })
    } else {
        // Pass the node's own serial so a node AHEAD of this reader is named as
        // a stale reader rather than an unexplainable node (opposite fixes).
        account_for(intent, &node.intent_content_hash, now, Some(node.intent_serial as i64))
    };
    let behind_by = gen_cell
        .get("generation")
        .and_then(|v| v.get("behind_by"))
        .and_then(|v| v.as_u64())
        .unwrap_or(0) as u32;
    let intent_age_seconds = gen_cell
        .get("generation")
        .and_then(|v| v.get("intent_age_seconds"))
        .and_then(|v| v.as_i64())
        .unwrap_or(-1);

    let mut gen_cells = BTreeMap::new();
    gen_cells.insert(node.host_id.clone(), gen_cell.clone());
    // A generation's artifact type is `forward-only`, and that is a decision
    // §3.3a already made rather than one taken here: "It does not make any
    // generation other than the newest TARGETABLE… The tail is evidence for
    // grading, never a target for converging." So when a node reports a
    // generation no artifact explains, the honest pointer is the closed set's
    // `refuse` — converge forward to the current generation and KEEP the
    // finding — and never `rollback`, which would tell an operator to put the
    // node back onto a generation nobody can name.
    //
    // This is also where the library's fail-closed rule earned its keep: passing
    // no artifact type here made `grade_row` REFUSE to emit an unaccountable row
    // rather than emit one with no pointer, which is exactly the "a grade that
    // needs a pointer and has none must not ship" shape §6.3 specifies.
    let gen_row = grade_row(
        "generation", "intent", gen_cells, goatnet, "live", false, Some("forward-only"), g,
    )?;
    rows.push(flatten(
        &gen_row,
        "forward-only",
        &node.intent_content_hash,
        intent.get("content_hash").and_then(|v| v.as_str()).unwrap_or(""),
        g,
    )?);

    // -- component rows: governed AND observed -------------------------------
    let by_ledger: BTreeMap<&str, &ObservedComponent> =
        node.components.iter().map(|c| (c.component.as_str(), c)).collect();
    let empty = Vec::new();
    let governed = intent.get("components").and_then(|v| v.as_array()).unwrap_or(&empty);
    let mut components_governed = 0usize;
    let mut components_observed = 0usize;

    for component in governed {
        let expected = expected_cell(
            component.get("members").and_then(|m| m.get(&node.member_group)),
        );
        // A component the goatnet declares this member does not run is not a
        // row: `absent` is a declared answer, not a gap.
        if expected.get("state").and_then(|v| v.as_str()) == Some("absent") {
            continue;
        }
        components_governed += 1;
        let comp_name = component.get("name").and_then(|v| v.as_str()).unwrap_or("");
        let declared_type = component
            .get("artifact_type")
            .and_then(|v| v.as_str())
            .unwrap_or("image")
            .to_string();
        let Some(key) = ledger_key(component) else {
            // `ledger_component: null` — declared NOT ledger-covered. Governed,
            // counted, and permanently unobservable through this surface until
            // somebody declares an instrument.
            rows.push(coverage_row(
                Gap {
                    subject: "component",
                    name: comp_name,
                    member_group: &node.member_group,
                    host_id: &node.host_id,
                    artifact_type: &declared_type,
                    coverage: COVERAGE_NO_INSTRUMENT,
                    detail: "declared `ledger_component: null` — deployed from a playbook rather than a role carrying the ADR 1060 ledger meta-dependency, so no node can ever report a tip for it".to_string(),
                    goatnet,
                },
                g,
            )?);
            continue;
        };
        let Some(observed) = by_ledger.get(key) else {
            // An instrument exists; this node has not reported through it.
            rows.push(coverage_row(
                Gap {
                    subject: "component",
                    name: comp_name,
                    member_group: &node.member_group,
                    host_id: &node.host_id,
                    artifact_type: &declared_type,
                    coverage: COVERAGE_SILENT,
                    detail: format!(
                        "governed and ledger-covered as `{key}`, and this node's convergence record carries no tip for it — not deployed here yet, or the deploying role has not run since the ledger landed"
                    ),
                    goatnet,
                },
                g,
            )?);
            continue;
        };
        components_observed += 1;

        let observed_v = observed_cell(observed);
        let mut cells = BTreeMap::new();
        let expected_text = expected.get("value").and_then(|v| v.as_str()).unwrap_or("").to_string();
        let observed_text = observed_v.get("value").and_then(|v| v.as_str()).unwrap_or("").to_string();
        cells.insert(node.member_group.clone(), expected);
        cells.insert(node.host_id.clone(), observed_v);
        let artifact_type = component
            .get("artifact_type")
            .and_then(|v| v.as_str())
            .unwrap_or(&observed.artifact_type)
            .to_string();
        let row = grade_row(
            "component",
            if comp_name.is_empty() { key } else { comp_name },
            cells,
            goatnet,
            "live",
            false,
            Some(&artifact_type),
            g,
        )?;
        rows.push(flatten(&row, &artifact_type, &observed_text, &expected_text, g)?);
    }

    // -- render rows: the CF-4 watch-set (declared join, converge baseline) --
    //
    // A component's `render_watch` declares WHICH rendered files are its
    // config identity; the node's render manifest carries what the last
    // converge hashed them to, and the record carries what they hash to now.
    // The comparison (`account_for_render`) happens here, at read time.
    //
    // The join is at COMPONENT granularity, deliberately: declared paths may
    // embed `{{ vars }}` (carried literal in IntentV1, the `{{ ghcr_org }}`
    // rule) that only the deploying host can resolve, so a reader cannot match
    // a declared literal to a recorded resolved path. What it CAN honestly
    // grade is each recorded render against its own converge baseline, plus
    // the declared-vs-recorded count — which is how a shorter-than-declared
    // manifest stays visible instead of reading as complete.
    let mut renders_declared = 0usize;
    let mut renders_observed = 0usize;
    for component in governed {
        let expected = expected_cell(
            component.get("members").and_then(|m| m.get(&node.member_group)),
        );
        if expected.get("state").and_then(|v| v.as_str()) == Some("absent") {
            continue;
        }
        let watch: Vec<&str> = component
            .get("render_watch")
            .and_then(|v| v.as_array())
            .map(|a| a.iter().filter_map(|p| p.as_str()).collect())
            .unwrap_or_default();
        if watch.is_empty() {
            continue;
        }
        // The manifest lives under /var/lib/goat/<ledger_component>/, so a
        // component with no ledger home (`ledger_component: null`) has nowhere
        // to record renders; the generator refuses that combination, and this
        // guard keeps the reader honest against an older artifact.
        let Some(key) = ledger_key(component) else { continue };
        let comp_name = component.get("name").and_then(|v| v.as_str()).unwrap_or(key);
        renders_declared += watch.len();

        let mut recs: Vec<&ObservedRender> =
            node.renders.iter().filter(|r| r.component == key).collect();
        recs.sort_by(|a, b| a.path.cmp(&b.path));

        if recs.is_empty() {
            // Default-off recorder: every node is here until a recording
            // converge runs. Nothing-to-read, never a red — the same posture
            // as a node with no applied-intent stamp, for the same reason.
            let cell = serde_json::json!({
                "state": "unreported",
                "detail": format!(
                    "{} declared path(s) to watch, none recorded — the deploying role has not written a render manifest",
                    watch.len()
                ),
            });
            let mut cells = BTreeMap::new();
            cells.insert(node.host_id.clone(), cell);
            let row = grade_row("render", comp_name, cells, goatnet, "live", false, Some("render"), g)?;
            // A declared watch-set with nothing recorded is the SAME shape as a
            // ledger-covered component with no tip: an instrument exists and the
            // node has not reported through it. Marking it `silent` (ADR 1125)
            // keeps it out of the sync collapse, where it used to make a node
            // ungradable for a reason that is about observation, not
            // convergence.
            let mut flat = flatten(&row, "render", "", "", g)?;
            flat.coverage = COVERAGE_SILENT.to_string();
            rows.push(flat);
            continue;
        }

        renders_observed += recs.len();
        for rec in &recs {
            let record = serde_json::json!({
                "component": rec.component,
                "path": rec.path,
                "sha256": rec.sha256,
                "recorded_sha256": rec.recorded_sha256,
                "note": rec.note,
            });
            let cell = account_for_render(&record);
            let mut cells = BTreeMap::new();
            cells.insert(node.host_id.clone(), cell);
            let name = format!("{comp_name}:{}", rec.path);
            let row = grade_row("render", &name, cells, goatnet, "live", false, Some("render"), g)?;
            rows.push(flatten(&row, "render", &rec.sha256, &rec.recorded_sha256, g)?);
        }

        if recs.len() < watch.len() {
            let cell = serde_json::json!({
                "state": "unreported",
                "detail": format!(
                    "{} of {} declared path(s) recorded — the render manifest is shorter than the declared watch-set",
                    recs.len(),
                    watch.len()
                ),
            });
            let mut cells = BTreeMap::new();
            cells.insert(node.host_id.clone(), cell);
            let row = grade_row("render", comp_name, cells, goatnet, "live", false, Some("render"), g)?;
            let mut flat = flatten(&row, "render", "", "", g)?;
            // Partially recorded is still a gap in what can be seen — the
            // recorded paths above are graded on their own merits, and this row
            // is the statement about the ones that are missing.
            flat.coverage = COVERAGE_SILENT.to_string();
            rows.push(flat);
        }
    }

    // TWO FIELDS, TWO ANSWERS (ADR 1125 §A) — and the split is where this got
    // interesting, so the reasoning is here rather than in a commit message.
    //
    // `grade` is the SYNC verdict: the worst over rows that are real
    // comparisons. Coverage rows are excluded from it because "nothing
    // observes this" is not a statement about whether the node converged, and
    // folding it in replaced a convergence verdict with an observability one.
    //
    // `color` is NOT so excluded, and must not be. Excluding coverage from the
    // colour too made a node holding the current generation render GREEN while
    // reporting tips for one of twelve governed components — the false green
    // this entire surface exists to prevent, and the exact shape of F-324. The
    // grammar's own rule is older than this ADR and settles it: nothing
    // ungrounded may ever render green. A coverage gap is ungrounded.
    //
    // So a node with gaps reads `grade: match` (its convergence is fine) with
    // `color: idle` (it is not fully grounded) and `coverage_gaps: N`. The
    // consequence is deliberate and was accepted in the design: goatnets with
    // uninstrumented components stop showing green until the gaps are declared
    // or closed. Making the fleet look worse is the deliverable — the gaps were
    // always there, and only their invisibility is new-ly gone.
    let graded: Vec<&str> =
        rows.iter().filter(|r| r.coverage == COVERAGE_OBSERVED).map(|r| r.grade.as_str()).collect();
    let coverage_gaps = rows.len() - graded.len();
    let grade = g.worst(&graded)?;
    // The colour is the sync grade's colour with ONE floor: a gap may not let
    // a node render green. It is a FLOOR and not a worst-wins, and that
    // distinction is load-bearing — `indeterminate` outranks `unaccountable`
    // in the grammar's precedence, so collapsing gaps in by worst-wins would
    // let an uninstrumented component MASK a red (a hand-edited render, a ref
    // no generation explains). Ungrounded must not read as fine; it must also
    // never read as quieter than a finding.
    let sync_color = g.color(&grade, graded.is_empty())?;
    let color = if coverage_gaps > 0 && sync_color == "green" {
        g.color("indeterminate", false)?
    } else {
        sync_color
    };
    Ok(NodeProjection {
        host_id: node.host_id.clone(),
        member_group: node.member_group.clone(),
        color,
        grade,
        intent_serial: node.intent_serial,
        intent_content_hash: node.intent_content_hash.clone(),
        behind_by,
        intent_age_seconds,
        report_age_ms: node.report_age_ms,
        intent_verified: node.intent_verified,
        components_governed,
        components_observed,
        renders_declared,
        renders_observed,
        coverage_gaps,
        rows,
    })
}

/// The rolled reading for a whole goatnet.
#[derive(Debug, Clone, PartialEq)]
pub struct GoatnetProjection {
    pub goatnet: String,
    pub overall_color: String,
    pub current_serial: u64,
    pub current_content_hash: String,
    pub nodes_expected: u32,
    pub nodes_reporting: u32,
    pub skew_band_generations: u32,
    pub skew_band_seconds: i64,
    pub nodes: Vec<NodeProjection>,
    /// ADR 1125 `coverage: unreported-scope` — members this goatnet's intent
    /// GOVERNS for which no node reported at all. `nodes_reporting` of
    /// `nodes_expected` already carried the arithmetic; this NAMES the missing
    /// ones, because "1 of 2" does not say which one, and the whole F-324
    /// class is a claim about a specific host nobody checked.
    pub unreported_members: Vec<String>,
}

/// Worst *grounded* colour across nodes: red > amber > green; `idle` when nodes
/// exist but none is grounded; `unmetered` when nothing reported at all.
///
/// Lifted verbatim from the time domain's `overall_color`
/// (ops/zenoh-trial-peers/time-health/src/main.rs) rather than re-derived —
/// this is the ADR 1036 §6 rule, and consistency does not get its own.
pub fn overall_color(colors: &[String]) -> &'static str {
    if colors.is_empty() {
        return "unmetered";
    }
    if colors.iter().any(|c| c == "red") {
        "red"
    } else if colors.iter().any(|c| c == "amber") {
        "amber"
    } else if colors.iter().any(|c| c == "green") {
        "green"
    } else {
        "idle"
    }
}

/// Roll a goatnet's reporting nodes into one reading.
///
/// `intent` is None when the rollup could not read or could not verify its
/// goatnet's artifact. It then REFUSES to grade (P14 fail-closed): every node
/// reads `unreadable`/idle rather than green, because a comparison against an
/// intent nobody could trust is not a comparison.
pub fn project_goatnet(
    intent: Option<&Value>,
    nodes: &[ObservedNode],
    now: i64,
    g: &Grammar,
) -> Result<GoatnetProjection, GrammarError> {
    let Some(intent) = intent else {
        return Ok(GoatnetProjection {
            goatnet: String::new(),
            overall_color: if nodes.is_empty() { "unmetered".into() } else { "idle".into() },
            current_serial: 0,
            current_content_hash: String::new(),
            nodes_expected: 0,
            nodes_reporting: nodes.len() as u32,
            skew_band_generations: 0,
            skew_band_seconds: 0,
            nodes: Vec::new(),
            unreported_members: Vec::new(),
        });
    };

    let mut projections = Vec::new();
    for node in nodes {
        projections.push(project_node(intent, node, now, g)?);
    }
    projections.sort_by(|a, b| a.host_id.cmp(&b.host_id));

    let colors: Vec<String> = projections.iter().map(|p| p.color.clone()).collect();

    // The intra-goatnet skew band: the spread ACROSS this goatnet's own nodes,
    // which is the question a per-node grade cannot answer. A zero-wide band
    // means every reporting node holds the SAME generation — whether or not it
    // is the current one. "They agree" and "they are right" are different
    // findings, and the band reports the first while the per-node grades report
    // the second.
    let placeable: Vec<&NodeProjection> = projections
        .iter()
        .filter(|p| !p.intent_content_hash.is_empty() && p.intent_age_seconds >= 0)
        .collect();
    let (skew_band_generations, skew_band_seconds) = if placeable.len() < 2 {
        (0, 0)
    } else {
        let behind: Vec<u32> = placeable.iter().map(|p| p.behind_by).collect();
        let ages: Vec<i64> = placeable.iter().map(|p| p.intent_age_seconds).collect();
        (
            behind.iter().max().unwrap() - behind.iter().min().unwrap(),
            ages.iter().max().unwrap() - ages.iter().min().unwrap(),
        )
    };

    let governed_members: Vec<String> = intent
        .get("members")
        .and_then(|v| v.as_array())
        .map(|a| a.iter().filter_map(|v| v.as_str().map(String::from)).collect())
        .unwrap_or_default();
    let nodes_expected = governed_members.len() as u32;
    // A governed member no node claims. Reported by NAME rather than left to
    // the reader's subtraction: `nodes_reporting` of `nodes_expected` says how
    // many are missing and never which, and every failure in the F-324 class
    // was a specific host nobody looked at.
    let claimed: std::collections::BTreeSet<&str> =
        projections.iter().map(|p| p.member_group.as_str()).collect();
    let unreported_members: Vec<String> = governed_members
        .iter()
        .filter(|m| !claimed.contains(m.as_str()))
        .cloned()
        .collect();

    Ok(GoatnetProjection {
        goatnet: intent.get("goatnet").and_then(|v| v.as_str()).unwrap_or("").to_string(),
        overall_color: overall_color(&colors).to_string(),
        current_serial: intent.get("serial").and_then(|v| v.as_u64()).unwrap_or(0),
        current_content_hash: intent
            .get("content_hash")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        nodes_expected,
        nodes_reporting: projections.len() as u32,
        skew_band_generations,
        skew_band_seconds,
        nodes: projections,
        unreported_members,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn grammar() -> Grammar {
        Grammar::load().unwrap()
    }

    /// One member, three governed components: one ledger-covered and reported,
    /// one declared `ledger_component: null`, one ledger-covered and NOT
    /// reported. Before ADR 1125 the last two were a `continue` each.
    fn intent() -> Value {
        serde_json::json!({
            "goatnet": "alpha",
            "members": ["alpha_hosts", "alpha_standby"],
            "serial": 4,
            "content_hash": "sha256:cafe",
            "generated_at": "2026-08-01T00:00:00Z",
            "components": [
                {"name": "reported", "artifact_type": "image", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/a@sha256:1111"}}},
                {"name": "playbook-deployed", "artifact_type": "image",
                 "ledger_component": null, "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/b@sha256:2222"}}},
                {"name": "never-ran", "artifact_type": "image", "members": {
                    "alpha_hosts": {"state": "resolved", "value": "ghcr.io/x/c@sha256:3333"}}}
            ],
            "capabilities": []
        })
    }

    fn node() -> ObservedNode {
        ObservedNode {
            host_id: "host-a".into(),
            member_group: "alpha_hosts".into(),
            intent_content_hash: "sha256:cafe".into(),
            intent_serial: 4,
            intent_verified: true,
            report_age_ms: 0,
            components: vec![ObservedComponent {
                component: "reported".into(),
                artifact_type: "image".into(),
                reference: "ghcr.io/x/a@sha256:1111".into(),
                reconciled: "confirmed".into(),
                live_ref: "ghcr.io/x/a@sha256:1111".into(),
            }],
            renders: vec![],
        }
    }

    fn row<'a>(p: &'a NodeProjection, name: &str) -> &'a ProjectedRow {
        p.rows.iter().find(|r| r.name == name).unwrap_or_else(|| panic!("no row named {name}"))
    }

    #[test]
    fn a_governed_fact_nothing_observes_is_a_row_not_a_smaller_numerator() {
        let (i, n) = (intent(), node());
        let p = project_node(&i, &n, 1_785_801_600, &grammar()).unwrap();

        // The denominator is unchanged — all three are governed, one observed.
        assert_eq!(p.components_governed, 3);
        assert_eq!(p.components_observed, 1);

        // But the two that used to vanish now have rows.
        let reported = row(&p, "reported");
        assert_eq!(reported.grade, "match");
        assert_eq!(reported.coverage, COVERAGE_OBSERVED);
        assert!(reported.remediation.is_none(), "a match needs no pointer");

        let no_inst = row(&p, "playbook-deployed");
        assert_eq!(no_inst.coverage, COVERAGE_NO_INSTRUMENT);
        assert_eq!(no_inst.grade, "indeterminate", "nothing to read is never match");
        assert_ne!(no_inst.color, "green", "coverage gaps are never green");

        let silent = row(&p, "never-ran");
        assert_eq!(silent.coverage, COVERAGE_SILENT);
        assert_eq!(silent.grade, "indeterminate");
    }

    /// The reason coverage carries its OWN pointer: `indeterminate`/live
    /// dispatches to `reconverge`, and re-running the play cannot create ledger
    /// coverage. A wrong pointer turns a permanent gap into a suspected
    /// transient.
    #[test]
    fn no_instrument_points_at_declaring_an_instrument_not_at_re_running_the_play() {
        let (i, n) = (intent(), node());
        let p = project_node(&i, &n, 1_785_801_600, &grammar()).unwrap();

        let no_inst = row(&p, "playbook-deployed").remediation.as_ref().expect("a pointer");
        assert_eq!(no_inst.kind, "instrument");
        assert!(no_inst.pointer.contains("ledger_component"), "{}", no_inst.pointer);
        assert!(no_inst.pointer.contains("playbook-deployed"), "the pointer names the subject");

        // A node that simply has not reported IS the re-run case, and keeps it.
        let silent = row(&p, "never-ran").remediation.as_ref().expect("a pointer");
        assert_eq!(silent.kind, "reconverge");

        // Every coverage state the grammar declares resolves to a known kind —
        // an unknown state fails loud rather than emitting a pointerless row.
        let g = grammar();
        for state in g.coverage_states() {
            g.coverage_remediation(state).expect("a declared state resolves");
        }
        assert!(g.coverage_remediation("invented").is_err());
    }

    #[test]
    fn a_governed_member_no_node_claims_is_named_not_left_to_subtraction() {
        let (i, n) = (intent(), node());
        let p = project_goatnet(Some(&i), std::slice::from_ref(&n), 1_785_801_600, &grammar())
            .unwrap();
        assert_eq!(p.nodes_expected, 2);
        assert_eq!(p.nodes_reporting, 1);
        assert_eq!(
            p.unreported_members,
            vec!["alpha_standby".to_string()],
            "1-of-2 says how many are missing and never WHICH — F-324 was a specific host"
        );

        // Nothing reporting at all is every member unreported, never silence.
        let none = project_goatnet(Some(&i), &[], 1_785_801_600, &grammar()).unwrap();
        assert_eq!(none.unreported_members.len(), 2);
        assert_ne!(none.overall_color, "green");
    }
}
