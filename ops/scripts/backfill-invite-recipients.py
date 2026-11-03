#!/usr/bin/env python3
"""Backfill the invite delivery record from the console's audit trail.

The console began recording an invite's recipient on the invite row itself with
#2728 (`candidate_email` / `sent_at` / `sent_via`). Every invite minted before
that image deployed carries a blank address, so the onboarding queue's Invited
tab shows "no address recorded" for links that were, in fact, emailed to a named
person — the address was read from the mint form, used for the send, and dropped.

It was not lost. The send is audited as `onboard_email_invite` (or
`onboard_email_download` for an .eml handed to the operator's own mail client),
and that row carries both the invite id and the recipient. This script replays
those rows onto the invite table.

What it will NOT do:

  * Overwrite an address already on a row. The row is the newer, authoritative
    record; the audit trail is the older, weaker one.
  * Invent a delivery. An invite with no successful send in the audit trail is
    left blank, because that is the truth — the sponsor copied the link and
    handed it over, and no address was ever collected. Roughly a third of the
    ZAGROS backlog is this case, and it must keep reading "no address recorded"
    rather than acquiring a plausible-looking one.
  * Claim receipt. `sent_via` records the route the console released the link
    by, exactly as the live path does: `email` means this console sent it,
    `eml` means it produced a message file for someone else to send.

Runs on the mgmt host, where both files live. Dry-run by default.

    sudo ops/scripts/backfill-invite-recipients.py                 # plan
    sudo ops/scripts/backfill-invite-recipients.py --apply         # write

Paths default to the container's bind-mounted host paths; override for a
different goatnet layout with --db / --audit.
"""

import argparse
import json
import re
import sqlite3
import sys
from datetime import datetime, timezone

DEFAULT_DB = "/var/lib/goat-id-console/onboard.db"
DEFAULT_AUDIT = "/var/lib/goat-id-console/audit.jsonl"

# audit action -> the sent_via value the live console writes for that route.
ROUTE = {
    "onboard_email_invite": "email",
    "onboard_email_download": "eml",
}


# The audit writer emits Go's RFC3339Nano — NINE fractional digits. Python's
# fromisoformat only learned to accept more than six in 3.11, so parsing the
# string as-is works on the Thor (3.12) and raises on the prod Orin (3.10,
# Ubuntu-for-Tegra 22.04). Normalising the fraction to six digits keeps one
# code path across the fleet; do not "simplify" this back to fromisoformat.
_TS = re.compile(r"^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:?\d{2})$")


def parse_ts(ts: str) -> int:
    """RFC3339, any fractional precision -> unix seconds."""
    m = _TS.match(ts.strip())
    if not m:
        raise ValueError(f"unrecognised timestamp {ts!r}")
    base, frac, offset = m.groups()
    frac = (frac or "0")[:6].ljust(6, "0")
    if offset == "Z":
        offset = "+00:00"
    elif ":" not in offset:
        offset = offset[:-2] + ":" + offset[-2:]
    return int(datetime.fromisoformat(f"{base}.{frac}{offset}").astimezone(timezone.utc).timestamp())


