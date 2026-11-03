// SPDX-License-Identifier: Apache-2.0
//
// goat-consistency-wire — the ConsistencyV1 wire types.
//
// Hand-written prost-derive structs, no protoc and no build.rs — the convention
// every sibling env-health meter follows. Keep field tags in LOCK-STEP with the
// canonical schema at
// ops/zenoh-trial-peers/time-health/proto/env_health_v1.proto (arm 15).
//
// The crate declares only its OWN arm on the envelope oneof (`tags = "15"`),
// exactly as node-net-health declares only 14: a meter does not need to decode
// domains it never reads, and carrying the whole oneof in every crate is how
// five copies of a schema drift.
//
// A crate rather than a module since CF-6: the rollup meter WRITES these
// messages and the console READS them, and two hand-written copies of the same
// field tags inside one workspace is exactly the drift the lock-step comment
// above warns about — so there is one copy, and both binaries link it.

use prost::Message as _;

// -- ops/consistency/report/<host_id> ---------------------------------------

#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct ComponentStateV1 {
    #[prost(string, tag = "1")]
    pub component: String,
    #[prost(string, tag = "2")]
    pub artifact_type: String,
    #[prost(string, tag = "3")]
    pub r#ref: String,
    #[prost(string, tag = "4")]
    pub prior_ref: String,
    #[prost(int64, tag = "5")]
    pub ts_unix_ms: i64,
    #[prost(bool, tag = "6")]
    pub reversible: bool,
    #[prost(string, tag = "7")]
    pub reconciled: String,
    #[prost(string, tag = "8")]
    pub live_ref: String,
}

/// One watched config render (CF-4): the file's hash at collection time
/// against the hash the last converge recorded after rendering it. FACTS —
/// the comparison happens at the reader, never here.
#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct RenderStateV1 {
    #[prost(string, tag = "1")]
    pub component: String,
    #[prost(string, tag = "2")]
    pub path: String,
    /// The file's hash NOW; "" when it could not be hashed (see `note`).
    #[prost(string, tag = "3")]
    pub sha256: String,
    /// What the last converge recorded after rendering; "" when the deploying
    /// role could not hash the declared file at converge time.
    #[prost(string, tag = "4")]
    pub recorded_sha256: String,
    /// mtime of the render manifest — when the recorded baseline last changed.
    #[prost(int64, tag = "5")]
    pub recorded_at_unix_ms: i64,
    /// Why a hash is empty: "missing" | "unreadable" | a converge-time reason.
    #[prost(string, tag = "6")]
    pub note: String,
}

#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct ConsistencyReportV1 {
    #[prost(string, tag = "1")]
    pub host_id: String,
    #[prost(string, tag = "2")]
    pub goatnet: String,
    #[prost(string, tag = "3")]
    pub member_group: String,
    #[prost(string, tag = "4")]
    pub intent_content_hash: String,
    #[prost(uint64, tag = "5")]
    pub intent_serial: u64,
    #[prost(int64, tag = "6")]
    pub intent_generated_at_unix_ms: i64,
    #[prost(string, tag = "7")]
    pub intent_commit: String,
    #[prost(int64, tag = "8")]
    pub applied_at_unix_ms: i64,
    #[prost(bool, tag = "9")]
    pub intent_verified: bool,
    #[prost(message, repeated, tag = "10")]
    pub components: Vec<ComponentStateV1>,
    #[prost(message, repeated, tag = "11")]
    pub renders: Vec<RenderStateV1>,
    #[prost(int64, tag = "12")]
    pub collected_at_unix_ms: i64,
    #[prost(string, tag = "13")]
    pub collector_note: String,
}

// -- ops/consistency/health/<site> ------------------------------------------

#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct ConsistencyRowV1 {
    #[prost(string, tag = "1")]
    pub subject: String,
    #[prost(string, tag = "2")]
    pub name: String,
    #[prost(string, tag = "3")]
    pub artifact_type: String,
    #[prost(string, tag = "4")]
    pub grade: String,
    #[prost(string, tag = "5")]
    pub color: String,
    #[prost(string, tag = "6")]
    pub why: String,
    #[prost(string, tag = "7")]
    pub observed: String,
    #[prost(string, tag = "8")]
    pub expected: String,
    #[prost(string, tag = "9")]
    pub remediation_kind: String,
    #[prost(string, tag = "10")]
    pub remediation: String,
    #[prost(bool, tag = "11")]
    pub remediation_refuses: bool,
    /// ADR 1125: whether a comparison was POSSIBLE, orthogonal to `grade` —
    /// observed|no-instrument|silent|unreported-scope. Additive, so a row
    /// from an older publisher decodes with `""` and reads as the pre-1124
    /// shape rather than as an invented state.
    #[prost(string, tag = "12")]
    pub coverage: String,
}

