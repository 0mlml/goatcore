// SPDX-License-Identifier: Apache-2.0
//
// corpus.rs — the Rust implementation's run of the SHARED golden corpus.
//
// The corpus (ops/consistency/testdata/) is the conformance contract, not this
// file: this is the second of the two implementations that must pass it. The
// Python implementation reads the same bytes and runs the same expectations
// (ops/consistency/python/tests/test_corpus.py), which is what makes "one
// grammar, two executions" a guarantee rather than a hope.
//
// It reads the corpus from disk on purpose, unlike the grammar, which is
// compiled in: the grammar has to travel inside a container, and the corpus
// never leaves the repo. If this test cannot find ops/consistency/testdata/ it
// FAILS rather than skipping — a conformance test that silently passes when it
// cannot find the contract is worse than no test, because it reads as coverage.
//
// Canon: docs/design/consistency-foundation.md §4.3 (ADR 1110 D2).

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use goat_consistency::grading::{account_for, account_for_render, grade_cells, grade_intent, grade_row};
use goat_consistency::intent::content_hash;
use goat_consistency::Grammar;
use serde_json::Value;

fn testdata() -> PathBuf {
    // tests/ -> goat-consistency/ -> rust/ -> consistency/ -> testdata/
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(|p| p.parent())
        .expect("the crate must sit at ops/consistency/rust/goat-consistency")
        .join("testdata")
}

fn read(path: &Path) -> String {
    std::fs::read_to_string(path)
        .unwrap_or_else(|e| panic!("the corpus IS the contract — cannot read {}: {e}", path.display()))
}

/// YAML in, `serde_json::Value` out, so a corpus expectation and a computed
/// answer are compared as one value rather than field by field.
fn yaml_value(path: &Path) -> Value {
    let y: serde_yaml::Value = serde_yaml::from_str(&read(path)).expect("corpus YAML parses");
    serde_json::to_value(y).expect("corpus YAML is JSON-shaped")
}

fn json_value(path: &Path) -> Value {
    serde_json::from_str(&read(path)).expect("corpus JSON parses")
}

fn cases(name: &str) -> Vec<Value> {
    match yaml_value(&testdata().join("cases").join(name)) {
        Value::Array(a) => a,
        _ => panic!("{name} must be a list of cases"),
    }
}

fn s(v: &Value, key: &str) -> String {
    v.get(key).and_then(|x| x.as_str()).unwrap_or("").to_string()
}

/// Every case says what it pins. A case that cannot is a case nobody will
/// maintain, and an unmaintained corpus reads as coverage.
fn require_why(case: &Value) {
    assert!(
        !s(case, "why_this_case").trim().is_empty(),
        "{}: has no `why_this_case:`",
        s(case, "name")
    );
}

fn parse_now(text: &str) -> i64 {
    goat_consistency::intent::parse_utc_stamp(text)
        .unwrap_or_else(|| panic!("corpus `now:` must be the artifact's one stamp format: {text}"))
}

#[test]
fn precedence_cases() {
    let g = Grammar::load().unwrap();
    let cases = cases("precedence.yaml");
    assert!(!cases.is_empty(), "precedence.yaml pins nothing");
    for case in &cases {
        require_why(case);
        let grades: Vec<String> = case["grades"]
            .as_array()
            .expect("grades is a list")
            .iter()
            .map(|v| v.as_str().unwrap().to_string())
            .collect();
        assert_eq!(
            g.worst(&grades).unwrap(),
            s(case, "expect"),
            "{}: worst({grades:?})",
            s(case, "name")
        );
    }
}

#[test]
fn row_cases() {
    let g = Grammar::load().unwrap();
    let cases = cases("rows.yaml");
    assert!(!cases.is_empty(), "rows.yaml pins nothing");
    for case in &cases {
        require_why(case);
        let name = s(case, "name");
        let cells: BTreeMap<String, Value> = case
            .get("cells")
            .and_then(|v| v.as_object())
            .map(|o| o.iter().map(|(k, v)| (k.clone(), v.clone())).collect())
            .unwrap_or_default();
        let vantage = case.get("vantage").and_then(|v| v.as_str()).unwrap_or("repo");
        let row = grade_row(
            &s(case, "subject"),
            &s(case, "row_name"),
            cells,
            case.get("goatnet").and_then(|v| v.as_str()),
            vantage,
            case.get("declared").and_then(|v| v.as_bool()).unwrap_or(false),
            case.get("artifact_type").and_then(|v| v.as_str()),
            &g,
        )
        .unwrap_or_else(|e| panic!("{name}: {e}"));
        let expect = &case["expect"];

        assert_eq!(row.grade, s(expect, "grade"), "{name}: grade");
        assert_eq!(row.why, s(expect, "why"), "{name}: why");
        assert_eq!(
            Value::from(row.n),
            expect["grounding"]["n"],
            "{name}: grounding.n"
        );
        assert_eq!(
            row.vantage,
            s(&expect["grounding"], "vantage"),
            "{name}: grounding.vantage"
        );
        let actual_rem = match &row.remediation {
            None => Value::Null,
            Some(r) => serde_json::to_value(r).unwrap(),
        };
        assert_eq!(actual_rem, expect["remediation"], "{name}: remediation");

        // And the rule the grammar states about itself: exactly the grades that
        // declare they need a pointer carry one.
        assert_eq!(
            row.remediation.is_some(),
            g.needs_remediation(&row.grade).unwrap(),
            "{name}: remediation presence matches grammar"
        );
    }
}

