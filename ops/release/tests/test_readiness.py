#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# test_readiness.py — what the release-readiness document says when the tree
# cannot answer.
#
# The readiness document (graduate.py readiness, release-graduation.md §8) is rendered by
# the consistency console, which computes no verdict of its own. Everything
# interesting is therefore a DEGRADED case: a record whose `opened` date is
# unusable, a component with no ledger coverage, a pin with no `sha-` tag, a
# ledger whose org differs from the pin table's template. Each must come out
# as an explicit unknown, because the renderer turns unknown into a distinct
# visual state and turns a `false`/`0`/`""` into a measurement.
#
# The load-bearing assertions:
#   - `soak_unparseable_opened_is_unknown_not_zero` — a soak whose clock cannot
#     start must never render as "0 days elapsed", which reads as "just opened"
#     and is a countdown the tree cannot support.
#   - `ledger_joins_on_digest_across_org_rendering` — the pin table stores the
#     literal `{{ ghcr_org }}` and the ledger stores the resolved org, so a
#     whole-ref join silently reports every component as never-deployed. That
#     is the merged-but-not-deployed signal reading backwards.
#   - `blocked_names_every_failing_gate` — a blocked component must carry WHAT
#     blocks it, from the same gate_failures() `graduate` refuses on.
#   - `an_unreadable_paths_filter_is_unknown_not_zero_commits` — the
#     per-component "is there a build nothing pins?" reading comes from the
#     image workflow's `paths:` filter. A filter this tool will not evaluate
#     must never collapse to an empty path set, which counts every commit as
#     irrelevant and renders as "the deployed build is current".
#   - `a_waived_soak_is_its_own_state` — an operator-waived soak and a window
#     that has simply not elapsed are different facts and must not render the
#     same; a waiver naming nobody reads as unknown attribution.
#
# Plain python3 + asserts, no pytest — the ops/consistency/python/tests
# convention. Exit codes: 0 all pass, 1 one or more failed.

import datetime as dt
import importlib.util
import os
import re
import sys
import inspect
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
RELEASE = os.path.normpath(os.path.join(HERE, ".."))

spec = importlib.util.spec_from_file_location("graduate", os.path.join(RELEASE, "graduate.py"))
g = importlib.util.module_from_spec(spec)
spec.loader.exec_module(g)

TODAY = dt.date(2026, 8, 12)
HOP = {"from": "preprod", "to": "prod", "status": "live", "gates": [
    {"id": "candidate-record"}, {"id": "soak", "min_days": 2},
    {"id": "convergence-evidence"}, {"id": "validation-evidence"},
    {"id": "operator-ack"}]}
REF = "ghcr.io/{{ ghcr_org }}/goat-thing:sha-abc1234@sha256:" + "1" * 64
DEPLOYED_REF = "ghcr.io/usareur-af/goat-thing:sha-abc1234@sha256:" + "1" * 64


def record(**over):
    rec = {
        "component": "thing", "class": "ladder", "candidate_ref": REF,
        "opened": "2026-08-01", "reason": "why",
        "evidence": {"validation": "tests pinned", "preprod_convergence": "M-999"},
        "state": "candidate", "graduated": None, "operator_ack": "operator",
    }
    rec.update(over)
    return rec


FAILURES = []


def check(name, cond, detail=""):
    if cond:
        print(f"  ok   {name}")
    else:
        print(f"  FAIL {name} {detail}")
        FAILURES.append(name)


# --- soak -----------------------------------------------------------------

def soak_tests():
    print("soak clock")
    met = g.soak_facts(record(opened="2026-08-01"), HOP, TODAY)
    check("elapsed soak is met with its grounding",
          met["state"] == "met" and met["elapsed_days"] == 11
          and met["required_days"] == 2 and met["remaining_days"] == 0, met)

    soaking = g.soak_facts(record(opened="2026-08-11"), HOP, TODAY)
    check("mid-soak states elapsed and remaining, never a percentage",
          soaking["state"] == "soaking" and soaking["elapsed_days"] == 1
          and soaking["remaining_days"] == 1, soaking)

    unknown = g.soak_facts(record(opened=None), HOP, TODAY)
    check("soak_unparseable_opened_is_unknown_not_zero",
          unknown["state"] == "unknown" and "elapsed_days" not in unknown
          and unknown["required_days"] == 2 and "why" in unknown, unknown)

    nogate = g.soak_facts(record(), {"gates": [{"id": "candidate-record"}]}, TODAY)
    check("a ladder with no soak gate says so rather than inventing a window",
          nogate["state"] == "no-soak-gate" and nogate["required_days"] is None, nogate)

    # The clock is the record's `opened` date, and the document says so — the
    # schema carries no deployed-at field, so this is not a deploy clock.
    check("the soak clock names which date it counts from",
          g.soak_facts(record(), HOP, TODAY)["clock"] == "record `opened` date")