def read_audit(path):
    """invite_id -> (recipient, sent_at, sent_via) from the LATEST ok send.

    A re-send supersedes an earlier one, matching the live path: the row carries
    the most recent hand-over, and the per-send history stays in the audit trail.
    """
    out = {}
    malformed = 0
    undated = 0
    earliest = None
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                malformed += 1
                continue
            # Coverage is a property of the WHOLE trail, not of send rows only:
            # any audited action proves the trail was recording at that moment.
            try:
                seen = parse_ts(ev["ts"])
            except (KeyError, ValueError):
                seen = None
            if seen is not None and (earliest is None or seen < earliest):
                earliest = seen
            via = ROUTE.get(ev.get("action", ""))
            if via is None or ev.get("result") != "ok":
                continue
            invite = (ev.get("target") or {}).get("invite_token_prefix", "")
            to = ((ev.get("after") or {}).get("to") or "").strip()
            if not invite.startswith("inv-") or not to:
                continue
            try:
                at = parse_ts(ev["ts"])
            except (KeyError, ValueError):
                # A send we cannot date is a send we cannot report as absent.
                undated += 1
                continue
            prev = out.get(invite)
            if prev is None or at >= prev[1]:
                out[invite] = (to, at, via)
    return out, malformed, undated, earliest


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--db", default=DEFAULT_DB, help=f"onboarding store (default {DEFAULT_DB})")
    ap.add_argument("--audit", default=DEFAULT_AUDIT, help=f"audit trail (default {DEFAULT_AUDIT})")
    ap.add_argument("--apply", action="store_true", help="write the rows (default: plan only)")
    args = ap.parse_args()

    try:
        sends, malformed, undated, earliest = read_audit(args.audit)
    except FileNotFoundError:
        print(f"FATAL: no audit trail at {args.audit}", file=sys.stderr)
        return 2
    if malformed:
        print(f"note: skipped {malformed} unparseable audit line(s)")
    if undated:
        # Loud, because the failure mode is a FALSE NEGATIVE: dropped sends make
        # the run report "never emailed" for invites that were, in fact, emailed.
        # That is indistinguishable from the honest answer unless it is said here.
        print(f"WARNING: {undated} send(s) carried a timestamp this Python could not "
              f"read and were DROPPED. The 'never emailed' count below is therefore a "
              f"floor, not a finding — do not conclude an invite was never sent.",
              file=sys.stderr)
    print(f"audit: {len(sends)} invite(s) with a recoverable recipient")

    db = sqlite3.connect(args.db)
    cols = {r[1] for r in db.execute("PRAGMA table_info(onboard_invites)")}
    missing = {"candidate_email", "sent_at", "sent_via"} - cols
    if missing:
        # The console applies these on startup; a missing column means this host
        # still runs a pre-#2728 image and writing would fail mid-way.
        print(f"FATAL: onboard_invites lacks {sorted(missing)} — deploy the console "
              "image that adds them before backfilling", file=sys.stderr)
        return 2

    rows = list(db.execute(
        "SELECT id, status, candidate_email, created_at FROM onboard_invites ORDER BY created_at"))
    fillable, already, never_sent, uncovered = [], 0, 0, 0
    for invite_id, status, existing, created in rows:
        if existing:
            already += 1
            continue
        hit = sends.get(invite_id)
        if hit is None:
            # Blank, and the reason matters. Inside the trail's window the
            # absence of a send row IS evidence; before it, absence is silence.
            if earliest is not None and created is not None and created < earliest:
                uncovered += 1
            else:
                never_sent += 1
            continue
        fillable.append((invite_id, status, hit))

    if earliest is not None:
        since = datetime.fromtimestamp(earliest, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        print(f"audit trail covers from {since}")
    print(f"invites: {len(rows)} total · {already} already addressed · "
          f"{len(fillable)} fillable · {never_sent} never emailed · "
          f"{uncovered} predate the trail (unknowable)")

    by_status = {}
    for _, status, _ in fillable:
        by_status[status] = by_status.get(status, 0) + 1
    if by_status:
        print("  fillable by status: " + ", ".join(f"{k}={v}" for k, v in sorted(by_status.items())))

    for invite_id, status, (to, at, via) in fillable[:10]:
        when = datetime.fromtimestamp(at, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        print(f"  {invite_id}  {status:<9}  {to}  ({via} at {when})")
    if len(fillable) > 10:
        print(f"  … and {len(fillable) - 10} more")

    if not args.apply:
        print("\ndry run — nothing written. Re-run with --apply.")
        return 0
    if not fillable:
        print("\nnothing to write.")
        return 0

    with db:
        db.executemany(
            "UPDATE onboard_invites SET candidate_email = ?, sent_at = ?, sent_via = ? "
            "WHERE id = ? AND (candidate_email IS NULL OR candidate_email = '')",
            [(to, at, via, invite_id) for invite_id, _, (to, at, via) in fillable])
    print(f"\nwrote {len(fillable)} invite(s). {never_sent} left blank because no send "
          f"was recorded while the trail was running — for those, a link handed over "
          f"out of band is the honest answer. A further {uncovered} predate the trail "
          f"entirely: their recipients are not recoverable from here, and they are NOT "
          f"evidence that nothing was sent.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
