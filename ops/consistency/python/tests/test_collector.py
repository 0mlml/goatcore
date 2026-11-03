#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# test_collector.py — the per-node fact assembly, and what ABSENCE reads as.
#
# The collector (ops/consistency/collect/consistency-snapshot.py) and the stamp
# helper (ops/consistency/stamp.py) are the two pieces that turn a node's disk
# into the facts CF-3 publishes. Everything interesting about them is a
# degraded case, so that is what this file pins: no ledger, no stamp, an
# ADR 0859-era ledger row, a tip that is not actually running, and an artifact
# whose integrity check fails.
#
# The single most important assertion here is `stamp_absent_is_indeterminate_
# not_unaccountable`. Those two grades demand OPPOSITE first actions (§4.2), and
# a node that predates stamping has not been caught running something
# unexplained — it has never been asked. Getting that backwards would point
# every not-yet-stamped node in the fleet at `rollback.sh`.
#
# Plain python3 + asserts, no pytest — the ops/operators/test_*.py convention.
# Exit codes: 0 all pass, 1 one or more failed.

import importlib.util
import json
import os
import shutil
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
CONSISTENCY = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(CONSISTENCY, "python"))

from goat_consistency import grading, grammar as grammar_mod  # noqa: E402


def _load(name, relpath):
    """The collector and the stamp helper are hyphenated CLIs, not modules —
    the same importlib reach CF-2 removed from report.py, kept here on purpose
    because these two ARE scripts (they run on a node and on the control node
    respectively) rather than library code wearing a script's clothes."""
    spec = importlib.util.spec_from_file_location(name, os.path.join(CONSISTENCY, relpath))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


collector = _load("consistency_snapshot", "collect/consistency-snapshot.py")
stamp_mod = _load("consistency_stamp", "stamp.py")

FAILURES = []


def check(what, actual, expected):
    if actual != expected:
        FAILURES.append("%s\n    expected: %r\n    actual:   %r" % (what, expected, actual))


def ok(what, condition):
    if not condition:
        FAILURES.append(what)


def write_ledger(root, component, rows):
    d = os.path.join(root, component)
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "deploy-history.jsonl"), "w") as f:
        for row in rows:
            f.write(json.dumps(row) + "\n")


DIGEST = "sha256:" + "ab" * 32
REF = "ghcr.io/example/goat-zenoh-portal:sha-f33cb9d@" + DIGEST


# ---------------------------------------------------------------------------


def test_no_ledger_at_all_is_empty_and_says_so():
    """A node that has never been deployed by a ledger-writing role reports
    zero components AND a note explaining why. A short list with no note is the
    one output the collector must never produce — it reads as complete."""
    root = tempfile.mkdtemp()
    try:
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        check("no ledger -> no components", snap["components"], [])
        ok("no ledger -> the note says so", "no deploy ledger" in snap["collector_note"])
        ok("no ledger -> still a well-formed snapshot", snap["host_id"] == "node-a")
    finally:
        shutil.rmtree(root)


def test_stamp_absent_is_indeterminate_not_unaccountable():
    """THE case. A node with a ledger but no applied-intent stamp reports empty
    intent fields, and a reader must grade that `indeterminate` — nothing to
    read — and never `unaccountable`.

    §4.2 gives those two opposite first actions: re-run the play, versus do NOT
    re-run it because that erases the evidence. Every node in the fleet is in
    this state until a stamping checkout deploys it, so grading it
    `unaccountable` would point the whole fleet at rollback.sh on day one."""
    root = tempfile.mkdtemp()
    try:
        write_ledger(root, "goat-zenoh-portal", [{"ref": REF, "artifact_type": "image",
                                                  "ts": "2026-08-01T00:00:00Z"}])
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        check("no stamp -> empty content hash", snap["intent_content_hash"], "")
        check("no stamp -> serial 0", snap["intent_serial"], 0)
        check("no stamp -> applied_at unstatable", snap["applied_at_unix_ms"], 0)
        ok("no stamp -> the note says so", "no applied-intent stamp" in snap["collector_note"])

        # And the grade a reader lands on, computed by the shared library rather
        # than asserted about it.
        g = grammar_mod.load()
        intent = {"content_hash": "sha256:" + "cd" * 32, "serial": 3,
                  "generated_at": "2026-08-01T00:00:00Z", "history": []}
        cell = grading.account_for(intent, snap["intent_content_hash"])
        check("no stamp -> cell state", cell["state"], "unreported")
        check("no stamp -> grade", grading.grade_cells([cell], grammar=g), "indeterminate")
        ok("no stamp -> NOT unaccountable",
           grading.grade_cells([cell], grammar=g) != "unaccountable")
    finally:
        shutil.rmtree(root)


