// SPDX-License-Identifier: Apache-2.0
//
// release.rs — the Release readiness view (docs/design/release-graduation.md
// §8, ADR 1120), served inside the CF-6 console
// (docs/design/consistency-foundation.md §7.13).
//
// The question this page answers: for every ladder component — what is the
// value of record, what does each goatnet actually run, what is soaking and
// how much longer, what is ready to graduate, and when something is not
// ready, WHAT blocks it.
//
// THIS FILE COMPUTES NO LADDER FACT. Every grade, blocker, soak clock and
// ledger join arrives already computed in the `release-readiness-v1`
// document that `ops/release/graduate.py readiness` writes — the same
// `build_status()` grading and the same `gate_failures()` refusal `graduate`
// itself runs, so the page cannot say a component is ready that the tool
// would refuse. What lives here is presentation: a vocabulary table for the
// ladder's closed grade set, and the honesty rules below.
//
// Honesty rules (the §7.5 set, applied to a document rather than a wire):
//   - the document is a SNAPSHOT taken by the deploy that placed it. Its age
//     is rendered on the page, always, next to the commit it was generated
//     from — and past the console's redeploy rhythm the page DEGRADES into a
//     stated stale state rather than printing an old number in the same voice
//     as a fresh one. A readiness page that quietly ages is the failure mode
//     this whole surface exists to prevent;
//   - an absent or unreadable document renders as a refusal that says which
//     path was read. Never an empty table, never a green one;
//   - `null` is not `false`. A ledger question that could not be asked reads
//     "not asked" and looks different from "no";
//   - no bar, no percentage, no countdown the document does not carry: a soak
//     with an unusable clock says so instead of animating zero.
//
// READ-ONLY, like everything else on this console: no route here writes, and
// the document is re-read from disk on every render rather than cached, so a
// redeployed file takes effect without a restart.

use crate::render::{esc, STYLE};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

pub const SCHEMA: &str = "release-readiness-v1";

