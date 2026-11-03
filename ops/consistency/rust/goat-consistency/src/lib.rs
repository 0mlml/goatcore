// SPDX-License-Identifier: Apache-2.0
//
// goat-consistency — the consistency grading grammar, executed in Rust.
//
// The SECOND of the two implementations ADR 1110 D2 requires. The grammar is
// DATA (ops/consistency/grammar.yaml); this crate executes it, the Python
// package ops/consistency/python/goat_consistency/ executes it, and NEITHER
// defines a grade. What keeps that from being a promise is
// ops/consistency/testdata/ — the shared golden corpus, run by
// ops/consistency/python/tests/test_corpus.py on one side and
// goat-consistency/tests/corpus.rs on the other, over the same bytes with the
// same expectations. A grade whose behaviour is not pinned by a corpus case is
// not part of the grammar.
//
// Why Rust exists here at all: the rollup meter must compute `overall_color` to
// publish an EnvHealthV1, and the CF-6 console renders live records — both are
// long-running processes holding a Zenoh mTLS session, which is Rust by
// ADR 0112's decision test and by the panoscope precedent. Pretending one
// implementation could serve both languages would be false; so would letting
// two implementations each define the grammar.
//
// Canon: docs/design/consistency-foundation.md §4 (ADR 1110 D2/D4).

pub mod diff;
pub mod grading;
pub mod grammar;
pub mod intent;
pub mod project;
pub mod verify;

pub use diff::{diff_intents, CrossEnvReport, EnvIntent};
pub use grading::{account_for, account_for_render, grade_cells, grade_intent, grade_row, GradedRow};
pub use grammar::{Grammar, GrammarError, Remediation};
pub use intent::{parse_utc_stamp, utc_date};
pub use project::{
    project_goatnet, project_node, GoatnetProjection, NodeProjection, ObservedComponent,
    ObservedNode, ObservedRender, ProjectedRow,
};