def test_adr_0859_era_row_still_reports_its_ref():
    """An old ledger row carries `image` and no `ref`. Reading only `ref` would
    silently drop every component whose last deploy predates ADR 1060, and a
    dropped component reads as `not deployed` rather than as `old ledger`."""
    root = tempfile.mkdtemp()
    try:
        write_ledger(root, "netbird-mgmt",
                     [{"image": REF, "ts": "2026-05-01T00:00:00Z", "action": "deploy"}])
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        check("0859-era row count", len(snap["components"]), 1)
        check("0859-era ref read from `image`", snap["components"][0]["ref"], REF)
        check("0859-era artifact_type defaults to image",
              snap["components"][0]["artifact_type"], "image")
    finally:
        shutil.rmtree(root)


def test_unreconciled_and_diverged_never_read_as_converged():
    """§5.2's ordering caveat, enforced. A ledger row is written at role ENTRY,
    so an unreconciled tip is recorded INTENT. Reporting it as a converged fact
    would manufacture the false green the surface exists to prevent."""
    root = tempfile.mkdtemp()
    try:
        write_ledger(root, "goat-zenoh-portal", [{"ref": REF, "artifact_type": "image",
                                                  "ts": "2026-08-01T00:00:00Z"}])
        # Could not look at all.
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        check("no reconciliation -> unreconciled",
              snap["components"][0]["reconciled"], "unreconciled")

        # Looked, and nothing on the host runs it.
        check("nothing running -> diverged", collector.reconcile(REF, {}), ("diverged", ""))
        # Looked, and it is running under a different tag but the same digest —
        # which is the SAME artifact. Comparing whole strings would call this
        # drift; the digest is the identity.
        other = "ghcr.io/other-org/goat-zenoh-portal:whatever@" + DIGEST
        check("same digest, different org -> confirmed",
              collector.reconcile(REF, {"portal": [other]}), ("confirmed", other))
        # Could not look.
        check("docker unavailable -> unreconciled",
              collector.reconcile(REF, None), ("unreconciled", ""))
    finally:
        shutil.rmtree(root)


def test_stamp_carries_the_generation_and_verifies_it():
    """The stamp records which generation a run converges a node to, and whether
    the artifact passed its integrity check. `verified` is fail-closed: a reader
    treats false as `unreadable`, never as `match` (ADR 1109 P14)."""
    if not os.path.isdir(os.path.join(CONSISTENCY, "intent")):
        print("skip: no committed intent artifacts in this tree")
        return
    body = stamp_mod.build("zagros", "zagros_hosts")
    check("stamp format", body["format"], "goat-applied-intent-v1")
    check("stamp goatnet", body["goatnet"], "zagros")
    check("stamp member group", body["member_group"], "zagros_hosts")
    ok("stamp carries a content hash",
       str(body["intent_content_hash"]).startswith("sha256:"))
    ok("stamp carries a serial", isinstance(body["intent_serial"], int)
       and body["intent_serial"] >= 1)
    check("the committed artifact verifies", body["intent_verified"], True)
    ok("stamp carries NO timestamp of its own — the file's mtime is applied_at",
       not any(k.startswith("applied") for k in body))

    # A tampered artifact fails the check rather than being stamped as fact.
    with open(os.path.join(CONSISTENCY, "intent", "zagros", "intent.json")) as source_file:
        doc = dict(json.load(source_file))
    doc["content_hash"] = "sha256:" + "00" * 32
    committed, recomputed = grading.intent_mod.self_consistent(doc)
    ok("a tampered artifact does not self-verify", committed != recomputed)


def test_an_ungoverned_goatnet_is_refused_not_invented():
    """Forward-only scope: a goatnet not listed in fleet-pins.yaml has no
    generation to stamp. Inventing one would make an ungoverned node look
    converged, which is worse than it reporting nothing."""
    try:
        stamp_mod.build("not-a-goatnet", "")
    except SystemExit as e:
        ok("ungoverned goatnet is refused with a reason", "no intent artifact" in str(e))
        return
    FAILURES.append("an ungoverned goatnet was stamped rather than refused")


def write_manifest(root, component, entries):
    d = os.path.join(root, component)
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "render-manifest.json"), "w") as f:
        json.dump({"format": "goat-render-manifest-v1", "component": component,
                   "renders": entries}, f)


def sha256_text(text):
    import hashlib
    return "sha256:" + hashlib.sha256(text.encode()).hexdigest()