// ---------------------------------------------------------------------------
// The document. Every field is optional or defaulted: a document produced by
// a newer generator must degrade to "unknown" on this console, never to a
// parse failure that takes the whole page with it.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct ReleaseDoc {
    #[serde(default)]
    pub schema: String,
    #[serde(default)]
    pub generated_unix_ms: i64,
    #[serde(default)]
    pub generated_date: Option<String>,
    #[serde(default)]
    pub source: Source,
    #[serde(default)]
    pub ladder: Ladder,
    #[serde(default)]
    pub unknowable: Vec<String>,
    #[serde(default)]
    pub components: Vec<Component>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Source {
    #[serde(default)]
    pub head_commit: Option<String>,
    #[serde(default)]
    pub git: Option<String>,
    #[serde(default)]
    pub note: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Ladder {
    #[serde(default)]
    pub rungs: Vec<Rung>,
    #[serde(default)]
    pub hop: Option<Hop>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Rung {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub goatnet: String,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Hop {
    #[serde(default, rename = "from")]
    pub from_rung: String,
    #[serde(default)]
    pub to: String,
    #[serde(default)]
    pub gates: Vec<Gate>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Gate {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub min_days: Option<i64>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Component {
    #[serde(default)]
    pub component: String,
    #[serde(default)]
    pub class: String,
    #[serde(default)]
    pub grade: String,
    #[serde(default)]
    pub var: Option<String>,
    #[serde(default)]
    pub artifact_type: Option<String>,
    #[serde(default)]
    pub fleet: Option<String>,
    #[serde(default)]
    pub image_workflow: Option<ImageWorkflow>,
    #[serde(default)]
    pub rungs: Vec<RungValue>,
    #[serde(default)]
    pub member_deviations: Vec<MemberDeviation>,
    #[serde(default)]
    pub candidate: Option<Candidate>,
    #[serde(default)]
    pub readiness_state: String,
    #[serde(default)]
    pub last_graduated: Option<Graduated>,
    #[serde(default)]
    pub ledger: Ledger,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct RungValue {
    #[serde(default)]
    pub rung: String,
    #[serde(default)]
    pub goatnet: String,
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub value: Option<String>,
    #[serde(default)]
    pub deviates_from_fleet: bool,
    #[serde(default)]
    pub deviation_reason: Option<String>,
    #[serde(default)]
    pub build: Build,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Build {
    #[serde(default)]
    pub tag: Option<String>,
    #[serde(default)]
    pub known: bool,
    #[serde(default)]
    pub why: Option<String>,
    #[serde(default)]
    pub commit: Option<String>,
    #[serde(default)]
    pub commit_date: Option<String>,
    #[serde(default)]
    pub commits_on_head_since: Option<i64>,
    /// The component-attributed reading: commits since this rung's pinned build
    /// that touched the paths its image workflow builds on. Absent on a document
    /// written by an older generator — which renders unknown, not zero.
    #[serde(default)]
    pub component_commits: Option<ComponentCommits>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct ComponentCommits {
    #[serde(default)]
    pub known: bool,
    #[serde(default)]
    pub why: Option<String>,
    #[serde(default)]
    pub workflow: Option<String>,
    #[serde(default)]
    pub paths: Vec<String>,
    #[serde(default)]
    pub count: Option<i64>,
    #[serde(default)]
    pub newest: Option<CommitRef>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct CommitRef {
    #[serde(default)]
    pub sha: Option<String>,
    #[serde(default)]
    pub date: Option<String>,
    #[serde(default)]
    pub subject: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct ImageWorkflow {
    #[serde(default)]
    pub workflow: Option<String>,
    #[serde(default)]
    pub paths: Option<Vec<String>>,
    #[serde(default)]
    pub why: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct MemberDeviation {
    #[serde(default)]
    pub member: String,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub absent: bool,
    #[serde(default)]
    pub value: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Candidate {
    #[serde(default)]
    pub path: Option<String>,
    #[serde(default)]
    pub class: String,
    #[serde(default)]
    pub candidate_ref: Option<String>,
    #[serde(default)]
    pub previous_fleet_ref: Option<String>,
    #[serde(default)]
    pub opened: Option<String>,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub evidence: BTreeMap<String, bool>,
    #[serde(default)]
    pub note: Option<String>,
    #[serde(default)]
    pub soak: Soak,
    #[serde(default)]
    pub readiness: Readiness,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Soak {
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub required_days: Option<i64>,
    #[serde(default)]
    pub opened: Option<String>,
    #[serde(default)]
    pub elapsed_days: Option<i64>,
    #[serde(default)]
    pub remaining_days: Option<i64>,
    #[serde(default)]
    pub clock: Option<String>,
    #[serde(default)]
    pub why: Option<String>,
    #[serde(default)]
    pub waiver: Option<Waiver>,
}

/// An operator's recorded decision that the soak window did not have to
/// elapse. `by` absent is a waiver the record could not attribute — rendered
/// as unknown attribution, never as an anonymous approval.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Waiver {
    #[serde(default)]
    pub by: Option<String>,
    #[serde(default)]
    pub attribution: String,
    #[serde(default)]
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Readiness {
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub blockers: Vec<String>,
    #[serde(default)]
    pub ack_required: bool,
    #[serde(default)]
    pub operator_ack: Option<String>,
    #[serde(default)]
    pub soak_waiver: Option<Waiver>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Graduated {
    #[serde(default, rename = "ref")]
    pub graduated_ref: Option<String>,
    #[serde(default)]
    pub on: Option<String>,
    #[serde(default)]
    pub ack: Option<String>,
    #[serde(default)]
    pub soak_waiver: Option<Waiver>,
    #[serde(default)]
    pub path: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct Ledger {
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub path: Option<String>,
    #[serde(default)]
    pub newest: Option<LedgerRow>,
    /// `Some(true)` deployed, `Some(false)` not deployed, `None` the question
    /// could not be asked. The three MUST stay distinguishable all the way to
    /// the cell — a `None` flattened to `false` invents a finding.
    #[serde(default)]
    pub pin_deployed: BTreeMap<String, Option<bool>>,
    #[serde(default)]
    pub newest_is_pinned: Option<bool>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct LedgerRow {
    #[serde(default)]
    pub sha: Option<String>,
    #[serde(default)]
    pub image: Option<String>,
    #[serde(default)]
    pub digest: Option<String>,
    #[serde(default)]
    pub pr: Option<serde_json::Value>,
    #[serde(default)]
    pub merged_at: Option<String>,
    #[serde(default)]
    pub ordered_by: Option<String>,
}

/// Read + parse the document. `Err` carries the sentence the page shows —
/// there is no silent fallback to an empty document, because an empty table
/// on this page reads as "nothing to release".
pub fn load(path: &str) -> Result<ReleaseDoc, String> {
    let text = std::fs::read_to_string(path).map_err(|e| {
        format!(
            "no release-readiness document at {path}: {e}. It is written by \
             `ops/release/graduate.py readiness` and placed by the \
             consistency-console role at deploy time — a console deployed \
             before that role change carries none."
        )
    })?;
    let doc: ReleaseDoc = serde_json::from_str(&text)
        .map_err(|e| format!("the document at {path} did not parse as JSON: {e}"))?;
    if doc.schema != SCHEMA {
        return Err(format!(
            "the document at {path} declares schema `{}`, not `{SCHEMA}` — refusing to \
             render a document whose shape this console does not know",
            doc.schema
        ));
    }
    Ok(doc)
}

// ---------------------------------------------------------------------------
// Vocabulary. The ladder's grade set (ADR 1120) is NOT the consistency
// grammar's, so it cannot come from grammar.yaml; this is the one table that
// translates it, in the same shape as render.rs's `grade_human`. A term this
// table does not know renders as itself on a neutral chip — the console does
// not invent vocabulary.
// ---------------------------------------------------------------------------

fn ladder_grade(grade: &str) -> (&'static str, &'static str, String) {
    match grade {
        "aligned" => ("green", "aligned", "every live rung runs the same ref".into()),
        "candidate" => ("idle", "candidate on preprod", "preprod is ahead of prod with an open record".into()),
        "hotfix-backfill-owed" => ("amber", "hotfix — backfill owed", "prod is ahead of preprod; the ref still owes a normal graduation".into()),
        "legacy-declared" => ("amber", "legacy skew (allowlisted)", "declared in the ladder's legacy allowlist, not by a record".into()),
        "UNDECLARED-SKEW" => ("red", "undeclared skew", "the rungs differ and no record or allowlist row says why — CI refuses this".into()),
        "env-local" => ("unmetered", "env-local", "the component is absent on a rung; there is nothing to compare".into()),
        "single-rung" => ("unmetered", "single rung", "only one rung resolves a value".into()),
        "exempt" => ("unmetered", "exempt", "declared out of the ladder's scope".into()),
        other => ("idle", "", format!("`{other}` is not a ladder grade this console knows")),
    }
}

fn readiness_state(state: &str) -> (&'static str, &'static str, String) {
    match state {
        "ready" => ("green", "ready to graduate", "every hop gate passes — `graduate.py graduate` would bless the fold".into()),
        "soaking" => ("idle", "soaking", "the only unmet gate is the soak window".into()),
        "soak-waived" => ("amber", "soak waived", "the soak window is unmet and the record carries an operator's waiver — `graduate` still refuses on the gate; the waiver records the decision, it does not override it".into()),
        "blocked" => ("amber", "blocked", "one or more hop gates fail; the reasons are listed on the row".into()),
        "no-candidate" => ("unmetered", "no open candidate", "nothing is being promoted for this component right now".into()),
        "not-applicable" => ("unmetered", "not on the ladder", "the component is declared exempt".into()),
        other => ("idle", "", format!("`{other}` is not a readiness state this console knows")),
    }
}

fn chip(color: &str, label: &str, title: &str) -> String {
    format!(
        r#"<span class="chip chip-{}" title="{}">{}</span>"#,
        esc(color),
        esc(title),
        esc(label)
    )
}

fn grade_chip(grade: &str) -> String {
    let (color, label, meaning) = ladder_grade(grade);
    let label = if label.is_empty() { grade } else { label };
    chip(color, label, &format!("ladder grade: {grade} — {meaning}"))
}

fn readiness_chip(state: &str) -> String {
    let (color, label, meaning) = readiness_state(state);
    let label = if label.is_empty() { state } else { label };
    chip(color, label, &format!("{state} — {meaning}"))
}

/// The identity a human compares: the `sha-…` tag when the ref carries one,
/// else the digest's first bytes. The verbatim ref rides the hover title —
/// the same display rule the drift grid uses.
fn ref_identity(value: Option<&String>) -> String {
    let Some(v) = value else {
        return r#"<span class="dim">not pinned</span>"#.into();
    };
    let tag = v
        .rsplit_once(":sha-")
        .and_then(|(_, rest)| rest.split('@').next())
        .map(|s| format!("sha-{s}"));
    let shown = tag.unwrap_or_else(|| match v.split_once("@sha256:") {
        Some((_, d)) => format!("sha256:{}…", &d[..d.len().min(12)]),
        None => v.clone(),
    });
    format!(r#"<span class="mono" title="{}">{}</span>"#, esc(v), esc(&shown))
}

/// The reading this page leads with: has anything landed that BUILDS this
/// component since the build this rung pins. The count comes from the
/// component's declared image workflow `paths:` filter — the same filter
/// GitHub evaluates to decide whether a push produces an image — so a count
/// above zero means images were almost certainly built that this rung's pin
/// does not name. Anything the generator could not evaluate arrives with
/// `known: false` and renders as an unknown carrying its reason.
fn component_commits_note(cc: Option<&ComponentCommits>) -> String {
    let Some(cc) = cc else {
        return format!(
            r#"{} <span class="dim">this document carries no per-component build-input reading (written by an older generator)</span>"#,
            chip("unmetered", "newer build unknown", "the document predates the per-component reading")
        );
    };
    let workflow = cc.workflow.as_deref().unwrap_or("no workflow declared");
    match (cc.known, cc.count) {
        (true, Some(0)) => format!(
            r#"{} <span class="dim">nothing has touched this component's build inputs since the pinned build — the deployed build is current for this component's sources (paths declared by <code>{}</code>)</span>"#,
            chip("green", "build current", "no commit since the pinned build touches the paths this component's image workflow builds on"),
            esc(workflow),
        ),
        // Rung-scoped wording on purpose: another rung may well pin a build
        // from these commits (that is what a candidate IS). The "no pin names
        // it" claim is made once, at component level, where it is checkable.
        (true, Some(n)) => format!(
            r#"{} <span>{} commit(s) since the pinned build touch this component's build inputs (the <code>paths:</code> filter of {}) — a newer image for this component almost certainly exists, and this rung does not run it.{}</span>"#,
            chip("amber", "newer build unpinned", "commits that this component's image workflow builds on have landed since the pinned build"),
            n,
            esc(workflow),
            match &cc.newest {
                Some(c) => format!(
                    r#" newest: <span class="mono">{}</span> {} — {}"#,
                    esc(c.sha.as_deref().unwrap_or("?")),
                    esc(c.date.as_deref().unwrap_or("date unknown")),
                    esc(c.subject.as_deref().unwrap_or("")),
                ),
                None => String::new(),
            },
        ),
        _ => format!(
            r#"{} <span class="dim">{}</span>"#,
            chip("unmetered", "newer build unknown", "the tree cannot say whether a newer image exists for this component"),
            esc(cc.why.as_deref().unwrap_or("no reason stated")),
        ),
    }
}

fn build_note(b: &Build) -> String {
    let component = component_commits_note(b.component_commits.as_ref());
    if !b.known {
        let why = b.why.clone().unwrap_or_else(|| "not derivable from the tree".into());
        return format!(
            r#"{component}<br><span class="dim">build commit unknown — {}</span>"#,
            esc(&why)
        );
    }
    let date = b.commit_date.as_deref().unwrap_or("date unknown");
    let all = match b.commits_on_head_since {
        Some(n) => format!(
            r#"<span class="dim">built from {} ({}); the generating checkout carried {} later commit(s) — all commits, not this component's</span>"#,
            esc(b.tag.as_deref().unwrap_or("?")),
            esc(date),
            n
        ),
        None => format!(
            r#"<span class="dim">built from {} ({})</span>"#,
            esc(b.tag.as_deref().unwrap_or("?")),
            esc(date)
        ),
    };
    format!("{component}<br>{all}")
}

/// The ledger cell. Three states, three different sentences — `None` never
/// collapses into "no".
fn deployed_cell(v: Option<&Option<bool>>) -> String {
    match v {
        Some(Some(true)) => chip("green", "deployed", "a ledger row records this exact digest"),
        Some(Some(false)) => chip(
            "amber",
            "no deploy recorded",
            "the pinned digest appears in no deploy-history row for this component — pinned but never recorded as deployed, or deployed by a path that writes no ledger row",
        ),
        _ => chip(
            "unmetered",
            "not asked",
            "no ledger coverage, no ledger file, or a pin with no digest to join on — the question could not be put",
        ),
    }
}

fn soak_line(s: &Soak) -> String {
    match s.state.as_str() {
        "met" | "soaking" => {
            let elapsed = match s.elapsed_days {
                Some(d) => format!("{d}d elapsed"),
                None => "elapsed unknown".into(),
            };
            let required = match s.required_days {
                Some(d) => format!("window {d}d"),
                None => "window unstated".into(),
            };
            let remaining = match (s.state.as_str(), s.remaining_days) {
                ("soaking", Some(r)) => format!(", {r}d remaining"),
                _ => String::new(),
            };
            format!(
                "{} — {elapsed}, {required}{remaining} <span class=\"dim\">(clock: {})</span>{}",
                chip(
                    if s.state == "met" { "green" } else { "idle" },
                    if s.state == "met" { "soak met" } else { "soaking" },
                    "measured from the record's opened date"
                ),
                esc(s.clock.as_deref().unwrap_or("unstated")),
                // A waiver on a window that elapsed anyway is not a finding,
                // but dropping it would hide that the decision was taken.
                match &s.waiver {
                    Some(w) => format!(
                        r#" <span class="dim">(a waiver is recorded, and the window elapsed regardless: {})</span>"#,
                        waiver_line(w)
                    ),
                    None => String::new(),
                },
            )
        }
        "waived" => {
            let window = match (s.elapsed_days, s.required_days) {
                (Some(e), Some(r)) => format!(" — {e}d elapsed of a {r}d window"),
                (None, _) => format!(
                    " — elapsed unknown ({})",
                    esc(s.why.as_deref().unwrap_or("the record's clock could not be read"))
                ),
                _ => String::new(),
            };
            let w = s.waiver.clone().unwrap_or_default();
            format!(
                "{} {}{window} <span class=\"dim\">(`graduate` still refuses on the soak gate — the waiver records the decision, it does not override it)</span>",
                chip(
                    if w.by.is_some() { "amber" } else { "red" },
                    if w.by.is_some() { "soak waived" } else { "soak waived — no attribution" },
                    "an operator decided the window did not have to elapse; this is not a met window",
                ),
                waiver_line(&w),
            )
        }
        "unknown" => format!(
            "{} <span class=\"dim\">{}</span>",
            chip("unmetered", "soak unknown", "the record's clock could not be read"),
            esc(s.why.as_deref().unwrap_or("the record carries no usable opened date")),
        ),
        "no-soak-gate" => chip("unmetered", "no soak gate", "this ladder hop declares no soak window"),
        other => chip("idle", other, "a soak state this console does not know"),
    }
}

fn blockers_html(r: &Readiness) -> String {
    if r.blockers.is_empty() {
        return String::new();
    }
    let items: String = r
        .blockers
        .iter()
        .map(|b| format!("<li>{}</li>", esc(b)))
        .collect();
    format!(
        r#"<div class="rel-block"><strong>blocked by</strong><ul class="rel-blockers">{items}</ul>
<p class="grounding">verbatim from <code>graduate.py</code>'s own gate check — the same refusal
`graduate` prints.</p></div>"#
    )
}

fn candidate_html(c: &Candidate) -> String {
    let ev: String = c
        .evidence
        .iter()
        .map(|(k, present)| {
            format!(
                " {}",
                chip(
                    if *present { "green" } else { "amber" },
                    &format!("{k}: {}", if *present { "named" } else { "empty" }),
                    "the gate is a PRESENCE check — the tool cannot verify the evidence is true",
                )
            )
        })
        .collect();
    let ack = match (&c.readiness.operator_ack, c.readiness.ack_required) {
        (Some(a), _) => format!("ack <span class=\"mono\">{}</span>", esc(a)),
        (None, true) => "<span class=\"dim\">no operator ack yet (the hop requires one)</span>".into(),
        (None, false) => "<span class=\"dim\">no ack required by this hop</span>".into(),
    };
    format!(
        r#"<div class="rel-block">
<p><strong>open candidate</strong> {} &middot; opened {} &middot; {} &middot; {}</p>
<p>{}</p>
<p class="rel-reason">{}</p>
<p class="grounding">evidence:{} &middot; record <code>{}</code></p>
{}
</div>"#,
        ref_identity(c.candidate_ref.as_ref()),
        esc(c.opened.as_deref().unwrap_or("date unstated")),
        esc(if c.class.is_empty() { "ladder" } else { &c.class }),
        ack,
        soak_line(&c.soak),
        esc(c.reason.as_deref().unwrap_or("")),
        ev,
        esc(c.path.as_deref().unwrap_or("path unstated")),
        blockers_html(&c.readiness),
    )
}

/// The component-level form of the build-input reading. A per-rung cell can
/// only say that one rung is behind; when EVERY rung with a usable reading is
/// behind, no pin in the table names those builds — the built-but-pinned-
/// nowhere gap this page exists to show. Stated only when every reading is
/// usable: one unknown rung and the claim becomes an inference.
fn unpinned_build_html(c: &Component) -> String {
    let readings: Vec<&ComponentCommits> = c
        .rungs
        .iter()
        .filter(|r| r.state != "absent")
        .filter_map(|r| r.build.component_commits.as_ref())
        .collect();
    let inputs = match c.image_workflow.as_ref().and_then(|w| w.workflow.as_deref()) {
        Some(w) => format!(
            r#"<p class="grounding">build inputs: the <code>paths:</code> filter of <code>{}</code>, declared for this component in <code>ops/pins/fleet-pins.yaml</code>.</p>"#,
            esc(w)
        ),
        None => format!(
            r#"<p class="grounding">build inputs: undeclared — {}</p>"#,
            esc(c
                .image_workflow
                .as_ref()
                .and_then(|w| w.why.as_deref())
                .unwrap_or("this document names no image workflow for the component")),
        ),
    };
    let counts: Vec<i64> = readings
        .iter()
        .filter(|cc| cc.known)
        .filter_map(|cc| cc.count)
        .collect();
    let finding = if readings.is_empty() || counts.len() != readings.len() {
        format!(
            r#"<p class="rel-reason">{} whether a build exists that no rung pins cannot be settled for this component — at least one rung's reading is unknown.</p>"#,
            chip("unmetered", "unpinned build unknown", "one or more rungs could not be read")
        )
    } else if counts.contains(&0) {
        format!(
            r#"<p class="rel-reason">{} at least one rung pins a build current with this component's sources.</p>"#,
            chip("green", "a rung is current", "some live rung runs a build no later commit has invalidated")
        )
    } else {
        let least = counts.iter().min().copied().unwrap_or_default();
        let newest = readings
            .iter()
            .filter_map(|cc| cc.newest.as_ref())
            .max_by(|a, b| a.date.cmp(&b.date));
        format!(
            r#"<p class="rel-reason">{} every live rung is behind this component's own sources by at least {} commit(s): a newer image was almost certainly built and NO pin in the table names it.{}</p>"#,
            chip("amber", "built, pinned nowhere", "no rung pins a build as new as this component's sources"),
            least,
            match newest {
                Some(cm) => format!(
                    r#" newest such commit: <span class="mono">{}</span> {} — {}"#,
                    esc(cm.sha.as_deref().unwrap_or("?")),
                    esc(cm.date.as_deref().unwrap_or("date unknown")),
                    esc(cm.subject.as_deref().unwrap_or("")),
                ),
                None => String::new(),
            },
        )
    };
    format!(r#"<div class="rel-block">{finding}{inputs}</div>"#)
}

fn waiver_line(w: &Waiver) -> String {
    let who = match (&w.by, w.attribution.as_str()) {
        (Some(by), _) => format!("waived by <span class=\"mono\">{}</span>", esc(by)),
        (None, _) => "<strong>waived — attribution unknown</strong>: the record names no waiver author".into(),
    };
    format!(
        "{who} — {}",
        esc(w.reason.as_deref().unwrap_or("no reason recorded")),
    )
}

fn deviations_html(c: &Component) -> String {
    let mut out = String::new();
    for r in &c.rungs {
        if r.deviates_from_fleet {
            out.push_str(&format!(
                r#"<p class="rel-dev">{} <strong>{}</strong> runs a declared deviation from the fleet value: {}</p>"#,
                chip("amber", "deviation", "a per-goatnet value declared with a reason — a correct state, not drift"),
                esc(&r.goatnet),
                esc(r.deviation_reason.as_deref().unwrap_or("no reason recorded — CI refuses this")),
            ));
        }
    }
    for m in &c.member_deviations {
        out.push_str(&format!(
            r#"<p class="rel-dev">{} <strong>{}</strong>{}: {}</p>"#,
            chip("unmetered", "member deviation", "intra-goatnet (node parity's axis) — the ladder compares goatnets, not members"),
            esc(&m.member),
            if m.absent { " (absent)" } else { "" },
            esc(m.reason.as_deref().unwrap_or("no reason recorded")),
        ));
    }
    out
}

fn ledger_html(c: &Component) -> String {
    let l = &c.ledger;
    let newest = match &l.newest {
        Some(n) => format!(
            "newest row {} &middot; PR {} &middot; merged {} &middot; {}",
            ref_identity(n.image.as_ref()),
            n.pr.as_ref().map(|p| p.to_string()).unwrap_or_else(|| "unstated".into()),
            esc(n.merged_at.as_deref().unwrap_or("unstated")),
            match l.newest_is_pinned {
                Some(true) => chip("green", "is a current pin", "the newest recorded deploy is a value the pin table currently names"),
                Some(false) => chip("amber", "not a current pin", "the newest recorded deploy is a digest no current pin names — check whether a bump was lost or the row belongs to a pin outside the ladder table"),
                None => chip("unmetered", "not asked", "no digest to join on"),
            },
        ),
        None => format!(
            r#"<span class="dim">no deploy row: {}</span>"#,
            esc(&l.status)
        ),
    };
    format!(
        r#"<p class="grounding">deploy ledger ({}): {}{}</p>"#,
        esc(l.path.as_deref().unwrap_or(&l.status)),
        newest,
        match &l.newest {
            Some(n) => n
                .ordered_by
                .as_deref()
                .filter(|o| !o.starts_with("merged_at"))
                .map(|o| format!(" <span class=\"dim\">(ordered by {})</span>", esc(o)))
                .unwrap_or_default(),
            None => String::new(),
        }
    )
}

fn component_html(c: &Component, rungs: &[Rung]) -> String {
    let mut cells = String::new();
    // The build note repeats verbatim when two rungs run the same ref; say it
    // once, on the first column that introduces the value (the drift grid's
    // render-identical-values-once rule). The VALUE itself still renders in
    // every column — collapsing that would hide which rung holds what.
    let mut previous: Option<&Option<String>> = None;
    for r in rungs {
        let rv = c.rungs.iter().find(|x| x.rung == r.name);
        let repeats = matches!((rv, previous), (Some(rv), Some(p)) if &rv.value == p);
        previous = rv.map(|rv| &rv.value);
        let (value, note, deployed) = match rv {
            Some(rv) if repeats => (
                ref_identity(rv.value.as_ref()),
                r#"<span class="dim">same as the rung to its left</span>"#.to_string(),
                deployed_cell(c.ledger.pin_deployed.get(&r.name)),
            ),
            Some(rv) if rv.state == "absent" => (
                r#"<span class="dim">absent</span>"#.to_string(),
                String::new(),
                deployed_cell(c.ledger.pin_deployed.get(&r.name)),
            ),
            Some(rv) => (
                ref_identity(rv.value.as_ref()),
                build_note(&rv.build),
                deployed_cell(c.ledger.pin_deployed.get(&r.name)),
            ),
            None => (
                r#"<span class="dim">no value in the document</span>"#.to_string(),
                String::new(),
                deployed_cell(None),
            ),
        };
        cells.push_str(&format!(
            r#"<td><div class="rel-val">{value} {deployed}</div><div class="rel-meta">{note}</div></td>"#
        ));
    }
    let detail = format!(
        "{}{}{}{}",
        unpinned_build_html(c),
        c.candidate.as_ref().map(candidate_html).unwrap_or_default(),
        deviations_html(c),
        ledger_html(c),
    );
    let last = match &c.last_graduated {
        Some(gd) => format!(
            r#"<p class="grounding">last graduated {} on {} (ack {}){} &middot; <code>{}</code></p>"#,
            ref_identity(gd.graduated_ref.as_ref()),
            esc(gd.on.as_deref().unwrap_or("date unstated")),
            esc(gd.ack.as_deref().unwrap_or("unstated")),
            match &gd.soak_waiver {
                Some(w) => format!(" &middot; {} {}", chip("amber", "soak waived", "that graduation did not wait out the soak window"), waiver_line(w)),
                None => String::new(),
            },
            esc(gd.path.as_deref().unwrap_or("")),
        ),
        None => r#"<p class="grounding">no graduated record for this component yet</p>"#.into(),
    };
    format!(
        r#"<tr class="rel-row">
<th scope="row"><div class="mono">{}</div><div class="rel-meta">{}</div></th>
<td>{} {}</td>
{cells}
</tr>
<tr class="rel-detail"><td colspan="{}"><details><summary>detail</summary>{detail}{last}</details></td></tr>"#,
        esc(&c.component),
        esc(c.var.as_deref().unwrap_or("")),
        readiness_chip(&c.readiness_state),
        grade_chip(&c.grade),
        rungs.len() + 2,
    )
}

fn age_text(ms: i64) -> String {
    if ms < 0 {
        return "generated in the future — clocks disagree".into();
    }
    let s = ms / 1000;
    if s >= 86_400 {
        format!("{}d ago", s / 86_400)
    } else if s >= 3_600 {
        format!("{}h ago", s / 3_600)
    } else {
        format!("{}m ago", s / 60)
    }
}

// How old the snapshot may get before the page says so in its own state. The
// document is rewritten by every console deploy, and the ladder it renders
// moves in days — the soak window is 1 day and components graduated several
// times a week through . So a week without a redeploy means the page has
// certainly missed pins, records and merges; a month means nothing on it can be
// read as current. Both windows are about the DEPLOY rhythm, not the ladder's
// gates: the fix is always the same, redeploy the console.
const STALE_AFTER_DAYS: i64 = 7;
const VERY_STALE_AFTER_DAYS: i64 = 30;

const REFRESH: &str = "Redeploy the consistency console (the <code>consistency-console</code> \
     role regenerates this document from the deploying checkout) — nothing else refreshes it.";

/// (chip, severity class, what the operator should do). The three degraded
/// cases — aged out, no stamp, clocks disagreeing — each get their own words;
/// none of them may look like the fresh case.
fn age_state(doc: &ReleaseDoc, now_ms: i64) -> (String, &'static str, String) {
    if doc.generated_unix_ms <= 0 {
        return (
            chip("unmetered", "age unstatable", "the document carries no generation stamp, so its age cannot be computed"),
            "prov-degraded",
            format!("age unstatable — the document carries no generation stamp, so how stale it is cannot be known. {REFRESH}"),
        );
    }
    let delta = now_ms - doc.generated_unix_ms;
    if delta < 0 {
        return (
            chip("amber", "clock skew", "the document is stamped in the future relative to this host"),
            "prov-degraded",
            format!("{} — the generating checkout's clock and this host's disagree, so the age below is not usable.", age_text(delta)),
        );
    }
    let days = delta / 86_400_000;
    let age = age_text(delta);
    if days >= VERY_STALE_AFTER_DAYS {
        (
            chip("red", "very stale", "no console deploy has refreshed this document in over a month"),
            "prov-degraded sev-red",
            format!("generated {age} — over {VERY_STALE_AFTER_DAYS} days without a console deploy. Read nothing on this page as current. {REFRESH}"),
        )
    } else if days >= STALE_AFTER_DAYS {
        (
            chip("amber", "stale", "no console deploy has refreshed this document in over a week"),
            "prov-degraded",
            format!("generated {age} — over {STALE_AFTER_DAYS} days without a console deploy, so pins, records and merges since then are missing here. {REFRESH}"),
        )
    } else {
        (
            chip("green", "fresh", "written by a console deploy inside the redeploy rhythm this page assumes"),
            "",
            format!("generated {age}."),
        )
    }
}

fn provenance_html(doc: &ReleaseDoc, now_ms: i64) -> String {
    let (state_chip, sev, sentence) = age_state(doc, now_ms);
    format!(
        r#"<p class="grounding {sev}">{state_chip} document dated {} &middot; {sentence} From commit <span class="mono">{}</span>.
It is a SNAPSHOT written by the deploy that placed it, not a live reading: merges, records
and pins that landed after that commit are not in it.</p>"#,
        esc(doc.generated_date.as_deref().unwrap_or("date unstated")),
        esc(doc.source.head_commit.as_deref().unwrap_or("unknown")),
    )
}

fn unknowable_html(doc: &ReleaseDoc) -> String {
    if doc.unknowable.is_empty() {
        return String::new();
    }
    let items: String = doc
        .unknowable
        .iter()
        .map(|u| format!("<li>{}</li>", esc(u)))
        .collect();
    format!(
        r#"<details class="rel-unknowable"><summary>what this page cannot know</summary><ul>{items}</ul></details>"#
    )
}

pub fn render_body(doc: &ReleaseDoc, now_ms: i64) -> String {
    let rungs: Vec<Rung> = if doc.ladder.rungs.is_empty() {
        // A document with no rung list still renders its components; the
        // columns are then whatever the first component names.
        doc.components
            .first()
            .map(|c| {
                c.rungs
                    .iter()
                    .map(|r| Rung { name: r.rung.clone(), goatnet: r.goatnet.clone() })
                    .collect()
            })
            .unwrap_or_default()
    } else {
        doc.ladder.rungs.clone()
    };
    let head: String = rungs
        .iter()
        .map(|r| {
            format!(
                r#"<th>{}<div class="rel-meta">{}</div></th>"#,
                esc(&r.name),
                esc(&r.goatnet)
            )
        })
        .collect();
    let hop = match &doc.ladder.hop {
        Some(h) => {
            let gates: Vec<String> = h
                .gates
                .iter()
                .map(|g| match g.min_days {
                    Some(d) => format!("{} ({d}d)", g.id),
                    None => g.id.clone(),
                })
                .collect();
            format!(
                r#"<p class="grounding">hop {} &rarr; {} gates: {}</p>"#,
                esc(&h.from_rung),
                esc(&h.to),
                esc(&gates.join(", "))
            )
        }
        None => r#"<p class="grounding">the document names no live hop — gate detail is unavailable</p>"#.into(),
    };
    let rows: String = doc.components.iter().map(|c| component_html(c, &rungs)).collect();
    format!(
        r#"<section class="panel">
<h2>release readiness <span class="dim">(repo vantage — the ladder as of the document below)</span></h2>
{}
{hop}
<table class="rows rel"><thead><tr><th>component</th><th>state</th>{head}</tr></thead>
<tbody>{rows}</tbody></table>
{}
</section>"#,
        provenance_html(doc, now_ms),
        unknowable_html(doc),
    )
}

pub fn render_refusal(err: &str) -> String {
    format!(
        r#"<section class="panel refusal"><h2>release readiness — REFUSED</h2>
<p class="refusal-text">{}</p>
<p class="grounding">This page renders no ladder state it cannot read. An empty table would say
"nothing to release", which is a different claim.</p></section>"#,
        esc(err)
    )
}

pub fn render_page(doc: &Result<ReleaseDoc, String>, now_ms: i64, version: &str) -> String {
    let body = match doc {
        Ok(d) => render_body(d, now_ms),
        Err(e) => render_refusal(e),
    };
    format!(
        r#"<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>release readiness</title>
<style>{STYLE}</style>
</head>
<body>
<header>
<h1>release readiness</h1>
<p class="tagline">what is merged, what is soaking, what is ready to graduate, and what blocks the rest
&mdash; ADR 1120's ladder, rendered. <a href="/">consistency &rarr;</a></p>
</header>
{body}
<footer>
<p class="dim">goat-consistency-console v{} &middot; ADR 1120 &middot; every grade, blocker and soak clock is
computed by <code>ops/release/graduate.py readiness</code> — the same code path <code>graduate</code>
refuses on. This page renders them and computes nothing.</p>
</footer>
</body>
</html>"#,
        esc(version)
    )
}

#[cfg(test)]
mod tests;