/// The SKEW-vs-unaccountable distinction, computed from a reader's inputs only:
/// one committed generation plus one observed content hash, no git tree. That
/// constraint is the case set's whole value (§3.3a) — a case that reached for
/// history outside the artifact would prove nothing about the readers that have
/// none, which is every reader this crate exists to serve.
#[test]
fn accountability_cases() {
    let g = Grammar::load().unwrap();
    let cases = cases("accountability.yaml");
    assert!(!cases.is_empty(), "accountability.yaml pins nothing");
    for case in &cases {
        require_why(case);
        let name = s(case, "name");
        let doc = json_value(
            &testdata()
                .join("generations")
                .join(format!("{}.intent.json", s(case, "generation"))),
        );
        let now = parse_now(&s(case, "now"));
        let observed = case["observed"].as_object().cloned().unwrap_or_default();
        let mut cells = serde_json::Map::new();
        for (node, hash) in &observed {
            cells.insert(node.clone(), account_for(&doc, hash.as_str().unwrap_or(""), now, None));
        }
        assert_eq!(Value::Object(cells.clone()), case["expect"]["cells"], "{name}: cells");
        let values: Vec<&Value> = cells.values().collect();
        assert_eq!(
            grade_cells(values, false, &g).unwrap(),
            s(&case["expect"], "grade"),
            "{name}: grade"
        );
    }
}

/// The CF-4 render classification, computed from a node's own facts only: one
/// render record — the file's hash now against what the last converge
/// recorded. No generation appears in these inputs on purpose: a per-host
/// render is a function of host vars no repo-side reader may evaluate, so the
/// converge-time record is the only honest baseline a reader has.
#[test]
fn render_cases() {
    let g = Grammar::load().unwrap();
    let cases = cases("renders.yaml");
    assert!(!cases.is_empty(), "renders.yaml pins nothing");
    for case in &cases {
        require_why(case);
        let name = s(case, "name");
        let cell = account_for_render(&case["record"]);
        assert_eq!(cell, case["expect"]["cell"], "{name}: cell");
        assert_eq!(
            grade_cells([&cell], false, &g).unwrap(),
            s(&case["expect"], "grade"),
            "{name}: grade"
        );
    }
}

/// Whole committed generations: the canonical serialization, the hash over it,
/// and every graded row including its generated `why` and its remediation
/// pointer.
///
/// The `content_hash` assertion is the one with teeth across languages. CF-5
/// signs SHA-256 over those exact bytes; if Rust serialized the canonical body
/// even a space differently from Python, the two implementations would compute
/// different identities for the same document and every reader would grade
/// every node `unaccountable`. This is the case that says so.
#[test]
fn generation_fixtures() {
    let g = Grammar::load().unwrap();
    let dir = testdata().join("generations");
    let mut fixtures: Vec<String> = std::fs::read_dir(&dir)
        .unwrap_or_else(|e| panic!("cannot read {}: {e}", dir.display()))
        .filter_map(|e| e.ok())
        .map(|e| e.file_name().to_string_lossy().to_string())
        .filter(|n| n.ends_with(".intent.json"))
        .collect();
    fixtures.sort();
    assert!(!fixtures.is_empty(), "no generation fixtures — the corpus pins nothing");

    for fixture in fixtures {
        let stem = fixture.trim_end_matches(".intent.json").to_string();
        let doc = json_value(&dir.join(&fixture));
        let expected = json_value(&dir.join(format!("{stem}.expected.json")));

        assert_eq!(
            content_hash(&doc),
            s(&doc, "content_hash"),
            "{stem}: content_hash reproduces from the body"
        );

        let rows: Vec<Value> = grade_intent(&doc, "repo", &g)
            .unwrap_or_else(|e| panic!("{stem}: {e}"))
            .iter()
            .map(|r| r.as_corpus_json())
            .collect();
        assert_eq!(Value::Array(rows), expected["rows"], "{stem}: graded rows");
    }
}

/// The committed artifacts themselves are real IntentV1 documents whose hashes
/// reproduce. Not part of the shared corpus — the Python `--check` gate already
/// covers regeneration — but it is the cheapest possible guard against the Rust
/// canonical form drifting away from the files it will actually be pointed at
/// in production.
#[test]
fn committed_artifacts_hash_reproduces_in_rust() {
    let dir = testdata()
        .parent()
        .expect("testdata has a parent")
        .join("intent");
    let mut seen = 0;
    for entry in std::fs::read_dir(&dir).expect("ops/consistency/intent must exist") {
        let path = entry.expect("readable entry").path().join("intent.json");
        if !path.exists() {
            continue;
        }
        let doc = json_value(&path);
        assert_eq!(
            content_hash(&doc),
            s(&doc, "content_hash"),
            "{}: the Rust canonical form must reproduce the committed content_hash",
            path.display()
        );
        seen += 1;
    }
    assert!(seen >= 1, "no committed intent artifacts found under {}", dir.display());
}
