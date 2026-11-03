#!/usr/bin/env python3
"""deployed-vs-declared.py — is the fleet actually running what main declares?

The gap this closes
-------------------
The consistency surface grades a node's DECLARED state against its APPLIED
state, and both are files that only a deploy writes:

    /etc/consistency-console/intent.json   declared — copied from the
                                           deploying checkout
    /var/lib/goat/intent/applied.json      applied — written by the same
                                           deploy, via rollback-ledger

So a change that merges and is never deployed moves NEITHER. They agree, and
the surface grades green. On-host declared-vs-applied is structurally
incapable of seeing declared-but-undeployed; it is really applied-vs-applied.
The role that places the artifact says as much, and names ADR 1109 FM-2 (a
node fetching its own intent from a site-local replica) as the eventual fix.

This script is the interim: it is the one reader that holds BOTH the current
checkout (what main declares now) and the host (what is actually running), and
compares them. It needs no new design and no fabric — the data already exists.

It finds pins merged and never deployed (host one image behind), and intent
artifacts stale by several serials.

How the join works
------------------
`ops/pins/fleet-pins.yaml` already carries the join key: each component
declares `var` (the Ansible variable holding its pinned image) and
`ledger_component` (the directory under /var/lib/goat/ where the ADR 1060
deploy ledger for it lives). So:

    declared = group_vars/<member_group>/pins.yml : <var>
    running  = /var/lib/goat/<ledger_component>/deploy-history.jsonl (tip).ref

Comparison is on the @sha256: DIGEST, never the whole ref. The pin carries
`{{ ghcr_org }}` unresolved and the host's copy has it resolved to one of two
namespaces (usareur-af / goat-maintainer) — comparing whole strings would report drift
on every row. The digest is the identity; the namespace is transport.

The ledger tip is a claim, not an observation
--------------------------------------------
A `deploy-history.jsonl` records that a deploy RAN. It is not evidence that the
thing is still running, and it is never removed when a component is taken off a
host. So a ledger directory OUTLIVES the deployment, and reading tips alone
grades a component that is not on the host at all — `ok` when the stale tip
happens to match the pin, `DRIFT` when it does not. Both are inventions.

A pinned row can describe a component with no container there, running or
stopped, and read `DRIFT` or `ok` either way.

So every image row is falsified against the host's running containers in the
same pass, by the same rule `ops/consistency/collect/consistency-snapshot.py`
already applies on-node (`reconcile()`): match on the whole ref, on the digest,
or on the bare image name. Nothing running by that name means the ledger is a
remnant, and the row says so instead of grading.

Honesty
-------
Unreachable, missing ledger, unparseable tip, or a ledger with nothing running
behind it is `indeterminate` — NEVER `ok`. "I could not check" must not read as
"it is fine"; that conflation is the whole failure this script exists to end.
Exit codes distinguish them. If the container list itself cannot be read, every
row says so rather than quietly falling back to the ledger's word.

Usage
-----
    ops/scripts/deployed-vs-declared.py --goatnet site_a \
        --host operator@mesh-host.example

    # Through an env door (ops/scripts/env-door.sh <env>):
    ops/scripts/deployed-vs-declared.py --goatnet site_a \
        --host operator@127.0.0.1 --port 12201

Exit: 0 all matched · 1 drift found · 2 nothing drifted but something was
indeterminate · 3 usage/config error.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
FLEET_PINS = REPO / "ops" / "pins" / "fleet-pins.yaml"
GROUP_VARS = REPO / "ops" / "ansible" / "group_vars"
INTENT = REPO / "ops" / "consistency" / "intent"

DIGEST_RE = re.compile(r"@(sha256:[0-9a-f]{64})")
# Any tag, not just `sha-…`: several components pin an upstream version
# (`:1.9.0@sha256:…`) and showing those as "-" made a correct row look like a
# missing one.
TAG_RE = re.compile(r"^(?:[^@]*/)?[^/@:]+:([^@\s]+)")


def digest_of(ref: str | None) -> str | None:
    if not ref:
        return None
    m = DIGEST_RE.search(ref)
    return m.group(1) if m else None


def tag_of(ref: str | None) -> str:
    if not ref:
        return "-"
    m = TAG_RE.search(ref.strip())
    if m:
        return m.group(1)
    # Digest-only pin (no tag at all) — show a short digest, never "-".
    d = digest_of(ref)
    return d[7:19] if d else "-"


def image_name(ref: str | None) -> str | None:
    """The bare image name — the last path segment before any tag or digest.

    `ghcr.io/{{ ghcr_org }}/goat-id-console:sha-x@sha256:y` -> `goat-id-console`
    `oryd/kratos:v1.3.1`                                    -> `kratos`

    Needed because `ledger_component` is NOT one-to-one with a pin var. Several
    pins can name a component whose ADR 1060 ledger is SHARED for a whole stack,
    and that ledger's ref is the stack's primary artifact, not each pin's image.
    `kratos-selfservice-ui` maps to `ledger_component: ory`, whose tip is
    `oryd/kratos:v1.3.1` — the Kratos SERVER, while the pin is the selfservice
    UI. Comparing those two reported a false "unpinned host" until this guard
    existed. If the names differ, this ledger does not track this pin and the
    row is not comparable — say so, never grade it.
    """
    if not ref:
        return None
    head = ref.split("@", 1)[0]
    seg = head.rsplit("/", 1)[-1]
    return seg.split(":", 1)[0] or None


def load_yaml(path: Path):
    try:
        import yaml
    except ImportError:
        sys.exit("PyYAML is required: pip install pyyaml")
    with path.open() as fh:
        return yaml.safe_load(fh)


def ssh(host: str, port: int, remote_cmd: str, become_pass: str | None) -> tuple[int, str]:
    """Run one command on the host. Returns (rc, stdout).

    Uses a single non-interactive SSH per call. Never interpolates a value
    into a position where an empty variable could make the command read from
    stdin instead — the failure mode that can leak a sudo password into a
    transcript.
    """
    cmd = ["ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
           "-o", "ConnectTimeout=12", "-o", "BatchMode=yes"]
    if port:
        cmd += ["-p", str(port)]
    cmd.append(host)
    if become_pass:
        # Password reaches sudo on stdin and NOTHING downstream reads stdin.
        cmd.append(f"sudo -S -p '' sh -c {shquote(remote_cmd)}")
        p = subprocess.run(cmd, input=become_pass + "\n", capture_output=True,
                           text=True, timeout=90)
    else:
        cmd.append(f"sudo -n sh -c {shquote(remote_cmd)}")
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=90)
    return p.returncode, p.stdout


def shquote(s: str) -> str:
    return "'" + s.replace("'", "'\"'\"'") + "'"


def running_refs(host: str, port: int, become: str | None) -> list[str] | None:
    """Every image reference running on this host, or None if it could not look.

    Both the tag-ish `.Config.Image` (what compose asked for) and the resolved
    `.Image` / `RepoDigests[0]` are collected, because a ledger tip may be any
    of those forms — several goat components pin a bare image ID
    (`db5c827c09b9`) rather than a registry ref. Same collection the on-node
    collector does; see consistency-snapshot.py:running_images().

    `RepoDigests` is deliberately NOT asked for. It is an IMAGE field; a
    CONTAINER inspect has no such key, and asking for it makes the whole
    `docker inspect` fail with a template error and no stdout — which this
    reader would then read as "could not look at all". `.Config.Image` (the ref compose asked
    for, digest and all) plus `.Image` (the resolved image ID) is what a
    container actually carries.
    """
    remote = (
        'CS=$(docker ps --format "{{.Names}}") || exit 7; '
        'printf "OK\\n"; '
        '[ -z "$CS" ] || docker inspect '
        '--format "{{.Config.Image}}|{{.Image}}" $CS'
    )
    rc, out = ssh(host, port, remote, become)
    lines = out.splitlines()
    if rc != 0 or not lines or lines[0].strip() != "OK":
        return None
    refs: list[str] = []
    for line in lines[1:]:
        refs += [p for p in line.split("|") if p and not p.startswith("<")]
    return refs


def running_image_digests(host: str, port: int, become: str | None) -> list[dict] | None:
    """Every running container's compose ref, resolved image id, and the
    registry digests docker actually recorded for that image (`RepoDigests`),
    or None if the host could not be asked.

    `RepoDigests` is an IMAGE field, not a container field; asking a
    CONTAINER inspect for it breaks the whole templated call and returns
    nothing (see running_refs's own note on this). So this asks a second,
    plain `docker inspect` on each container's resolved image id, where the
    field is real — one extra round trip per container, acceptable because
    this is an interactive diagnostic, not a hot path.

    This exists because `is_running()` below only answers "is ANYTHING named
    like this running", via a loose ref/digest/bare-name match — which means
    a DIFFERENT BUILD running under the same image name reads as a match on
    name alone, and the falsifier it feeds never fires (F-547: declared A,
    the ledger's tip also says A, so the ledger-vs-pin compare says `ok`, and
    the host is actually running B — a name match, not a digest match, is
    reported as confirmation). `running_digest_sets()` below turns this into
    an actual digest comparison.
    """
    remote = (
        'CS=$(docker ps --format "{{.Names}}") || exit 7; '
        'printf "OK\\n"; '
        'for c in $CS; do '
        '  REF=$(docker inspect --format "{{.Config.Image}}|{{.Image}}" "$c" 2>/dev/null) || exit 7; '
        '  IMG=${REF##*|}; '
        '  DIGS=$(docker image inspect --format "{{json .RepoDigests}}" "$IMG" 2>/dev/null) || exit 7; '
        '  printf "%s\\t%s\\n" "$REF" "$DIGS"; '
        'done'
    )
    rc, out = ssh(host, port, remote, become)
    lines = out.splitlines()
    if rc != 0 or not lines or lines[0].strip() != "OK":
        return None
    result: list[dict] = []
    for line in lines[1:]:
        if "\t" not in line:
            return None
        ref_part, _, digs_part = line.partition("\t")
        config_image, _, image_id = ref_part.partition("|")
        digs_part = digs_part.strip()
        try:
            repo_digests = json.loads(digs_part) if digs_part else []
        except json.JSONDecodeError:
            return None
        if not config_image or not image_id or (repo_digests is not None and (not isinstance(repo_digests, list) or not all(isinstance(ref, str) for ref in repo_digests))):
            return None
        result.append({
            "config_image": config_image or None,
            "image_id": image_id or None,
            "repo_digests": repo_digests,
        })
    return result


def running_digest_sets(detail: list[dict] | None, name: str | None) -> list[set[str]] | None:
    """Keep each container's evidence separate: one matching replica cannot
    confirm another replica. An empty set is an image without digest evidence;
    None means collection failed. Multiple RepoDigests on one image are aliases.
    """
    if detail is None or not name:
        return None
    return [{digest_of(ref) for ref in c.get("repo_digests") or [] if digest_of(ref)}
            for c in detail if image_name(c.get("config_image")) == name]


def is_running(tip_ref: str | None, live: list[str] | None) -> bool | None:
    """Is the artifact this ledger tip names actually running here?

    None means the host could not be asked — which is not `False`. "I could not
    look" and "I looked and it is not there" are different facts and this
    surface keeps them apart everywhere else too.

    Match is deliberately loose — whole ref, digest, OR bare image name. A
    different BUILD of the same component is running and belongs in the drift
    comparison; only "nothing by this name is here" makes the ledger a remnant.
    """
    if live is None:
        return None
    if not tip_ref:
        return False
    head = tip_ref.split("@", 1)[0]
    want_name, want_dig = image_name(tip_ref), digest_of(tip_ref)
    for cand in live:
        if cand == tip_ref or cand == head:
            return True
        if want_dig and digest_of(cand) == want_dig:
            return True
        if want_name and image_name(cand) == want_name:
            return True
    return False


def grade_component(declared_ref: str, tip: dict | None, live: list[str] | None,
                     running_digs: list[set[str]] | None) -> str:
    """The grading decision for one component — a pure function so the
    fixture tests below can drive it without a host.

    `declared_ref` is the pin (main declares); `tip` is the ledger's tip row
    for this component's ledger directory, or None if there isn't one here;
    `live` is `running_refs()`'s loose name/tag/id list, used only for the
    "is anything by this name here at all" falsifier; `running_digs` is
    `running_digest_sets()`'s exact-digest observation for this component's
    bare name, or None if the host's images could not be read.
    """
    d_dig = digest_of(declared_ref)
    d_name = image_name(declared_ref)
    if tip is None:
        # No ledger directory for this component on this host. On a
        # multi-host goatnet that usually means it runs on a SIBLING host,
        # not that it is missing — hence not-checked, never ok.
        return "NOT ON THIS HOST"

    r_name = image_name(tip.get("ref"))
    if d_name and r_name and d_name != r_name:
        # Shared ledger: it tracks a different artifact than this pin.
        return f"LEDGER TRACKS {r_name}"
    if d_dig is None:
        return "INDETERMINATE"   # the PIN carries no digest — cannot compare

    # A row is written `action: pending` when the play starts and promoted
    # to `action: deploy` by an end-of-play success handler (ADR 1060 /
    # F-530); a tip still reading `pending` means the play never finished —
    # refused, failed, or still running — so its ref is a claim about what
    # was ATTEMPTED, not evidence of what is now running. Treat it as
    # unconfirmed rather than comparing its digest as if it were settled.
    tip_pending = tip.get("action") == "pending"
    r_dig = None if tip_pending else digest_of(tip.get("ref"))

    if tip_pending:
        state = "LEDGER PENDING, UNCONFIRMED"
    elif r_dig is None:
        # A ledger row exists but its ref carries no digest: the host is
        # running a FLOATING TAG. Distinct from "not deployed here" and
        # worse — the pin table pins a digest and the host does not, so the
        # running bytes can change with no commit anywhere.
        state = "UNPINNED ON HOST"
    elif d_dig == r_dig:
        state = "ok"
    else:
        state = "DRIFT"

    # Falsifier 1: is anything by this name on this host at all? A ledger
    # tip is a record that a deploy RAN, and nothing removes it when the
    # component leaves the host, so an `ok`/`DRIFT`/`UNPINNED ON HOST` for a
    # component that is not running at all is invented. Non-image
    # artifacts (ADR 1060 tracks packages and kernel modules too) have no
    # container to look for and keep their ledger grading.
    if (state in ("ok", "DRIFT", "UNPINNED ON HOST")
            and (tip.get("artifact_type") or "image") == "image"
            and is_running(tip.get("ref"), live) is False):
        return "LEDGER ONLY, NOT RUNNING"

    if (tip.get("artifact_type") or "image") != "image":
        return state

    # Every running replica must have evidence of the declared digest. An
    # image can have multiple repository aliases, so compare per container.
    if state in ("ok", "LEDGER PENDING, UNCONFIRMED"):
        if running_digs and any(digs and d_dig not in digs for digs in running_digs):
            return "RUNNING DRIFT"
        if running_digs and all(d_dig in digs for digs in running_digs):
            return "ok"
        if state == "ok":
            return "RUNNING UNCONFIRMED"

    return state


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--goatnet", required=True,
                    help="intent name, e.g. zagros or zagros_prod")
    ap.add_argument("--member-group", default=None,
                    help="group_vars dir (default: <goatnet>_hosts)")
    ap.add_argument("--host", required=True, help="user@address")
    ap.add_argument("--port", type=int, default=0)
    ap.add_argument("--become-pass-env", default=None,
                    help="env var holding the sudo password (NOT the password)")
    args = ap.parse_args()

    import os
    become = os.environ.get(args.become_pass_env) if args.become_pass_env else None
    if args.become_pass_env and not become:
        print(f"error: ${args.become_pass_env} is empty", file=sys.stderr)
        return 3

    group = args.member_group or f"{args.goatnet}_hosts"
    pins_path = GROUP_VARS / group / "pins.yml"
    if not pins_path.exists():
        print(f"error: no pins for member group {group} ({pins_path})", file=sys.stderr)
        return 3

    fleet = load_yaml(FLEET_PINS)
    pins = load_yaml(pins_path) or {}
    components = {k: v for k, v in (fleet.get("components") or {}).items()
                  if isinstance(v, dict) and v.get("ledger_component") and v.get("var")}

    # --- intent serial: the artifact the consistency surface grades against ---
    rows = []
    declared_serial = None
    ip = INTENT / args.goatnet / "intent.json"
    if ip.exists():
        declared_serial = json.loads(ip.read_text()).get("serial")

    rc, out = ssh(args.host, args.port, "cat /var/lib/goat/intent/applied.json", become)
    applied_serial = None
    if rc == 0 and out.strip():
        try:
            applied_serial = json.loads(out).get("intent_serial")
        except json.JSONDecodeError:
            applied_serial = None

    if declared_serial is None or applied_serial is None:
        rows.append(("intent artifact", str(declared_serial), str(applied_serial), "INDETERMINATE"))
    elif declared_serial == applied_serial:
        rows.append(("intent artifact", f"serial {declared_serial}", f"serial {applied_serial}", "ok"))
    else:
        rows.append(("intent artifact", f"serial {declared_serial}",
                     f"serial {applied_serial}", f"DRIFT ({declared_serial - applied_serial} behind)"))

    # --- per-component: declared pin vs deploy-ledger tip ---
    # One SSH for all ledgers rather than one per component.
    names = sorted({c["ledger_component"] for c in components.values()})
    # ONE line per component, name and payload separated by a tab, the line
    # ALWAYS newline-terminated by printf itself.
    #
    # The naive form (`printf name-tab; tail file`) is wrong: a
    # deploy-history.jsonl whose final line lacks a trailing newline lets the
    # NEXT component's name concatenate onto this component's payload, and the
    # tab-partition then attributes one component's row to another. That
    # can produce a false "unpinned" reading — a script that invents drift is worse than no
    # script. `tr -d` strips any embedded newline so one component can never
    # occupy two lines.
    remote = "; ".join(
        f'printf "%s\\t%s\\n" {shquote(n)} '
        f'"$(tail -n 1 /var/lib/goat/{n}/deploy-history.jsonl 2>/dev/null | tr -d "\\r\\n")"'
        for n in names)
    rc, out = ssh(args.host, args.port, remote, become)
    tips: dict[str, dict] = {}
    if rc == 0:
        for line in out.splitlines():
            if "\t" not in line:
                continue
            name, _, payload = line.partition("\t")
            payload = payload.strip()
            if not payload:
                continue
            try:
                tips[name] = json.loads(payload)
            except json.JSONDecodeError:
                pass  # skip malformed tip payload

    # The falsifier. Two more SSH calls: what is ACTUALLY running here, by
    # loose name/tag match and — because loose matching alone cannot tell a
    # different build apart from the declared one — by exact digest.
    # Without both, every row below is the ledger's word for itself.
    live = running_refs(args.host, args.port, become)
    detail = running_image_digests(args.host, args.port, become)

    for comp, spec in sorted(components.items()):
        var, ledger = spec["var"], spec["ledger_component"]
        declared_ref = pins.get(var)
        if declared_ref is None:
            continue  # not declared for this goatnet (a deviation, or absent)
        tip = tips.get(ledger)
        running_digs = running_digest_sets(detail, image_name(declared_ref))
        state = grade_component(declared_ref, tip, live, running_digs)
        rows.append((comp, tag_of(declared_ref), tag_of(tip.get("ref") if tip else None), state))

    # --- report ---
    w = max(len(r[0]) for r in rows) + 2
    print(f"{'component'.ljust(w)}{'declared (main)'.ljust(20)}{'running (host)'.ljust(20)}state")
    print("-" * (w + 46))
    drift = indet = 0
    for name, dec, run, state in rows:
        if state.startswith("DRIFT") or state == "RUNNING DRIFT" or state == "UNPINNED ON HOST":
            drift += 1
        elif (state == "INDETERMINATE" or state == "NOT ON THIS HOST"
              or state == "RUNNING UNCONFIRMED"
              or state == "LEDGER PENDING, UNCONFIRMED"
              or state.startswith("LEDGER TRACKS") or state.startswith("LEDGER ONLY")):
            indet += 1
        print(f"{name.ljust(w)}{dec.ljust(20)}{run.ljust(20)}{state}")
    print("-" * (w + 46))
    print(f"{len(rows)} checked · {drift} drifted · {indet} indeterminate · host {args.host}")
    if live is None:
        print("\nCONTAINER RECONCILIATION UNAVAILABLE — `docker ps` could not be read on\n"
              "this host, so every row above is the deploy LEDGER's claim about itself,\n"
              "not observed fact. Image rows cannot be confirmed until this can be read.")
    elif detail is None:
        print("\nDIGEST RECONCILIATION UNAVAILABLE — the per-image `docker inspect` calls\n"
              "did not come back. Matching image ledger rows remain RUNNING UNCONFIRMED.")
    if indet:
        print("\nNOT CHECKED, which is not the same as fine:\n"
              "  NOT ON THIS HOST  no ledger here — usually a sibling host on a\n"
              "                    multi-host goatnet. Re-run against it.\n"
              "  RUNNING UNCONFIRMED  one or more running image digests could not be verified.\n"
              "  INDETERMINATE     the PIN carries no digest, so nothing to compare.\n"
              "  LEDGER TRACKS x   this component shares an ADR 1060 ledger whose ref is\n"
              "                    a different artifact; that ledger cannot answer for\n"
              "                    this pin.\n"
              "  LEDGER PENDING,   the ledger's tip row is still `action: pending` — the\n"
              "  UNCONFIRMED       deploy play started and never reached its end-of-play\n"
              "                    promote (refused, failed, or still running); its ref is\n"
              "                    a claim about what was attempted, not what is running.\n"
              "  LEDGER ONLY,      a deploy ledger says this was deployed here and nothing\n"
              "  NOT RUNNING       by that name is running now. The ledger is a remnant of a\n"
              "                    host that no longer carries the component — the pin is\n"
              "                    unanswerable here, and this host is not the one to\n"
              "                    deploy it to.")
    if drift:
        print("\nNEEDS ACTION:\n"
              "  DRIFT             main declares one thing, the host runs another —\n"
              "                    usually a pin bump that merged and was never deployed.\n"
              "  RUNNING DRIFT     the pin and the ledger's tip agree with each other, but\n"
              "                    the container actually running under this image name\n"
              "                    carries a DIFFERENT digest from both — trust\n"
              "                    what is observed running over what the ledger claims.\n"
              "  UNPINNED ON HOST  the pin table pins a digest, the host runs a floating\n"
              "                    tag. The running bytes can change with no commit.")
    return 1 if drift else (2 if indet else 0)


if __name__ == "__main__":
    sys.exit(main())