# --- fastlane debt ------------------------------------------------
def backfill_tests():
    print("fastlane backfill debt")
    comp = {"fleet": REF, "deviations": {}}
    live = [{"name": "preprod", "goatnet": "pre"}, {"name": "prod", "goatnet": "prd"}]
    hot = record(**{"class": "hotfix", "opened": "2026-08-10",
                    "evidence": {"validation": "", "preprod_convergence": ""}})
    hot["_path"] = g.RECORDS / "thing" / "20260810-hotfix-x.yaml"
    graded = g.grade_component("thing", comp, live, [hot], {}, set())
    check("an open hotfix on the ref every rung runs grades hotfix-backfill-owed, not aligned",
          graded["grade"] == "hotfix-backfill-owed", graded)
    none = g.grade_component("thing", comp, live, [], {}, set())
    check("the same rungs with no open hotfix grade aligned", none["grade"] == "aligned", none)
    done = dict(hot); done["state"] = "graduated"
    check("a graduated hotfix no longer owes anything",
          g.grade_component("thing", comp, live, [done], {}, set())["grade"] == "aligned")
    check("C9 has a cutoff and a max age, both named",
          isinstance(g.HOTFIX_BACKFILL_CUTOFF, dt.date) and g.HOTFIX_BACKFILL_MAX_DAYS >= 1)