def test_render_hand_edit_grades_unaccountable_red_then_reverted():
    """THE CF-4 gate, end to end on a real filesystem: a watched rendered file
    is hashed by the collector against its converge-time manifest; a hand-edit
    grades `unaccountable` with the reconverge pointer; reverting the edit
    takes the same row back to `match`. Red-then-reverted, through the shared
    library rather than asserted about it."""
    root = tempfile.mkdtemp()
    g = grammar_mod.load()
    try:
        rendered = "services:\n  router:\n    image: zenoh-pqc-fips\n"
        fpath = os.path.join(root, "nova-zenoh", "docker-compose.yml")
        os.makedirs(os.path.dirname(fpath), exist_ok=True)
        with open(fpath, "w") as f:
            f.write(rendered)
        write_manifest(root, "nova-zenoh",
                       [{"path": fpath, "sha256": sha256_text(rendered), "note": ""}])

        # GREEN: the file is what the converge rendered.
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        check("untouched render count", len(snap["renders"]), 1)
        rec = snap["renders"][0]
        check("untouched render agrees", rec["sha256"], rec["recorded_sha256"])
        cell = grading.account_for_render(rec)
        check("untouched render cell", cell["state"], "resolved")
        check("untouched render grade", grading.grade_cells([cell], grammar=g), "match")

        # RED: hand-edit the rendered file.
        with open(fpath, "a") as f:
            f.write("    restart: never  # hand edit\n")
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        rec = snap["renders"][0]
        ok("hand-edit changes the current hash", rec["sha256"] != rec["recorded_sha256"])
        cell = grading.account_for_render(rec)
        check("hand-edited render cell", cell["state"], "unaccountable")
        row = grading.grade_row("render", "zenoh-router:%s" % fpath, {"node-a": cell},
                                "zagros", vantage="live", artifact_type="render", grammar=g)
        check("hand-edited render grade", row.grade, "unaccountable")
        check("hand-edited render pointer kind", row.remediation["kind"], "reconverge")
        ok("the pointer is the refresh play, not rollback.sh",
           "refresh.yml" in row.remediation["pointer"]
           and "rollback.sh" not in row.remediation["pointer"])

        # REVERTED: back to the converge-time bytes.
        with open(fpath, "w") as f:
            f.write(rendered)
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        cell = grading.account_for_render(snap["renders"][0])
        check("reverted render cell", cell["state"], "resolved")
        check("reverted render grade", grading.grade_cells([cell], grammar=g), "match")
    finally:
        shutil.rmtree(root)


def test_render_absence_states_stay_apart():
    """A deleted watched file is drift (`missing` -> unaccountable at the
    reader); an unhashable-at-converge entry has no baseline (`unreadable`);
    a malformed manifest is a collector NOTE, never a silent shorter list."""
    root = tempfile.mkdtemp()
    try:
        gone = os.path.join(root, "nova-zenoh", "docker-compose.yml")
        write_manifest(root, "nova-zenoh",
                       [{"path": gone, "sha256": sha256_text("x"), "note": ""},
                        {"path": gone + ".2", "sha256": "", "note": "missing at converge"}])
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        check("both entries reported", len(snap["renders"]), 2)
        by_path = {r["path"]: r for r in snap["renders"]}
        check("deleted file note", by_path[gone]["note"], "missing")
        check("deleted file current hash empty", by_path[gone]["sha256"], "")
        check("unhashable-at-converge keeps its converge note",
              by_path[gone + ".2"]["note"], "missing at converge")
        cell = grading.account_for_render(by_path[gone])
        check("deleted file is unaccountable", cell["state"], "unaccountable")
        cell = grading.account_for_render(by_path[gone + ".2"])
        check("no baseline is unreadable", cell["state"], "unreadable")

        # A malformed manifest surfaces in the note rather than vanishing.
        d = os.path.join(root, "broken-component")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "render-manifest.json"), "w") as f:
            f.write("not json{")
        snap = collector.build(root, "node-a", "docker", reconcile_enabled=False)
        ok("a malformed manifest is named in the collector note",
           "render manifest for broken-component" in snap["collector_note"])
    finally:
        shutil.rmtree(root)


def test_snapshot_is_byte_stable_for_unchanged_facts():
    """The publisher re-reads this file on every beat. If identical facts
    produced different bytes, every beat would look like a change."""
    root = tempfile.mkdtemp()
    try:
        write_ledger(root, "a", [{"ref": REF, "artifact_type": "image",
                                  "ts": "2026-08-01T00:00:00Z"}])
        one = collector.build(root, "n", "docker", reconcile_enabled=False)
        time.sleep(0.01)
        two = collector.build(root, "n", "docker", reconcile_enabled=False)
        for doc in (one, two):
            doc.pop("collected_at_unix_ms")
        check("identical facts -> identical snapshot", one, two)
    finally:
        shutil.rmtree(root)


# ---------------------------------------------------------------------------
# The docker seam. Everything above hands `reconcile()` a live-map built by
# hand, which is why F-440 shipped: `running_images()` — the one function that
# actually talks to docker — was never run, and it asked a CONTAINER for
# `RepoDigests`, an IMAGE field. The template failed, stdout was empty, the
# returncode was ignored, and the node graded EVERY component `diverged`.
#
# A test that restates the format string would pass on the same wrong
# assumption. So these run the real function against a stub that
# behaves the way docker does: it knows which fields a container carries and
# fails the whole call on any other, exactly as Go's template execution does.
# ---------------------------------------------------------------------------

