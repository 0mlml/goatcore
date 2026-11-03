#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# test_corpus.py — the Python implementation's run of the shared golden corpus.
#
# The corpus (ops/consistency/testdata/) is the conformance contract, not this
# file: this is one of the two implementations that must pass it. The Rust
# implementation (CF-3's rollup meter, CF-6's console) reads the same bytes and
# runs the same expectations, which is what makes "one grammar, two executions"
# a guarantee rather than a hope. Canon:
# docs/design/consistency-foundation.md §4.3 (ADR 1110 D2).
#
# Plain python3 + asserts, no pytest — the ops/operators/test_*.py convention,
# so CI runs it with `python3 <file>` and nothing else has to be installed.
#
# Exit codes: 0 all cases pass, 1 one or more failed.

import datetime
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.normpath(os.path.join(HERE, "..")))

import yaml  # noqa: E402

from goat_consistency import grade_intent, grading, grammar as grammar_mod  # noqa: E402
from goat_consistency import intent as intent_mod  # noqa: E402
from goat_consistency.paths import TESTDATA_DIR  # noqa: E402

CASES = os.path.join(TESTDATA_DIR, "cases")
GENERATIONS = os.path.join(TESTDATA_DIR, "generations")

FAILURES = []


def check(case_name, what, actual, expected):
    if actual != expected:
        FAILURES.append("%s: %s\n    expected: %r\n    actual:   %r"
                        % (case_name, what, expected, actual))
        return False
    return True


def load_yaml(path):
    with open(path) as f:
        return yaml.safe_load(f) or []


def require_why(case):
    """Every case says what it pins. A case that cannot is a case nobody will
    maintain, and an unmaintained corpus is worse than none — it reads as
    coverage."""
    if not str(case.get("why_this_case") or "").strip():
        FAILURES.append("%s: has no `why_this_case:`" % case.get("name"))


def run_precedence(g):
    cases = load_yaml(os.path.join(CASES, "precedence.yaml"))
    for case in cases:
        require_why(case)
        check(case["name"], "worst(%s)" % case["grades"],
              g.worst(case["grades"]), case["expect"])
    return len(cases)


def run_rows(g):
    cases = load_yaml(os.path.join(CASES, "rows.yaml"))
    for case in cases:
        require_why(case)
        row = grading.grade_row(
            case["subject"], case["row_name"], case.get("cells") or {},
            case.get("goatnet"), vantage=case.get("vantage", "repo"),
            declared=bool(case.get("declared")),
            artifact_type=case.get("artifact_type"), grammar=g)
        expect = case["expect"]
        name = case["name"]
        check(name, "grade", row.grade, expect["grade"])
        check(name, "why", row.why, expect["why"])
        check(name, "grounding.n", row.grounding["n"], expect["grounding"]["n"])
        check(name, "grounding.vantage", row.grounding["vantage"],
              expect["grounding"]["vantage"])
        # The window is always carried, even when there isn't one: a reading
        # whose window is unstated is a reading a renderer will guess at.
        check(name, "grounding.window_seconds", row.grounding["window_seconds"], None)
        check(name, "remediation", row.remediation, expect["remediation"])

        # And the rule the grammar states about itself: exactly the grades that
        # declare they need a pointer carry one.
        check(name, "remediation presence matches grammar",
              row.remediation is not None, g.needs_remediation(row.grade))
    return len(cases)


def run_accountability(g):
    """The SKEW-vs-unaccountable distinction, computed from a reader's inputs
    only: one committed generation plus one observed content hash, no git tree.
    That constraint is the case set's whole value (design doc §3.3a) — a case
    that reached for history outside the artifact would prove nothing about the
    readers that have none."""
    cases = load_yaml(os.path.join(CASES, "accountability.yaml"))
    for case in cases:
        require_why(case)
        name = case["name"]
        with open(os.path.join(GENERATIONS, case["generation"] + ".intent.json")) as f:
            doc = json.load(f)
        now = datetime.datetime.strptime(case["now"], "%Y-%m-%dT%H:%M:%SZ").replace(
            tzinfo=datetime.timezone.utc)
        cells = {node: grading.account_for(doc, observed, now=now)
                 for node, observed in (case["observed"] or {}).items()}
        check(name, "cells", cells, case["expect"]["cells"])
        check(name, "grade", grading.grade_cells(cells.values(), grammar=g),
              case["expect"]["grade"])
    return len(cases)


def run_renders(g):
    """The CF-4 render classification, computed from a node's own facts only:
    one render record — the file's hash now against what the last converge
    recorded. No generation appears in these inputs on purpose: a per-host
    render is a function of host vars no repo-side reader may evaluate, so the
    converge-time record is the only honest baseline a reader has."""
    cases = load_yaml(os.path.join(CASES, "renders.yaml"))
    for case in cases:
        require_why(case)
        name = case["name"]
        cell = grading.account_for_render(case["record"] or {})
        check(name, "cell", cell, case["expect"]["cell"])
        check(name, "grade", grading.grade_cells([cell], grammar=g),
              case["expect"]["grade"])
    return len(cases)


def run_generations(g):
    count = 0
    for fixture in sorted(f for f in os.listdir(GENERATIONS) if f.endswith(".intent.json")):
        stem = fixture[: -len(".intent.json")]
        path = os.path.join(GENERATIONS, fixture)
        with open(path) as f:
            doc = json.load(f)
        with open(os.path.join(GENERATIONS, stem + ".expected.json")) as f:
            expected = json.load(f)

        # The fixture is a real IntentV1 document or it pins nothing.
        intent_mod.validate_schema(doc, "corpus fixture %s" % fixture)

        # Cross-language contract: the canonical serialization and the hash over
        # it must reproduce the committed content_hash EXACTLY. If the Rust
        # implementation serializes differently, this is the case that says so —
        # and it matters because CF-5 signs those exact bytes.
        check(stem, "content_hash reproduces from the body",
              intent_mod.content_hash(doc), doc["content_hash"])

        rows = [
            {"subject": r.subject, "name": r.name, "grade": r.grade, "why": r.why,
             "remediation": r.remediation, "grounding": r.grounding}
            for r in grade_intent(doc, grammar=g)
        ]
        check(stem, "graded rows", rows, expected["rows"])
        count += 1
    return count


def main():
    g = grammar_mod.load()
    n_prec = run_precedence(g)
    n_rows = run_rows(g)
    n_acct = run_accountability(g)
    n_rend = run_renders(g)
    n_gens = run_generations(g)

    if FAILURES:
        print("FAIL: %d corpus expectation(s) not met\n" % len(FAILURES))
        for f in FAILURES:
            print("  " + f)
        print("\nThe corpus is the contract (ops/consistency/testdata/). If the new")
        print("behaviour is correct, the fix is a corpus diff a reviewer can read —")
        print("not a quieter assertion.")
        return 1

    print("ok: grammar v%s — %d precedence case(s), %d row case(s), "
          "%d accountability case(s), %d render case(s), %d generation(s); "
          "%d grade(s) defined, all with a colour and a remediation rule."
          % (g.version, n_prec, n_rows, n_acct, n_rend, n_gens, len(g.grades)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