# --- adjacent-hop grading (GH #1303) ---------------------------------------
def pair_tests():
    print("adjacent-hop grading")
    three = [{"name": "dev", "goatnet": "dev"}, {"name": "preprod", "goatnet": "pre"},
             {"name": "prod", "goatnet": "prd"}]
    two = three[1:]
    NEW = "ghcr.io/{{ ghcr_org }}/goat-thing:sha-new0000@sha256:" + "2" * 64
    cand = record(candidate_ref=NEW)
    cand["_path"] = g.RECORDS / "thing" / "20260812-new0000.yaml"

    mid = {"fleet": REF, "deviations": {"pre": {"value": NEW}}}
    graded = g.grade_component("thing", mid, three, [], {}, set())
    check("a deviation on the middle rung alone, with no record, grades UNDECLARED-SKEW",
          graded["grade"] == "UNDECLARED-SKEW", graded)
    check("the grade names the hops that skew",
          graded["pairs"] == {"dev->preprod": "UNDECLARED-SKEW", "preprod->prod": "UNDECLARED-SKEW"},
          graded["pairs"])
    check("the same middle-rung deviation with its candidate record grades candidate",
          g.grade_component("thing", mid, three, [cand], {}, set())["grade"] == "candidate")

    dev_only = {"fleet": REF, "deviations": {"dev": {"value": NEW}}}
    check("a dev-only deviation with its record grades candidate",
          g.grade_component("thing", dev_only, three, [cand], {}, set())["grade"] == "candidate")
    check("a dev-only deviation with no record grades UNDECLARED-SKEW on the dev hop only",
          g.grade_component("thing", dev_only, three, [], {}, set())["pairs"]
          == {"dev->preprod": "UNDECLARED-SKEW", "preprod->prod": "aligned"})

    dev_and_pre = {"fleet": REF, "deviations": {"dev": {"value": NEW}, "pre": {"value": NEW}}}
    check("a candidate that has reached preprod grades candidate on the prod hop and aligned on the dev hop",
          g.grade_component("thing", dev_and_pre, three, [cand], {}, set())["pairs"]
          == {"dev->preprod": "aligned", "preprod->prod": "candidate"})

    hot = record(**{"class": "hotfix", "candidate_ref": NEW})
    hot["_path"] = g.RECORDS / "thing" / "20260812-hotfix-new0000.yaml"
    check("a middle-rung deviation backed by a hotfix record reads hotfix-backfill-owed",
          g.grade_component("thing", mid, three, [hot], {}, set())["grade"] == "hotfix-backfill-owed")
    prod_ahead = {"fleet": REF, "deviations": {"prd": {"value": NEW}}}
    check("prod ahead with a record owes a backfill whatever the record's class (C4)",
          g.grade_component("thing", prod_ahead, three, [cand], {}, set())["pairs"]
          == {"dev->preprod": "aligned", "preprod->prod": "hotfix-backfill-owed"})

    same = {"fleet": REF, "deviations": {}}
    check("three equal rungs grade aligned", g.grade_component("thing", same, three, [], {}, set())["grade"] == "aligned")
    check("the two-rung ladder grades exactly as before",
          g.grade_component("thing", {"fleet": REF, "deviations": {"pre": {"value": NEW}}}, two, [], {}, set())["grade"]
          == "UNDECLARED-SKEW"
          and g.grade_component("thing", {"fleet": REF, "deviations": {"pre": {"value": NEW}}}, two, [cand], {}, set())["grade"]
          == "candidate")
    absent_prod = {"fleet": REF, "deviations": {"pre": {"value": NEW}, "prd": {"absent": True}}}
    check("a rung that is absent takes no part in the pairs",
          g.grade_component("thing", absent_prod, three, [], {}, set())["pairs"] == {"dev->preprod": "UNDECLARED-SKEW"})
    check("the worst hop wins the component grade",
          g.GRADE_ORDER[0] == "UNDECLARED-SKEW" and g.GRADE_ORDER[-1] == "aligned")

    # The hop into prod is the ladder's top hop, not whichever rung resolves
    # highest: a component prod does not run has no hop into prod.
    check("a candidate that reached preprod on a component absent from prod grades candidate, not owed",
          g.grade_component("thing", absent_prod, three, [cand], {}, set())["pairs"] == {"dev->preprod": "candidate"})
    absent_pre = {"fleet": REF, "deviations": {"pre": {"absent": True}, "prd": {"value": NEW}}}
    check("prod ahead of dev with preprod absent is still the hop into prod (C4)",
          g.grade_component("thing", absent_pre, three, [cand], {}, set())["pairs"] == {"dev->prod": "hotfix-backfill-owed"})

    # The legacy allowlist predates the dev rung: it excuses the preprod->prod
    # hop it was written for and nothing else.
    allow = {"thing": {"reason": "predates the ladder"}}
    check("the legacy allowlist does not excuse an undeclared dev-hop skew",
          g.grade_component("thing", dev_only, three, [], {}, allow)["pairs"]
          == {"dev->preprod": "UNDECLARED-SKEW", "preprod->prod": "aligned"})
    check("the legacy allowlist still excuses the preprod->prod hop",
          g.grade_component("thing", dev_and_pre, three, [], {}, allow)["pairs"]
          == {"dev->preprod": "aligned", "preprod->prod": "legacy-declared"}
          and g.grade_component("thing", mid, two, [], {}, allow)["grade"] == "legacy-declared")

    # An open hotfix record owes its backfill whether its ref runs on the
    # upper rung or the lower one (§2: "whether or not the rungs agree").
    check("a hotfix record for the lower rung's ref owes its backfill on that hop",
          g.grade_component("thing", mid, three, [hot], {}, set())["pairs"]
          == {"dev->preprod": "hotfix-backfill-owed", "preprod->prod": "hotfix-backfill-owed"}
          and g.grade_component("thing", mid, two, [hot], {}, set())["grade"] == "hotfix-backfill-owed")

    # The daily review names the hops that skew, from the readiness document.
    row = g.grade_component("thing", mid, three, [], {}, set())
    comp = g.component_readiness(row, mid, [], three, HOP, TODAY, None, {"dev", "pre", "prd"})
    check("the readiness document carries the graded hops", comp.get("pairs") == row["pairs"], comp.get("pairs"))
    real = g.build_readiness
    g.build_readiness = lambda today: {"components": [comp], "source": {"head_commit": None}}
    try:
        review = g.build_review(today=TODAY, ack="tester")
    finally:
        g.build_readiness = real
    what = [a["what"] for a in review["attention"] if a["component"] == "thing"]
    check("the review names the skewed hops",
          what == ["thing: dev and preprod, preprod and prod run different refs and no record says why."], what)