DOCKER_STUB = r"""#!/usr/bin/env python3
# A `docker` that answers the way the real one does. Written with no
# backslash escapes on purpose: it is generated, embedded and executed, and
# an escaping layer is exactly the kind of thing a fixture must not add.
import json, os, re, sys

# Only what a CONTAINER inspect actually carries. RepoDigests is deliberately
# absent: it belongs to an IMAGE, and asking a container for it is the bug
# this stub exists to catch.
FIELDS = {".Name", ".Id", ".Image", ".Config.Image", ".State.Status"}
STATE = json.load(open(os.environ["DOCKER_STUB_STATE"]))
argv = sys.argv[1:]

if argv[:1] == ["ps"]:
    if STATE.get("ps_fails"):
        print("Cannot connect to the Docker daemon", file=sys.stderr)
        sys.exit(1)
    print(chr(10).join(STATE["containers"].keys()))
    sys.exit(0)

if argv[:1] == ["inspect"]:
    i = argv.index("--format")
    fmt, names = argv[i + 1], argv[i + 2:]
    tokens = re.findall("{{([^}]*)}}", fmt)
    for tok in tokens:
        if tok not in FIELDS:
            print('template parsing error: template: :1:1: executing "" at '
                  '<%s>: map has no entry for key "%s"'
                  % (tok, tok.lstrip(".")), file=sys.stderr)
            sys.exit(1)
    for n in names:
        c = STATE["containers"].get(n, {})
        out = fmt
        for tok in tokens:
            out = out.replace("{{" + tok + "}}", c.get(tok, ""))
        print(out)
    sys.exit(0)

sys.exit(2)
"""


def _stub_docker(root, containers, ps_fails=False):
    """A `docker` on disk that answers like the real one. Returns its path."""
    state = os.path.join(root, "docker-state.json")
    with open(state, "w") as f:
        json.dump({"containers": containers, "ps_fails": ps_fails}, f)
    path = os.path.join(root, "docker")
    with open(path, "w") as f:
        f.write(DOCKER_STUB)
    os.chmod(path, 0o755)
    os.environ["DOCKER_STUB_STATE"] = state
    return path


def test_running_images_reads_a_container_docker_will_actually_answer_for():
    """F-440. The live probe must ask only for fields a container carries.

    Asking for one it does not fails the WHOLE inspect, and the node then
    reports every component diverged with nothing saying it could not look.
    """
    root = tempfile.mkdtemp()
    try:
        docker = _stub_docker(root, {
            "goat-zenoh-portal": {
                ".Name": "/goat-zenoh-portal",
                ".Config.Image": REF,
                ".Image": "sha256:" + "cd" * 32,
            },
        })
        live = collector.running_images(docker)
        ok("a container inspect SUCCEEDS — no image-only field is asked for, "
           "so the result is neither None (could not look) nor {} (nothing "
           "running), the two shapes a failed template collapses into",
           bool(live))
        check("the running ref is reported under its container name",
              live and live.get("goat-zenoh-portal")[0], REF)
        check("a tip that IS running grades confirmed, not diverged",
              collector.reconcile(REF, live), ("confirmed", REF))
    finally:
        shutil.rmtree(root)


def test_a_docker_that_cannot_be_read_is_unreconciled_never_diverged():
    """The F-440 aggravator: an ignored returncode turned "I could not look"
    into "nothing is running". None and {} are not interchangeable here."""
    root = tempfile.mkdtemp()
    try:
        docker = _stub_docker(root, {}, ps_fails=True)
        check("docker unavailable -> None, never an empty map",
              collector.running_images(docker), None)
        check("and None grades unreconciled",
              collector.reconcile(REF, None), ("unreconciled", ""))

        write_ledger(root, "goat-zenoh-portal",
                     [{"ts": "2026-09-04T00:00:00Z", "ref": REF}])
        snap = collector.build(root, "node-a", docker, reconcile_enabled=True)
        check("the node's tip is not graded on evidence it does not have",
              snap["components"][0]["reconciled"], "unreconciled")
        ok("and the snapshot SAYS it could not reconcile",
           "could not reconcile" in snap["collector_note"])
    finally:
        shutil.rmtree(root)


def main():
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    if FAILURES:
        print("FAIL: %d expectation(s) not met\n" % len(FAILURES))
        for f in FAILURES:
            print("  " + f)
        return 1
    print("ok: per-node fact assembly — absence reads as the honest degraded "
          "case, an unreconciled tip never reads as converged, and a node with "
          "no applied-intent stamp grades indeterminate rather than unaccountable.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
