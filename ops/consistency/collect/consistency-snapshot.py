#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# consistency-snapshot.py — assemble ONE node's convergence facts.
#
# Runs on the node, on a systemd timer, and writes an atomic JSON snapshot that
# the Rust publisher (goat-consistency-health) reads and puts on
# ops/consistency/report/<host_id>. This is the node-net-health shape exactly:
# a privileged host-side collector writes a file; an unprivileged container
# reads it and publishes. The alternative — giving the publishing container the
# docker socket — would hand a read-only observability meter the ability to
# control every container on the host, which is not a trade this surface needs
# to make to answer "what are you running".
#
# IT ASSEMBLES FACTS AND EMITS NO VERDICT. There is no grade in this file's
# output, because a node knows what it is running and does NOT know whether that
# is current — that is a comparison against a tree it does not have. Grading
# happens at the rollup and at every reader, at read time (§5.1).
#
# Three inputs, all written by the rollback-ledger shared role:
#   /var/lib/goat/<component>/deploy-history.jsonl    the ADR 1060 tips
#   /var/lib/goat/intent/applied.json                 the applied-intent stamp
#   /var/lib/goat/<component>/render-manifest.json    the CF-4 render baseline
#                                                     (post-render, default-off)
#
# WHAT ABSENCE MEANS, stated because it is the difference between a useful
# surface and a lying one:
#   - no ledger at all      -> zero components; the node reports what it can,
#                              and the reader grades the missing rows
#                              `indeterminate` — nothing to read.
#   - no applied-intent stamp -> the intent fields stay EMPTY. A reader grades
#                              that `indeterminate`, never `unaccountable`: a
#                              node deployed before stamping existed has not
#                              been caught running something unexplained, it has
#                              simply never been asked. Older ledgers must read
#                              as the honest degraded case, not as a finding.
#
# Canon: docs/design/consistency-foundation.md §5.2.

import argparse
import hashlib
import json
import os
import socket
import subprocess
import sys
import time

LEDGER_ROOT = "/var/lib/goat"
STAMP_RELPATH = "intent/applied.json"
RENDER_MANIFEST = "render-manifest.json"
RENDER_MANIFEST_FORMAT = "goat-render-manifest-v1"
DEFAULT_OUT = "/var/lib/goat-consistency/snapshot.json"


def now_ms():
    return int(time.time() * 1000)


def read_last_json_line(path):
    """The ledger tip. Read from the END rather than parsed whole: a
    deploy-history.jsonl accumulates one row per real change and the only row
    this surface reports is the newest. A malformed tail line returns None
    rather than raising — the component then reports nothing, which a reader
    grades `indeterminate`, and an unparseable row is not evidence of drift."""
    try:
        with open(path, "rb") as f:
            data = f.read()
    except OSError:
        return None
    for line in reversed(data.decode("utf-8", "replace").splitlines()):
        line = line.strip()
        if not line:
            continue
        try:
            row = json.loads(line)
        except ValueError:
            return None
        return row if isinstance(row, dict) else None
    return None


def iso_to_ms(text):
    """A ledger `ts` (`%Y-%m-%dT%H:%M:%SZ`, ansible's iso8601) as unix ms, or 0.
    Zero means unstatable and is rendered as such; it is never treated as the
    epoch."""
    for fmt in ("%Y-%m-%dT%H:%M:%SZ", "%Y-%m-%dT%H:%M:%S.%fZ"):
        try:
            return int(time.mktime(time.strptime(text, fmt)) - time.timezone) * 1000
        except (TypeError, ValueError):
            continue
    return 0


def running_images(docker_bin):
    """Every image reference running on this host, by container name.

    Both the tag-ish `.Config.Image` (what compose asked for) and the resolved
    `.Image` digest are collected, because a ledger tip may be either form and a
    reconciliation that only understood one would report a correctly-converged
    node as diverged.

    DO NOT ask a container for `RepoDigests`. It is an IMAGE field; a container
    inspect has no such key, and Go template execution FAILS the whole call —
    docker writes the error to stderr, stdout is empty, and a returncode this
    function used to ignore. `live` then became an empty MAP, which is not "I
    could not look" but "I looked and nothing is running", so `reconcile()`
    graded every component on the node `diverged`. That is what shipped:
    measured 2026-09-04 on mgmt-zagros-prod-01-orin (36 diverged / 5 unreconciled
    / 0 confirmed) and mgmt-zagros-01-thor, on containers independently confirmed
    to be running the pinned digests. F-438 / F-440.

    Hence: no field a container does not carry, and EVERY failure returns None.
    An empty dict is reserved for the one case that truly means it — a host
    running no containers at all."""
    try:
        names = subprocess.run(
            [docker_bin, "ps", "--format", "{{.Names}}"],
            capture_output=True, text=True, timeout=30, check=False)
    except (OSError, subprocess.SubprocessError):
        return None
    if names.returncode != 0:
        return None
    containers = [n for n in names.stdout.split() if n]
    if not containers:
        return {}
    try:
        out = subprocess.run(
            [docker_bin, "inspect", "--format",
             "{{.Name}}\t{{.Config.Image}}\t{{.Image}}"]
            + containers,
            capture_output=True, text=True, timeout=60, check=False)
    except (OSError, subprocess.SubprocessError):
        return None
    if out.returncode != 0:
        # A partial or failed inspect is NOT an observation. Returning what
        # little came back would grade every container it did not describe as
        # diverged — the F-440 failure, in a quieter form.
        return None
    live = {}
    for line in out.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) < 3:
            continue
        name = parts[0].lstrip("/")
        live[name] = [p for p in parts[1:] if p and not p.startswith("<")]
    return live