# --- the daily review (ADR 1177) ------------------------------------------
def review_tests():
    print("daily review")
    try:
        doc = g.build_review(today=dt.date.today(), ack="tester")
    except SystemExit as why:  # a tree with no goatnets declares no rungs
        print(f"  skip  {why}")
        return
    check("the review document names its schema and the tree",
          doc.get("schema") == "release-review-v1" and "head" in doc and "check" in doc, list(doc))
    items = doc["act_now"] + doc["soaking"] + doc["blocked"]
    check("every item carries a component, a record path and the exact command",
          all(i.get("component") and i.get("record") and "graduate.py graduate" in i.get("command", "") for i in items),
          [i.get("component") for i in items if not i.get("command")])
    check("every graduate command names the ack that was asked for",
          all("--ack tester" in i["command"] for i in items))
    check("a soaking item names its runnable date", all(i.get("runnable") for i in doc["soaking"]), doc["soaking"])
    check("a blocked item names its blockers", all(i.get("blockers") for i in doc["blocked"]))
    check("every item says what it is, what leaving it does and what acting does",
          all(i.get("what") and i.get("if_left") and i.get("if_acted") for i in items),
          [i["component"] for i in items if not (i.get("what") and i.get("if_left") and i.get("if_acted"))])
    check("a ladder candidate's if-acted names the prod deploy in capitals; a fastlane debt's says no deploy",
          all(("PROD DEPLOY" in i["if_acted"]) == (i["class"] != "hotfix") for i in items), [(i["component"], i["class"]) for i in items])
    check("the summary counts pending prod changes", "prod_changes_pending" in doc.get("summary", {}))
    check("the review never writes", not any(w in src for w in ("write_text(", "safe_dump(")
          for src in [inspect.getsource(g.build_review), inspect.getsource(g.cmd_review)]))


# --- soak waivers ---------------------------------------------------------

WAIVER = {"by": "operator@example.test", "reason": "prod carries the defect this ref fixes"}


def waiver_tests():
    print("soak waivers")
    waived = g.soak_facts(record(opened="2026-08-12", soak_waiver=WAIVER), HOP, TODAY)
    check("a_waived_soak_is_its_own_state",
          waived["state"] == "waived" and waived["waiver"]["by"] == "operator@example.test"
          and waived["waiver"]["attribution"] == "named"
          and waived["elapsed_days"] == 0 and waived["required_days"] == 2, waived)

    plain = g.soak_facts(record(opened="2026-08-12"), HOP, TODAY)
    check("the same unmet window with no waiver stays `soaking`",
          plain["state"] == "soaking" and "waiver" not in plain, plain)

    anon = g.soak_facts(record(opened="2026-08-12", soak_waiver={"by": None, "reason": "r"}),
                        HOP, TODAY)
    check("a waiver naming nobody is waived with UNKNOWN attribution, not with a guess",
          anon["state"] == "waived" and anon["waiver"]["by"] is None
          and anon["waiver"]["attribution"] == "unknown", anon)

    met = g.soak_facts(record(opened="2026-08-01", soak_waiver=WAIVER), HOP, TODAY)
    check("a window that elapsed anyway reads `met`, and still carries the waiver",
          met["state"] == "met" and met["waiver"]["by"] == "operator@example.test", met)

    noclock = g.soak_facts(record(opened=None, soak_waiver=WAIVER), HOP, TODAY)
    check("a waiver over an unreadable clock is waived with the clock's reason, no timer",
          noclock["state"] == "waived" and "elapsed_days" not in noclock
          and "not a date" in noclock["why"], noclock)

    r = g.candidate_readiness(record(opened="2026-08-12", soak_waiver=WAIVER), HOP, TODAY)
    check("readiness reads soak-waived — distinct from soaking AND from ready",
          r["state"] == "soak-waived" and r["soak_waiver"]["by"] == "operator@example.test", r)
    check("a waived soak does NOT clear the blocker — `graduate` still refuses on it",
          any(b.startswith("soak") for b in r["blockers"])
          and r["blockers"] == g.gate_failures(record(opened="2026-08-12"), HOP, TODAY), r)

    other = g.candidate_readiness(
        record(opened="2026-08-12", soak_waiver=WAIVER,
               evidence={"validation": "", "preprod_convergence": ""}), HOP, TODAY)
    check("a waiver never upgrades a component blocked on something else",
          other["state"] == "blocked", other)

    # The waiver is a FIELD now. A record whose prose says a soak was waived
    # while carrying no `soak_waiver:` block renders as an ordinary unmet
    # window — the exact misreport this field exists to end.
    #
    # Scanned fields are where an operator RECORDS that decision, never
    # `reason:`. `reason:` describes what the image changes, so a record whose
    # subject IS the waiver feature says "operator-waived" while claiming
    # nothing about its own soak — this check flagged exactly that on the
    # record introducing the field, which is a false positive, not a find.
    DECISION_FIELDS = ("operator_ack", "note", "evidence")
    prose = []
    for rec in g.load_records():
        if "_error" in rec or rec.get("soak_waiver"):
            continue
        claimed = " ".join(str(rec.get(f) or "") for f in DECISION_FIELDS)
        if re.search(r"WAIVED|was waived|operator-waived", claimed):
            prose.append(str(rec["_path"].relative_to(g.REPO)))
    check("every committed record that RECORDS a waived soak carries the field",
          not prose, prose)