#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct NodeConsistencyV1 {
    #[prost(string, tag = "1")]
    pub host_id: String,
    #[prost(string, tag = "2")]
    pub member_group: String,
    #[prost(string, tag = "3")]
    pub color: String,
    #[prost(string, tag = "4")]
    pub grade: String,
    #[prost(uint64, tag = "5")]
    pub intent_serial: u64,
    #[prost(string, tag = "6")]
    pub intent_content_hash: String,
    #[prost(uint32, tag = "7")]
    pub behind_by: u32,
    #[prost(int64, tag = "8")]
    pub intent_age_seconds: i64,
    #[prost(int64, tag = "9")]
    pub report_age_ms: i64,
    #[prost(bool, tag = "10")]
    pub intent_verified: bool,
    #[prost(message, repeated, tag = "11")]
    pub rows: Vec<ConsistencyRowV1>,
    #[prost(uint32, tag = "12")]
    pub components_governed: u32,
    #[prost(uint32, tag = "13")]
    pub components_observed: u32,
    /// CF-4 grounding: declared watch paths vs recorded renders — counts with
    /// denominators, so a declared-but-unrecorded watch-set stays a number.
    #[prost(uint32, tag = "14")]
    pub renders_declared: u32,
    #[prost(uint32, tag = "15")]
    pub renders_observed: u32,
    /// ADR 1125: rows that are coverage gaps rather than comparisons. Carried
    /// beside `grade`, never folded into it.
    #[prost(uint32, tag = "16")]
    pub coverage_gaps: u32,
}

#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct ConsistencyDetailV1 {
    #[prost(message, repeated, tag = "1")]
    pub nodes: Vec<NodeConsistencyV1>,
    #[prost(string, tag = "2")]
    pub goatnet: String,
    #[prost(uint64, tag = "3")]
    pub current_serial: u64,
    #[prost(string, tag = "4")]
    pub current_content_hash: String,
    #[prost(uint32, tag = "5")]
    pub nodes_expected: u32,
    #[prost(uint32, tag = "6")]
    pub nodes_reporting: u32,
    #[prost(uint32, tag = "7")]
    pub skew_band_generations: u32,
    #[prost(int64, tag = "8")]
    pub skew_band_seconds: i64,
    #[prost(bool, tag = "9")]
    pub intent_readable: bool,
    #[prost(string, tag = "10")]
    pub intent_note: String,
    /// ADR 1125 `unreported-scope`: governed members no node claimed, BY NAME.
    /// A count says how many are missing and never which one.
    #[prost(string, repeated, tag = "11")]
    pub unreported_members: Vec<String>,
}

#[derive(Clone, PartialEq, prost::Message, serde::Serialize)]
pub struct EnvHealthV1 {
    #[prost(string, tag = "1")]
    pub domain: String,
    #[prost(string, tag = "2")]
    pub scope: String,
    #[prost(int64, tag = "3")]
    pub ts_unix_ms: i64,
    #[prost(string, tag = "4")]
    pub overall_color: String,
    #[prost(uint64, tag = "5")]
    pub sample_count: u64,
    #[prost(uint32, tag = "6")]
    pub window_secs: u32,
    #[prost(oneof = "EnvDetail", tags = "15")]
    pub detail: Option<EnvDetail>,
}

#[derive(Clone, PartialEq, prost::Oneof, serde::Serialize)]
pub enum EnvDetail {
    #[prost(message, tag = "15")]
    Consistency(ConsistencyDetailV1),
}

/// Decode a report off the bus, or None. A payload that is not a
/// ConsistencyReportV1 is dropped rather than partially believed.
pub fn decode_report(bytes: &[u8]) -> Option<ConsistencyReportV1> {
    ConsistencyReportV1::decode(bytes).ok()
}

/// Decode a rolled envelope off the bus, or None — the console's read side.
/// Same posture: an undecodable payload is dropped rather than partially
/// believed.
pub fn decode_health(bytes: &[u8]) -> Option<EnvHealthV1> {
    EnvHealthV1::decode(bytes).ok()
}
