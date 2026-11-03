// SPDX-License-Identifier: Apache-2.0
//
// api.rs — the aggregate state as structured, self-dating JSON
// (consistency-foundation.md §7.8 as-built: the hosted-variant API).
//
// The same honesty grammar as the page, machine-shaped:
//   - every grade travels WITH its why sentence and its grounding (counts
//     with denominators, ages, serials) — never a bare colour;
//   - an unreachable goatnet's entry says so, with the age of its last
//     reading — `reachability.state` is `never-heard` / `stale` / `live`,
//     and the entry is never omitted;
//   - the cross-env section is the SAME `goat_consistency::diff_intents`
//     report the page renders — one computation, two projections;
//   - the document dates itself (`generated_unix_ms`) and versions its shape
//     (`schema: "aggregate-v1"`), so a consumer can tell a stale capture
//     from a current one and an old shape from a new one.
//
// Everything here is a PURE projection of the same inputs the renderer gets.
// No verdict originates in this file: grades, whys and remediation pointers
// come off the wire or out of the shared library verbatim.

use std::collections::BTreeMap;

use goat_consistency::{diff_intents, EnvIntent};
use goat_consistency_wire::NodeConsistencyV1;
use serde_json::{json, Value};

use crate::render::{collapse_grades, detail_of, reach_of, Reach, View};

pub const SCHEMA: &str = "aggregate-v1";

/// One goatnet's API input — the render View plus the identity the page
/// carries in its own layout.
pub struct ApiEnv<'a> {
    pub name: &'a str,
    pub endpoint: &'a str,
    pub view: View<'a>,
}

fn ms_to_s(ms: i64) -> i64 {
    ms / 1000
}

fn remediation_json(kind: &str, pointer: &str, refuses: bool) -> Value {
    if pointer.is_empty() {
        Value::Null
    } else {
        json!({ "kind": kind, "pointer": pointer, "refuses": refuses })
    }
}

fn node_json(node: &NodeConsistencyV1) -> Value {
    let rows: Vec<Value> = node
        .rows
        .iter()
        .map(|r| {
            json!({
                "subject": r.subject,
                "name": r.name,
                "grade": r.grade,
                "color": r.color,
                "why": r.why,
                "observed": r.observed,
                "expected": r.expected,
                "remediation": remediation_json(&r.remediation_kind, &r.remediation, r.remediation_refuses),
                // ADR 1125. Empty from a pre-1124 publisher, which is why the
                // field is served as-is rather than defaulted to "observed":
                // "this publisher does not say" and "this publisher says it is
                // a real comparison" are different claims.
                "coverage": r.coverage,
            })
        })
        .collect();
    json!({
        "host": node.host_id,
        "member_group": node.member_group,
        "grade": node.grade,
        "color": node.color,
        "generation": {
            "serial": node.intent_serial,
            "behind_by": node.behind_by,
            "intent_age_s": if node.intent_age_seconds >= 0 { Value::from(node.intent_age_seconds) } else { Value::Null },
        },
        "report_age_s": ms_to_s(node.report_age_ms),
        "intent_verified": node.intent_verified,
        "counts": {
            "components_observed": node.components_observed,
            "components_governed": node.components_governed,
            "renders_observed": node.renders_observed,
            "renders_declared": node.renders_declared,
            // Rows that are not comparisons. Beside the grade, never inside it.
            "coverage_gaps": node.coverage_gaps,
        },
        "rows": rows,
    })
}

/// One goatnet's entry. Present for every CONFIGURED goatnet, whatever its
/// reachability — honest absence is a stated state, never a missing key.
pub fn goatnet_doc(e: &ApiEnv) -> Value {
    let v = &e.view;
    let (reach_state, last_heard_age_s) = match reach_of(v) {
        Reach::NeverHeard => ("never-heard", Value::Null),
        Reach::Stale { age_ms } => ("stale", Value::from(ms_to_s(age_ms))),
        Reach::Live { age_ms } => ("live", Value::from(ms_to_s(age_ms))),
    };
    let (intent_doc, refusal) = v.intent;
    let intent = json!({
        "verified": intent_doc.is_some(),
        "note": refusal,
        "serial": intent_doc.as_ref().and_then(|d| d.get("serial")).cloned().unwrap_or(Value::Null),
        "content_hash": intent_doc.as_ref().and_then(|d| d.get("content_hash")).cloned().unwrap_or(Value::Null),
        "generated_at": intent_doc.as_ref().and_then(|d| d.get("generated_at")).cloned().unwrap_or(Value::Null),
        "goatnet": intent_doc.as_ref().and_then(|d| d.get("goatnet")).cloned().unwrap_or(Value::Null),
    });

    let convergence = match v.latest.rollups.get(v.chosen_site) {
        None => Value::Null,
        Some(stored) => {
            let env = &stored.env;
            match detail_of(env) {
                None => json!({
                    "note": "an envelope arrived on this scope with no consistency arm — read, not evaluable; nothing is graded from it",
                    "overall_color": env.overall_color,
                    "published_unix_ms": env.ts_unix_ms,
                    "received_unix_ms": stored.received_unix_ms,
                }),
                Some(d) => {
                    let c = collapse_grades(d, v.grammar);
                    let mut by_grade: BTreeMap<String, usize> = BTreeMap::new();
                    for n in &d.nodes {
                        *by_grade.entry(n.grade.clone()).or_insert(0) += 1;
                    }
                    let worst = match &c.worst_grounded {
                        Some((grade, color)) => json!({ "grade": grade, "color": color }),
                        None => Value::Null,
                    };
                    json!({
                        "published_unix_ms": env.ts_unix_ms,
                        "received_unix_ms": stored.received_unix_ms,
                        "window_secs": env.window_secs,
                        "overall_color": env.overall_color,
                        "current_serial": d.current_serial,
                        "current_content_hash": d.current_content_hash,
                        "worst_grounded_grade": worst,
                        "counts": {
                            "nodes_reporting": d.nodes_reporting,
                            "nodes_expected": d.nodes_expected,
                            "nodes_grounded": c.grounded_n,
                            "nodes_ungrounded": c.ungrounded_n,
                            "by_grade": by_grade,
                            "skew_band_generations": d.skew_band_generations,
                            "skew_band_seconds": d.skew_band_seconds,
                        },
                        // Which governed members nobody reported for, by name —
                        // the machine half of the same statement (ADR 1125).
                        "unreported_members": d.unreported_members,
                        "meter_refusal": if d.intent_readable { Value::Null } else { Value::from(d.intent_note.clone()) },
                        "nodes": d.nodes.iter().map(node_json).collect::<Vec<_>>(),
                    })
                }
            }
        }
    };

    json!({
        "goatnet": e.name,
        "site": v.chosen_site,
        "endpoint": e.endpoint,
        "reachability": {
            "state": reach_state,
            "last_heard_age_s": last_heard_age_s,
            "bus": if v.latest.bus_note.is_empty() { "session not yet opened" } else { &v.latest.bus_note },
            "note": match reach_state {
                "never-heard" => format!(
                    "nothing heard on ops/consistency/health/{} — unreachable, not deployed, or silently denied; never green, never omitted",
                    v.chosen_site
                ),
                "stale" => "reading older than the staleness window — history, not current state".to_string(),
                _ => String::new(),
            },
        },
        "intent": intent,
        "convergence": convergence,
    })
}