# --- readiness ------------------------------------------------------------

def readiness_tests():
    print("readiness state + blockers")
    ready = g.candidate_readiness(record(), HOP, TODAY)
    check("a record meeting every gate is ready",
          ready["state"] == "ready" and ready["blockers"] == [], ready)

    soaking = g.candidate_readiness(record(opened="2026-08-12"), HOP, TODAY)
    check("only-soak failure reads as soaking, not blocked",
          soaking["state"] == "soaking" and len(soaking["blockers"]) == 1, soaking)

    blocked = g.candidate_readiness(
        record(opened="2026-08-12", evidence={"validation": "", "preprod_convergence": ""}),
        HOP, TODAY)
    check("blocked_names_every_failing_gate",
          blocked["state"] == "blocked" and len(blocked["blockers"]) == 3
          and any("preprod_convergence" in b for b in blocked["blockers"])
          and any(b.startswith("soak") for b in blocked["blockers"]), blocked)

    no_ack = g.candidate_readiness(record(operator_ack=None), HOP, TODAY)
    check("a missing operator_ack blocks when the ladder asks for one",
          no_ack["state"] == "blocked" and no_ack["ack_required"] is True
          and any("operator-ack" in b for b in no_ack["blockers"]), no_ack)

    hop_no_ack = dict(HOP, gates=[gg for gg in HOP["gates"] if gg["id"] != "operator-ack"])
    check("a ladder that asks for no ack does not invent the requirement",
          g.candidate_readiness(record(operator_ack=None), hop_no_ack, TODAY)["state"] == "ready")

    # Same failing gates, same words: the view cannot drift from the refusal.
    rec = record(opened="2026-08-12", evidence={"validation": "", "preprod_convergence": "x"})
    check("blockers are gate_failures() verbatim — one implementation",
          g.candidate_readiness(rec, HOP, TODAY)["blockers"][: len(g.gate_failures(rec, HOP, TODAY))]
          == g.gate_failures(rec, HOP, TODAY))


# --- ledger join ----------------------------------------------------------