def digest_of(reference):
    """The digest half of an OCI reference — the org-independent identity."""
    _, sep, digest = (reference or "").partition("@")
    return digest if sep else ""


def reconcile(ref, live):
    """Is this ledger tip actually what is running?

    §5.2's inherited ordering caveat: meta-dependencies run at role ENTRY, so
    the row records what the run INTENDED to converge to, not a post-success
    fact. `rollback.sh` mitigates that by reconciling against the live container
    before acting, and this publisher owes the same discipline — a tip reported
    unreconciled would manufacture exactly the false green the surface exists to
    prevent.

    Returns (state, live_ref). `live` of None means the node could not look at
    all, which is `unreconciled` — not `confirmed`, and not `diverged`."""
    if live is None:
        return "unreconciled", ""
    want_digest = digest_of(ref)
    for _, refs in sorted(live.items()):
        for candidate in refs:
            if candidate == ref:
                return "confirmed", candidate
            if want_digest and digest_of(candidate) == want_digest:
                return "confirmed", candidate
    # Nothing on this host is running it. Naming a nearest-miss would be a
    # guess; the reader is told the tip is not running and grades it unreadable.
    return "diverged", ""


def collect_components(ledger_root, live):
    components = []
    try:
        entries = sorted(os.listdir(ledger_root))
    except OSError:
        return components
    for name in entries:
        path = os.path.join(ledger_root, name, "deploy-history.jsonl")
        row = read_last_json_line(path)
        if row is None:
            continue
        # ADR 0859 back-compat: the old jsonl shape carried `image` with no
        # `ref`. Reading only `ref` would silently drop every component whose
        # last deploy predates ADR 1060, and a dropped component reads as
        # "not deployed" rather than as "old ledger".
        ref = row.get("ref") or row.get("image") or ""
        artifact_type = row.get("artifact_type") or "image"
        state, live_ref = ("unreconciled", "")
        if artifact_type == "image" and ref:
            state, live_ref = reconcile(ref, live)
        components.append({
            "component": name,
            "artifact_type": artifact_type,
            "ref": ref,
            "prior_ref": row.get("prior_ref") or "",
            "ts_unix_ms": iso_to_ms(row.get("ts")),
            "reversible": bool(row.get("reversible", True)),
            "reconciled": state,
            "live_ref": live_ref,
        })
    return components


def sha256_now(path):
    """The current hash of a watched file, or ("", note). The note keeps
    "missing" and "unreadable" APART, because a reader grades them oppositely:
    a file the converge rendered that is now GONE is drift (unaccountable); a
    file that could not be read is ungrounded (unreadable, never a verdict)."""
    try:
        h = hashlib.sha256()
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(1 << 20), b""):
                h.update(chunk)
        return "sha256:" + h.hexdigest(), ""
    except FileNotFoundError:
        return "", "missing"
    except OSError:
        return "", "unreadable"


def collect_renders(ledger_root):
    """The CF-4 render facts: for every component whose deploying role wrote a
    render manifest (/var/lib/goat/<component>/render-manifest.json, written
    AFTER its templates land), the hash the converge recorded and the hash the
    file carries NOW. Facts only — the comparison happens at the reader.

    Absence is honest here the same way it is everywhere else in this file:
    no manifest -> no render rows for that component, and a reader holding the
    intent's declared watch-set grades that `indeterminate` (declared, nothing
    recorded) rather than inventing either a green or a finding."""
    renders, notes = [], []
    try:
        entries = sorted(os.listdir(ledger_root))
    except OSError:
        return renders, notes
    for name in entries:
        path = os.path.join(ledger_root, name, RENDER_MANIFEST)
        try:
            with open(path) as f:
                doc = json.load(f)
            recorded_at_ms = int(os.path.getmtime(path) * 1000)
        except FileNotFoundError:
            continue
        except (OSError, ValueError):
            notes.append("render manifest for %s is unreadable — its renders are "
                         "unreported this pass" % name)
            continue
        if not isinstance(doc, dict) or doc.get("format") != RENDER_MANIFEST_FORMAT:
            notes.append("render manifest for %s is not a %s document — its renders "
                         "are unreported this pass" % (name, RENDER_MANIFEST_FORMAT))
            continue
        rows = doc.get("renders")
        if not isinstance(rows, list):
            notes.append("render manifest for %s carries no `renders` list — its "
                         "renders are unreported this pass" % name)
            continue
        for row in sorted((r for r in rows if isinstance(r, dict)),
                          key=lambda r: str(r.get("path") or "")):
            fpath = str(row.get("path") or "")
            if not fpath:
                continue
            recorded = str(row.get("sha256") or "")
            if recorded:
                current, note = sha256_now(fpath)
            else:
                # The converge itself could not hash the declared file; there is
                # no baseline, and the converge-time reason travels instead.
                current, note = "", str(row.get("note") or "unhashable at converge")
            renders.append({
                "component": name,
                "path": fpath,
                "sha256": current,
                "recorded_sha256": recorded,
                "recorded_at_unix_ms": recorded_at_ms,
                "note": note,
            })
    return renders, notes