/// The cross-env section — the same `diff_intents` report the drift panel
/// renders, with the cells VERBATIM from the artifacts.
pub fn cross_env_doc(envs: &[ApiEnv], now_ms: i64) -> Value {
    let verified: Vec<(&str, &Value)> = envs
        .iter()
        .filter_map(|e| e.view.intent.0.as_ref().map(|doc| (e.name, doc)))
        .collect();
    let not_compared: Vec<Value> = envs
        .iter()
        .filter(|e| e.view.intent.0.is_none())
        .map(|e| json!({ "env": e.name, "reason": e.view.intent.1 }))
        .collect();

    if verified.len() < 2 {
        return json!({
            "comparable": false,
            "note": format!(
                "a cross-env comparison needs at least two goatnets with verified committed intent; {} of {} configured verified",
                verified.len(), envs.len()
            ),
            "envs": verified.iter().map(|(n, _)| *n).collect::<Vec<_>>(),
            "not_compared": not_compared,
        });
    }
    let grammar = envs[0].view.grammar;
    let inputs: Vec<EnvIntent> =
        verified.iter().map(|(name, doc)| EnvIntent { env: name, intent: doc }).collect();
    let today = goat_consistency::utc_date(now_ms);
    let report = match diff_intents(&inputs, grammar, Some(&today)) {
        Ok(r) => r,
        Err(e) => {
            return json!({
                "comparable": false,
                "note": format!("the grading library refused: {e}"),
                "envs": verified.iter().map(|(n, _)| *n).collect::<Vec<_>>(),
                "not_compared": not_compared,
            })
        }
    };
    let rows: Vec<Value> = report
        .rows
        .iter()
        .map(|row| {
            let color = grammar.color(&row.grade, false).unwrap_or_else(|_| "idle".into());
            let (kind, ptr, refuses) = row
                .remediation
                .as_ref()
                .map(|r| (r.kind.as_str(), r.pointer.as_str(), r.refuses))
                .unwrap_or(("", "", false));
            json!({
                "subject": row.subject,
                "name": row.name,
                "grade": row.grade,
                "color": color,
                "why": row.why,
                "remediation": remediation_json(kind, ptr, refuses),
                "cells": row.cells,
            })
        })
        .collect();
    // WHICH generation of each artifact this verdict was computed FOR. Without
    // it the comparison is undatable by its reader: this console serves the
    // intent baked into its image at deploy time, so a page can be rendering a
    // two-day-old generation and look exactly like a current one (observed
    // the hosted aggregator was showing zagros serial 51 while the
    // tree was on 67). Same idea as a Kubernetes controller's
    // `observedGeneration`: a status is only readable next to the spec
    // revision it was computed from.
    let observed: Vec<Value> = verified
        .iter()
        .map(|(name, doc)| {
            let gen_at = doc.get("generated_at").and_then(|v| v.as_str());
            let age_s = gen_at
                .and_then(goat_consistency::parse_utc_stamp)
                .map(|t| now_ms / 1000 - t);
            json!({
                "env": name,
                "serial": doc.get("serial").cloned().unwrap_or(Value::Null),
                "content_hash": doc.get("content_hash").cloned().unwrap_or(Value::Null),
                "generated_at": gen_at,
                "age_s": age_s,
            })
        })
        .collect();
    json!({
        "comparable": true,
        "envs": report.envs,
        "observed_generations": observed,
        "dated_for": today,
        "summary": {
            "rows_compared": report.rows.len(),
            "by_grade": report.by_grade,
            "undeclared_drift": report.undeclared(),
        },
        "rows": rows,
        "not_compared": not_compared,
    })
}

/// The whole `/api/aggregate` document.
pub fn aggregate_doc(envs: &[ApiEnv], mode: &str, version: &str, now_ms: i64) -> Value {
    json!({
        "schema": SCHEMA,
        "service": "consistency-console",
        "adr": "1110",
        "version": version,
        "mode": mode,
        "generated_unix_ms": now_ms,
        "cross_env": cross_env_doc(envs, now_ms),
        "goatnets": envs.iter().map(goatnet_doc).collect::<Vec<_>>(),
    })
}
