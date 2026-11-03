#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# Backfill console rows for identities that only exist in the roster.
#
# THE GAP. Two paths onboard people. The console/three-stage path writes both an
# operator manifest and a console proposal row; an older `goat admin onboard`
# wrote only the manifest. Every lifecycle lane — the ADR 1136 dormancy sweep,
# the `archived` cold-storage pass, ops/identity/identity-usage.py — enumerates
# the CONSOLE store, so anyone onboarded that way is invisible to all of them.
#
# `goat admin onboard` now registers as it mints, so this is for the population
# that predates that. It is a ONE-OFF made re-runnable, not a routine job.
#
# IT READS THE COMMITTED ROSTER, NOT LOCAL MANIFESTS. `ops/operators/<env>/*.yml`
# is gitignored (it carries names and addresses), so a manifest directory
# reflects whichever mints that laptop happened to run.
# docs/operators/<env>/roster.json is the committed, PII-safe projection of that
# goatnet's own manifests and is what this enumerates.
#
# IT READS THAT GOATNET'S ROSTER, NOT THE UNION. This writes rows into a live
# console, so reading docs/operators/roster.json — the controller-side union
# across every environment — would manufacture console rows on one goatnet for
# people who only exist on another. That is identity crossing a goatnet
# boundary through a live write, which ADR 1172 forbids. The default roster
# follows --env, and --roster still overrides it for a deliberate one-off.
#
# REPORT-ONLY BY DEFAULT. --apply is required.
#
# usage:
#   backfill-console-rows.py --env <goatnet>                 # what would be written
#   backfill-console-rows.py --env <goatnet> --apply
#   backfill-console-rows.py --env <goatnet> --handle alice --apply
import argparse
import json
import os
import sys
import urllib.error
import urllib.request

REPO = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..")


def default_roster(env_name):
    """That goatnet's own committed roster (ADR 1172)."""
    return os.path.join(REPO, "docs", "operators", env_name, "roster.json")


def roster_handles(path, env_name):
    """Handles the roster declares, with whatever email-ish field it carries.

    The roster stores `email_hash`, not the address — deliberately, so a
    committed file cannot leak a mailing list. That means a backfilled row gets
    no email, and the console's duplicate-email guard cannot key on one. Stated
    here rather than worked around: a row with a handle and no email is still
    visible to every lifecycle lane, which is the whole point of the backfill,
    and the console's own registration path fills the email in when a mint
    supplies it.
    """
    # DEDUPE BY HANDLE. The roster lists a member once per org/goatnet
    # association, so 355 entries cover 245 people and 110 handles appear more
    # than once. Registering per entry would create two rows for one person —
    # and the endpoint's idempotency keys on EMAIL, which a roster-sourced row
    # does not have (the roster stores email_hash), so nothing downstream would
    # have caught it. Found in the dry run; it is exactly the duplicate the
    # collision-safety work exists to prevent (ADR 1048).
    with open(path) as source_file:
        d = json.load(source_file)
    seen, out = set(), []
    for m in d.get("members", []):
        h = (m or {}).get("handle")
        if not h or h in seen:
            continue
        seen.add(h)
        out.append({"handle": h, "status": (m or {}).get("status") or "",
                    "org": (m or {}).get("org") or ""})
    return out


def console_handles(base, key, timeout=30):
    req = urllib.request.Request(base.rstrip("/") + "/api/onboard/proposals")
    req.add_header("Authorization", "Bearer " + key)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        body = json.load(r)
    rows = body if isinstance(body, list) else (body.get("proposals") or [])
    return {(p.get("handle") or "").strip() for p in rows if p.get("handle")}


def register(base, key, handle, env_name, note, timeout=30):
    req = urllib.request.Request(
        base.rstrip("/") + "/api/onboard/register",
        data=json.dumps({"handle": handle, "email": "", "env": env_name,
                         "minted_by": "backfill", "note": note}).encode(),
        method="POST")
    req.add_header("Authorization", "Bearer " + key)
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.status, json.load(r)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--env", required=True)
    ap.add_argument(
        "--roster", default="",
        help="override the roster path (default: docs/operators/<--env>/roster.json). "
             "Never point this at docs/operators/roster.json — that is the union "
             "across every environment and this tool writes to a live console")
    ap.add_argument("--console", default=os.environ.get("CONSOLE_URL", ""))
    ap.add_argument("--handle", default="", help="just this one")
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--limit", type=int, default=0)
    a = ap.parse_args()

    key = os.environ.get("CONSOLE_API_KEY", "").strip()
    if not a.console or not key:
        print("need --console (or CONSOLE_URL) and CONSOLE_API_KEY", file=sys.stderr)
        return 2

    roster = a.roster or default_roster(a.env)
    if not os.path.exists(roster):
        print("REFUSING: no roster at %s." % roster, file=sys.stderr)
        print("  That is %s's own declared half and this tool writes rows into" % a.env, file=sys.stderr)
        print("  %s's live console, so it has nothing to enumerate. Build it with" % a.env, file=sys.stderr)
        print("    ops/operators/gen-roster.py --env %s" % a.env, file=sys.stderr)
        print("  Do NOT substitute docs/operators/roster.json: that is the union", file=sys.stderr)
        print("  across every environment (ADR 1172).", file=sys.stderr)
        return 2
    print("roster: %s" % os.path.relpath(roster, REPO))
    members = roster_handles(roster, a.env)
    try:
        have = console_handles(a.console, key)
    except Exception as e:                                     # noqa: BLE001
        # REFUSE rather than treat an unreadable console as an empty one: that
        # would make every roster handle look missing and a --apply run would
        # write hundreds of duplicate rows. Same rule as register-drift.sh.
        print("REFUSING: could not read the console's proposals (%s)." % e, file=sys.stderr)
        print("  An unreadable console reads as ZERO existing rows, which would", file=sys.stderr)
        print("  make this create a row for every person in the roster.", file=sys.stderr)
        return 2
    if not have:
        print("REFUSING: the console returned NO proposals. That is an empty or", file=sys.stderr)
        print("  wrong store, never a real answer on a live goatnet.", file=sys.stderr)
        return 2

    missing = [m for m in members if m["handle"] not in have]
    if a.handle:
        missing = [m for m in missing if m["handle"] == a.handle]
    if a.limit:
        missing = missing[:a.limit]

    print("roster: %d | console: %d | MISSING a console row: %d"
          % (len(members), len(have), len(missing)))
    if not missing:
        print("nothing to backfill")
        return 0
    if not a.apply:
        for m in missing[:40]:
            print("  would register  %s" % m["handle"])
        if len(missing) > 40:
            print("  ... and %d more" % (len(missing) - 40))
        print("\nREPORT ONLY — pass --apply to write.")
        return 0

    note = ("backfilled from docs/operators/roster.json — onboarded via the CLI "
            "path before it registered console rows")
    made = skipped = failed = 0
    for m in missing:
        try:
            code, body = register(a.console, key, m["handle"], a.env, note)
            if body.get("created"):
                made += 1
            else:
                skipped += 1
        except Exception as e:                                 # noqa: BLE001
            failed += 1
            if failed < 5:
                print("  FAILED %s — %s" % (m["handle"], e))
    print("\nregistered=%d already-present=%d failed=%d of %d"
          % (made, skipped, failed, len(missing)))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
