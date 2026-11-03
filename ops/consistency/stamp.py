#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# stamp.py — emit the applied-intent stamp body for one goatnet.
#
# Runs on the ANSIBLE CONTROL NODE (the deploying checkout), invoked once per
# play by the rollback-ledger shared role, which then writes the result to
# /var/lib/goat/intent/applied.json on each converging node. That file is one of
# the two inputs to the CF-3 per-node publisher; the other is the deploy ledger
# the same role already writes.
#
# Why the control node rather than the target: the stamp answers "which
# generation is this run converging you to", and only the deploying checkout
# knows that. A node cannot compute it — it has no tree — which is the whole
# reason §3.3 makes the actuator stamp provenance rather than the generator.
#
# WHAT `verified` MEANS, precisely, because the word is load-bearing:
# "every integrity check available at apply time passed." Since CF-5 that is
# two checks — the detached offline-CA signature verifies over the exact file
# bytes (ops/consistency/python/goat_consistency/signing.py, the ADR 1082
# scheme), AND the artifact's `content_hash` is the hash of its own canonical
# body. CF-5 added the signature exactly as §3.4 promised: `verified` kept its
# meaning and no reader changed. A reader treats `verified: false` as
# `unreadable` and never as `match` (ADR 1109 P14, fail-closed).
#
# Canon: docs/design/consistency-foundation.md §5.2 / §3.3 / §3.4.
#
# Usage:
#   python3 ops/consistency/stamp.py --goatnet zagros [--member-group zagros_hosts]
#
# Exit codes: 0 stamp emitted (possibly with verified=false), 2 unusable input.

import argparse
import json
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "python"))

try:
    from goat_consistency import intent as intent_mod  # noqa: E402
    from goat_consistency import signing  # noqa: E402
    from goat_consistency.paths import intent_path  # noqa: E402
except ModuleNotFoundError as e:  # X-PY-001
    sys.exit(f"stamp.py: {e} (X-PY-001). Run it with the Python that runs Ansible, "
             "which carries PyYAML, or: pip install pyyaml")

REPO_ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))


def deploying_commit():
    """The deploying checkout's HEAD — provenance, not identity (§3.3).

    The commit stays the name an operator uses in conversation; the content hash
    is what a comparison uses, because two different commits routinely produce
    identical intent and comparing on commit would grade every node diverged
    after every unrelated merge. A checkout with no git (a tarball, a courier
    copy) reports "" rather than a fabricated ref."""
    try:
        out = subprocess.run(
            ["git", "-C", REPO_ROOT, "rev-parse", "HEAD"],
            capture_output=True, text=True, timeout=10, check=False)
    except (OSError, subprocess.SubprocessError):
        return ""
    return out.stdout.strip() if out.returncode == 0 else ""


def build(goatnet, member_group):
    path = intent_path(goatnet)
    if not os.path.exists(path):
        raise SystemExit(
            "no intent artifact for goatnet '%s' at %s — a goatnet that is not "
            "governed by ops/pins/fleet-pins.yaml has no generation to stamp, "
            "and inventing one would make an ungoverned node look converged"
            % (goatnet, path))
    doc = intent_mod.read_committed(path)
    if not doc:
        raise SystemExit("%s is not readable as an IntentV1 document" % path)

    committed, recomputed = intent_mod.self_consistent(doc)
    hash_ok = bool(committed) and committed == recomputed
    sig = signing.verify_file(path, doc=doc)
    verified = hash_ok and sig.state == "verified"
    if verified:
        verification = ("signature valid (ca %s) + self-consistent content_hash"
                        % doc.get("ca_id"))
    elif not hash_ok:
        verification = ("FAILED: the artifact's content_hash is not the hash "
                        "of its own body")
    else:
        verification = "FAILED: %s" % sig.detail

    return {
        "format": "goat-applied-intent-v1",
        "goatnet": goatnet,
        "member_group": member_group or "",
        "intent_serial": doc.get("serial"),
        "intent_content_hash": doc.get("content_hash"),
        "intent_generated_at": doc.get("generated_at"),
        "intent_commit": deploying_commit(),
        # Not `applied_at`: the role owns that, because it must survive a
        # re-run that converges to the SAME generation without moving. A stamp
        # whose timestamp advanced on every play would report a node as freshly
        # converged when nothing had happened.
        "intent_verified": verified,
        "verification": verification,
    }


def main():
    ap = argparse.ArgumentParser(
        description="Emit the applied-intent stamp body for one goatnet "
                    "(consistency-foundation.md §5.2).")
    ap.add_argument("--goatnet", required=True)
    ap.add_argument("--member-group", default="")
    args = ap.parse_args()
    try:
        print(json.dumps(build(args.goatnet, args.member_group), sort_keys=True))
    except SystemExit as e:
        print(str(e), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