def ledger_tests():
    print("deploy-ledger join")
    check("digest_of reads the artifact identity out of a templated ref",
          g.digest_of(REF) == "sha256:" + "1" * 64 and g.digest_of(DEPLOYED_REF) == g.digest_of(REF))
    check("digest_of on a tag-only ref is None, not a guess",
          g.digest_of("ghcr.io/x/y:latest") is None and g.digest_of(None) is None)

    with tempfile.TemporaryDirectory() as tmp:
        roles = os.path.join(tmp, "roles")
        os.makedirs(os.path.join(roles, "thing"))
        with open(os.path.join(roles, "thing", "deploy-history.yml"), "w") as f:
            f.write("service: thing\nhistory:\n"
                    f"  - sha: sha-abc1234\n    image: {DEPLOYED_REF}\n"
                    "    pr: 42\n    merged_at: \"2026-08-10T00:00:00Z\"\n"
                    f"  - sha: sha-old\n    image: ghcr.io/usareur-af/goat-thing:sha-old@sha256:{'2' * 64}\n"
                    "    pr: 7\n    merged_at: 2026-07-01 00:00:00+00:00\n")
        old_roles, old_repo = g.ROLES, g.REPO
        g.ROLES = __import__("pathlib").Path(roles)
        g.REPO = __import__("pathlib").Path(tmp)
        try:
            facts = g.ledger_facts("thing", {}, {"fleet": REF, "prod": REF})
            check("ledger_joins_on_digest_across_org_rendering",
                  facts["pin_deployed"] == {"fleet": True, "prod": True}, facts)
            check("the newest row is the newest merged_at, mixed YAML shapes and all",
                  facts["newest"]["pr"] == 42
                  and facts["newest"]["ordered_by"] == "merged_at", facts["newest"])

            unpinned = g.ledger_facts("thing", {}, {"fleet": "ghcr.io/x/y@sha256:" + "9" * 64})
            check("a pinned artifact absent from the ledger reads false, not silent",
                  unpinned["pin_deployed"]["fleet"] is False, unpinned)

            no_digest = g.ledger_facts("thing", {}, {"fleet": "ghcr.io/x/y:latest"})
            check("a pin with no digest cannot be joined and says None, not False",
                  no_digest["pin_deployed"]["fleet"] is None, no_digest)

            none_cov = g.ledger_facts("thing", {"ledger_component": None}, {"fleet": REF})
            check("declared no-ledger-coverage is a stated gap, never a False",
                  none_cov["status"] == "no-ledger-coverage"
                  and none_cov["pin_deployed"]["fleet"] is None, none_cov)

            missing = g.ledger_facts("absent-role", {}, {"fleet": REF})
            check("a component whose ledger file does not exist says which file",
                  missing["status"] == "ledger-file-missing"
                  and missing["path"].endswith("absent-role/deploy-history.yml"), missing)
        finally:
            g.ROLES, g.REPO = old_roles, old_repo


# --- build facts ----------------------------------------------------------

def build_tests():
    print("pinned-build facts")
    no_pin = g.build_facts(None, "HEAD")
    check("no pin at all is an unknown with a reason",
          no_pin["tag"] is None and no_pin["known"] is False
          and no_pin["why"] == "no ref pinned", no_pin)
    digest_only = g.build_facts("ghcr.io/x/y@sha256:" + "1" * 64, "HEAD")
    check("a digest-only pin names no build commit and says why",
          digest_only["known"] is False and "sha-<short>" in digest_only["why"], digest_only)
    absent = g.build_facts("ghcr.io/x/y:sha-0000000@sha256:" + "1" * 64, "HEAD")
    check("a build commit this checkout does not carry is unknown, never 0 commits",
          absent["known"] is False and absent["tag"] == "sha-0000000"
          and "commits_on_head_since" not in absent, absent)
    check("an unpinned component still carries a reading, unknown for the outer reason",
          no_pin["component_commits"]["known"] is False
          and "declares no `image_workflow:`" in no_pin["component_commits"]["why"], no_pin)
    unbuilt = g.build_facts(
        None, "HEAD",
        g.image_workflow({"image_workflow": ".github/workflows/goat-id-console-image.yml"}))
    check("a declared workflow with nothing pinned says which fact is missing",
          unbuilt["component_commits"]["known"] is False
          and "no build commit" in unbuilt["component_commits"]["why"], unbuilt)


# --- per-component build inputs -------------------------------------------