def read_stamp(ledger_root):
    """The applied-intent stamp, plus `applied_at` from the file's MTIME.

    The stamp body deliberately carries no timestamp of its own (see the
    rollback-ledger role): a recorded one would rewrite the file every play and
    destroy the change-only signal, and it would be a claim by the run that
    INTENDED to converge rather than an observed fact. The mtime is the observed
    fact, and it moves only when the node genuinely changed generations."""
    path = os.path.join(ledger_root, STAMP_RELPATH)
    empty = {"goatnet": "", "member_group": "", "intent_content_hash": "",
             "intent_serial": 0, "intent_generated_at": "", "intent_commit": "",
             "intent_verified": False, "applied_at_unix_ms": 0}
    try:
        with open(path) as f:
            doc = json.load(f)
        mtime_ms = int(os.path.getmtime(path) * 1000)
    except (OSError, ValueError):
        return empty, "no applied-intent stamp at %s" % path
    if not isinstance(doc, dict):
        return empty, "applied-intent stamp at %s is not an object" % path
    return {
        "goatnet": doc.get("goatnet") or "",
        "member_group": doc.get("member_group") or "",
        "intent_content_hash": doc.get("intent_content_hash") or "",
        "intent_serial": int(doc.get("intent_serial") or 0),
        "intent_generated_at": doc.get("intent_generated_at") or "",
        "intent_commit": doc.get("intent_commit") or "",
        "intent_verified": bool(doc.get("intent_verified")),
        "applied_at_unix_ms": mtime_ms,
    }, ""


def build(ledger_root, host_id, docker_bin, reconcile_enabled):
    live = running_images(docker_bin) if reconcile_enabled else None
    notes = []
    if reconcile_enabled and live is None:
        notes.append("could not reconcile against running containers (docker unavailable) — "
                     "component tips are recorded intent, not observed fact")
    stamp, stamp_note = read_stamp(ledger_root)
    if stamp_note:
        notes.append(stamp_note)
    components = collect_components(ledger_root, live)
    if not components:
        notes.append("no deploy ledger under %s — this node has never been deployed by a "
                     "ledger-writing role" % ledger_root)
    renders, render_notes = collect_renders(ledger_root)
    notes.extend(render_notes)
    snapshot = dict(stamp)
    snapshot.update({
        "format": "goat-consistency-snapshot-v1",
        "host_id": host_id,
        "collected_at_unix_ms": now_ms(),
        "components": components,
        # CF-4: converge-recorded vs current hash per watched render. Facts —
        # the hand-edit verdict is the reader's, at read time.
        "renders": renders,
        # Every partial collection SAYS it is partial. A shorter list that looks
        # complete is the one output this collector must never produce.
        "collector_note": "; ".join(notes),
    })
    return snapshot


def main():
    ap = argparse.ArgumentParser(
        description="Assemble one node's convergence facts for the CF-3 publisher "
                    "(consistency-foundation.md §5.2).")
    ap.add_argument("--ledger-root", default=LEDGER_ROOT)
    ap.add_argument("--out", default=DEFAULT_OUT,
                    help="snapshot path; written atomically. `-` writes to stdout.")
    ap.add_argument("--host-id", default="")
    ap.add_argument("--docker-bin", default="docker")
    ap.add_argument("--no-reconcile", action="store_true",
                    help="skip the live-container reconciliation; every tip then reports "
                         "`unreconciled`, which a reader grades unreadable rather than green")
    args = ap.parse_args()

    snapshot = build(args.ledger_root, args.host_id or socket.gethostname(),
                     args.docker_bin, not args.no_reconcile)
    body = json.dumps(snapshot, sort_keys=True, indent=2) + "\n"

    if args.out == "-":
        sys.stdout.write(body)
        return 0
    os.makedirs(os.path.dirname(os.path.abspath(args.out)) or ".", exist_ok=True)
    tmp = args.out + ".tmp"
    with open(tmp, "w") as f:
        f.write(body)
    os.replace(tmp, args.out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