def workflow_tests():
    print("image-workflow paths (is there a build nothing pins?)")
    # The declared filter is read from the workflow itself — one home, not a
    # second mapping. This is the tree's real file, so a rename fails here.
    paths, why = g.workflow_push_paths(".github/workflows/goat-id-console-image.yml")
    check("the console workflow's push paths are read from the workflow",
          why is None and "ops/goat-id-console/**" in paths, (paths, why))

    missing, why = g.workflow_push_paths(".github/workflows/no-such-workflow.yml")
    check("a declared workflow that is not in the checkout is a stated unknown",
          missing is None and "not in this checkout" in why, why)

    with tempfile.TemporaryDirectory() as tmp:
        old_repo = g.REPO
        g.REPO = __import__("pathlib").Path(tmp)
        try:
            os.makedirs(os.path.join(tmp, "wf"))
            def write(name, body):
                with open(os.path.join(tmp, "wf", name), "w") as f:
                    f.write(body)
                return g.workflow_push_paths(f"wf/{name}")

            unfiltered, why = write("all.yml", "on:\n  push:\n    branches: [main]\njobs: {}\n")
            check("an_unreadable_paths_filter_is_unknown_not_zero_commits",
                  unfiltered is None and "no `on.push.paths` filter" in why, why)

            ignored, why = write("ign.yml", "on:\n  push:\n    paths-ignore: ['docs/**']\njobs: {}\n")
            check("a paths-ignore exclusion is refused rather than half-evaluated",
                  ignored is None and "paths-ignore" in why, why)

            negated, why = write("neg.yml", "on:\n  push:\n    paths: ['ops/**', '!ops/docs/**']\njobs: {}\n")
            check("a negated pattern is refused rather than counted as a match",
                  negated is None and "negated pattern" in why, why)

            ok, why = write("ok.yml", "on:\n  push:\n    paths: ['ops/thing/**']\njobs: {}\n")
            check("the boolean `on:` key YAML 1.1 produces is read, not missed",
                  why is None and ok == ["ops/thing/**"], (ok, why))
        finally:
            g.REPO = old_repo

    undeclared = g.image_workflow({})
    check("a component with no declared image_workflow is unknown, never `none`",
          undeclared["paths"] is None and "declares no `image_workflow:`" in undeclared["why"],
          undeclared)
    declared_none = g.image_workflow({"image_workflow": None})
    check("an explicit null reads as 'not built in this repo', with its own words",
          declared_none["paths"] is None
          and "no image workflow in this repo" in declared_none["why"], declared_none)

    facts = g.component_commit_facts(None, "HEAD", undeclared)
    check("an undeclared workflow yields no count and no zero",
          facts["known"] is False and "count" not in facts and facts["paths"] == [], facts)

    # Against the real checkout: a commit that IS HEAD has nothing after it.
    ok, head = g.git_out("rev-parse", "HEAD")
    if ok:
        real = g.component_commit_facts(head, head,
                                        g.image_workflow({"image_workflow": ".github/workflows/goat-id-console-image.yml"}))
        check("a pinned build at HEAD counts 0 component commits — a measurement",
              real["known"] is True and real["count"] == 0 and real["newest"] is None, real)


# --- the document ---------------------------------------------------------

def document_tests():
    print("document shape (this tree)")
    try:
        doc = g.build_readiness(today=TODAY)
    except SystemExit as why:  # a tree with no goatnets declares no rungs
        print(f"  skip  {why}")
        return
    check("schema and generation stamp are present",
          doc["schema"] == "release-readiness-v1" and doc["generated_date"] == TODAY.isoformat()
          and doc["generated_unix_ms"] > 0)
    check("every governed component appears once", len(doc["components"]) ==
          len(g.load_pins().get("components") or {}))
    check("the document states what it cannot know", len(doc["unknowable"]) >= 3
          and any("waiver" in u for u in doc["unknowable"]))
    # A limit that stopped being true must not stay on the page: the waiver is
    # a field now, and the built-but-pinned-nowhere question is answered from
    # the image workflows' own filters.
    stale_claims = [u for u in doc["unknowable"]
                    if "the ladder has no waiver field" in u
                    or "cannot list them" in u]
    check("retired limits are gone from what-this-page-cannot-know", not stale_claims,
          stale_claims)
    states = {c["readiness_state"] for c in doc["components"]}
    check("readiness_state is drawn from the closed vocabulary",
          states <= {"ready", "soaking", "soak-waived", "blocked", "no-candidate",
                     "not-applicable"}, states)
    check("every component declares where its build inputs come from",
          all("image_workflow" in c for c in doc["components"]))
    readings = [(c["component"], r["rung"], r["build"].get("component_commits"))
                for c in doc["components"] for r in c["rungs"]]
    check("every rung carries a component-commit reading, known or explained",
          all(cc is not None and (cc.get("known") or cc.get("why")) for _, _, cc in readings),
          [x for x in readings if not x[2]])
    check("no reading reports a count without having been able to read the filter",
          all(cc.get("known") for *_, cc in readings if "count" in cc))
    check("grades are build_status()'s, not re-derived here",
          all(c["grade"] in {"aligned", "candidate", "hotfix-backfill-owed", "legacy-declared",
                             "UNDECLARED-SKEW", "env-local", "single-rung", "exempt"}
              for c in doc["components"]))
    check("the ladder's live hop rides along so the view need not read ladder.yaml",
          doc["ladder"]["hop"]["from"] == "preprod" and doc["ladder"]["rungs"][0]["goatnet"])


# ---------------------------------------------------------------------------
# Graduation sets (ADR 1125). The value of a set is the REFUSAL, so that is
# what these pin — plus the two things a set must never do: half-graduate, or
# report one blocker when it has several.
# ---------------------------------------------------------------------------

def set_doc(**over):
    doc = {
        "name": "console-train",
        "opened": "2026-08-10",
        "rung": "preprod",
        "reason": "the console and the portal were validated together",
        "members": [
            {"component": "thing", "ref": REF, "record": "records/thing/x.yaml"},
            {"component": "other", "ref": REF, "record": "records/other/x.yaml"},
        ],
        "state": "open",
    }
    doc.update(over)
    return doc


def set_tests():
    print("\n-- graduation sets --")

    # Membership lookup only sees OPEN sets: a graduated set must not keep
    # refusing forever, which would make the mechanism a ratchet.
    open_set = set_doc()
    graduated = set_doc(name="old-train", state="graduated")
    check("an open set claims its members",
          g.open_set_for([open_set], "thing")["name"] == "console-train")
    check("a graduated set claims nobody",
          g.open_set_for([graduated], "thing") is None)
    check("a component in no set is unclaimed",
          g.open_set_for([open_set], "unrelated") is None)
    check("a parse-failed set claims nobody rather than everybody",
          g.open_set_for([{"_error": "bad yaml", "_path": "x"}], "thing") is None)

    # EVERY blocker, member-labelled. Reporting the first only would make the
    # second a surprise after the first is fixed — the experience this replaces.
    records = [record(component="thing"),
               record(component="other", opened="2026-08-12", evidence={"validation": "",
                                                                        "preprod_convergence": ""})]
    blockers = g.set_blockers(set_doc(), records, HOP, TODAY)
    check("blockers name their member", all(":" in b for b in blockers))
    check("a member failing several gates contributes all of them",
          len([b for b in blockers if b.startswith("other:")]) >= 3, blockers)
    check("a ready member contributes none",
          not any(b.startswith("thing:") for b in blockers), blockers)

    # A member whose record does not exist for the SET'S EXACT REF is a
    # blocker, not a silent skip: the set moves refs and the record carries the
    # evidence, so a member with neither is unreviewable.
    only_thing = [record(component="thing")]
    missing = g.set_blockers(set_doc(), only_thing, HOP, TODAY)
    check("a member with no record for its ref blocks",
          any("other" in b and "no OPEN candidate record" in b for b in missing), missing)

    # A ready set has no blockers at all — the refusal must be capable of
    # passing, or it is a wall rather than a gate.
    ready = [record(component="thing"), record(component="other")]
    check("a fully ready set is ready", g.set_blockers(set_doc(), ready, HOP, TODAY) == [])


def main():
    for fn in (soak_tests, backfill_tests, pair_tests, review_tests, waiver_tests, readiness_tests, ledger_tests, build_tests,
               workflow_tests, document_tests, set_tests):
        fn()
    if FAILURES:
        print(f"\n{len(FAILURES)} failure(s): {', '.join(FAILURES)}")
        return 1
    print("\nall readiness assertions hold")
    return 0


if __name__ == "__main__":
    sys.exit(main())
