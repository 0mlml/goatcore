#!/usr/bin/env python3
"""Backfill Kratos authorization fields traits.{groups,org,aff,vendor} -> metadata_public.

Self-service Ory settings let an identity write any field in its traits schema,
so a user could self-grant a login group (traits.groups), which the patched
consent UI forwards into the JWT groups claim that fences every web surface —
a privilege escalation (docs/findings/F-572-*.md, confirmed on dev 2026-09-23).
The fix moves the authorization fields off self-editable traits into
admin-managed metadata_public, which the settings flow cannot write.

This script performs the data-at-rest half: for each identity it copies
traits.{groups,org,aff,vendor} into metadata_public.{...} (merging, never
clobbering a value already there) and removes the trait copy.

Deploy order (why this is safe):
  1. Deploy the metadata-first READERS first — the console (Identity.Groups/
     Org/Aff read metadata_public, fall back to traits) and the patched consent
     image (reads metadata_public.groups/aff, falls back to traits). Old data
     still resolves during the window.
  2. Run this script (--apply) to move every identity's authz fields to
     metadata_public. Kratos validates the RESULT of a PATCH, so removing the
     traits leaves email/name (+ optional legacy vendor) which validate against
     either the old or the new schema — order vs the schema deploy is flexible,
     but running this FIRST avoids a window where an identity still carrying
     traits.groups cannot save its settings under the new schema.
  3. Deploy the new identity schema (groups/org/aff removed from traits).
  4. Re-run with --verify; it reports 0 identities still carrying any authz
     trait when the migration is complete.

Runs ON the mgmt host (Kratos admin API on 127.0.0.1:4434 by default). Stdlib
only — no pip installs on the Thor. Dry-run by default; pass --apply to write.

Reversibility: --reverse moves metadata_public.{...} back to traits.{...} (only
meaningful while the traits schema still accepts those fields, i.e. before the
step 3 schema deploy). Use it to roll the data back before rolling back the
schema.

Examples:
  python3 backfill-authz-traits-to-metadata.py --verify
  python3 backfill-authz-traits-to-metadata.py                 # dry-run
  python3 backfill-authz-traits-to-metadata.py --apply         # migrate
  python3 backfill-authz-traits-to-metadata.py --reverse --apply   # roll back
"""
import argparse
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_ADMIN = "http://127.0.0.1:4434"
PAGE_SIZE = 500
# The authorization fields that must live in admin-managed metadata_public,
# never in self-editable traits. "vendor" is the legacy org
# block (ADR 1052) and rides along so it is not left self-editable either.
AUTHZ_KEYS = ("groups", "org", "aff", "vendor")


def _req(method, url, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Accept", "application/json")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=30) as resp:
        raw = resp.read()
        return (json.loads(raw) if raw else None), dict(resp.headers)


def _next_link(headers):
    link = headers.get("Link") or headers.get("link") or ""
    for part in link.split(","):
        m = re.search(r"<([^>]+)>\s*;\s*rel=\"?next\"?", part)
        if m:
            return m.group(1)
    return None


def list_identities(admin):
    """Page through the Kratos admin identities list via the Link header."""
    out = []
    url = f"{admin}/admin/identities?page_size={PAGE_SIZE}"
    seen = set()
    while url:
        batch, headers = _req("GET", url)
        if batch:
            out.extend(batch)
        nxt = _next_link(headers)
        if not nxt:
            break
        url = urllib.parse.urljoin(admin, nxt)
        tok = urllib.parse.parse_qs(urllib.parse.urlparse(url).query).get("page_token", [""])[0]
        if tok in seen or not batch or len(batch) < PAGE_SIZE:
            break
        seen.add(tok)
    return out


def _nonempty(v):
    """True if v is a value worth moving (non-empty array/object/string)."""
    if isinstance(v, (list, dict, str)):
        return len(v) > 0
    return v is not None


def plan(identities, src_container, dst_container, forward):
    """Return actions moving AUTHZ_KEYS from src_container to dst_container.

    An action carries the merged dst object and the list of keys to remove from
    src. Which value survives depends on the direction:

    - forward (traits -> metadata_public): a key already in metadata_public is
      kept, even when empty. Once metadata_public carries a groups key the
      identity is migrated, and any authorization value in traits was written by
      the holder through the settings flow after that; it is removed, never
      copied, so a re-run cannot turn a self-grant into an admin-managed one
      
    - reverse (metadata_public -> traits): the admin-managed value always wins
      over whatever traits hold.

    Forward, an identity with no groups anywhere still gets an explicit
    `groups: []` written to dst, so a metadata-first reader always finds a
    groups key and never falls back to a self-edited traits value during the
    migration window.
    """
    actions, skipped = [], []
    for i in identities:
        src = i.get(src_container) or {}
        dst = dict(i.get(dst_container) or {})
        ident = {"id": i["id"], "email": (i.get("traits") or {}).get("email", "?")}
        migrated = forward and "groups" in dst
        move_keys = [k for k in AUTHZ_KEYS if k in src]
        moved = []
        for k in move_keys:
            if migrated:
                moved.append(f"{k}(DROPPED: written to traits after migration)")
            elif forward and k in dst:
                moved.append(f"{k}(kept existing)")
            else:
                dst[k] = src[k]
                moved.append(k)
        seeded = False
        if forward and "groups" not in dst:
            dst["groups"] = []
            seeded = True
        if not move_keys and not seeded:
            skipped.append({**ident, "why": f"nothing to move and groups already present in {dst_container}"})
            continue
        if seeded:
            moved.append("groups=[] (seed)")
        actions.append({**ident, "dst": dst, "remove": move_keys, "moved": moved})
    return actions, skipped


def apply_action(admin, act, src_container, dst_container):
    """JSON Patch: set the merged dst container, remove each moved src field."""
    patch = [{"op": "add", "path": f"/{dst_container}", "value": act["dst"]}]
    for k in act["remove"]:
        patch.append({"op": "remove", "path": f"/{src_container}/{k}"})
    _req("PATCH", f"{admin}/admin/identities/{act['id']}", patch)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--admin-url", default=DEFAULT_ADMIN, help=f"Kratos admin API (default {DEFAULT_ADMIN})")
    ap.add_argument("--apply", action="store_true", help="perform writes (default: dry-run)")
    ap.add_argument("--reverse", action="store_true", help="move metadata_public -> traits (rollback)")
    ap.add_argument("--verify", action="store_true", help="report identity counts by field state and exit")
    args = ap.parse_args()

    src_container, dst_container = (
        ("metadata_public", "traits") if args.reverse else ("traits", "metadata_public")
    )

    try:
        identities = list_identities(args.admin_url)
    except urllib.error.URLError as e:
        print(f"ERROR: cannot reach Kratos admin at {args.admin_url}: {e}", file=sys.stderr)
        return 2

    if args.verify:
        def has_authz(container):
            c = container or {}
            return any(k in c and _nonempty(c[k]) for k in AUTHZ_KEYS)
        in_traits = sum(1 for i in identities if has_authz(i.get("traits")))
        in_meta = sum(1 for i in identities if has_authz(i.get("metadata_public")))
        no_meta_groups = sum(1 for i in identities
                             if "groups" not in (i.get("metadata_public") or {}))
        print(f"identities:                        {len(identities)}")
        print(f"authz fields in traits:            {in_traits}")
        print(f"authz fields in metadata_public:   {in_meta}")
        print(f"missing metadata_public.groups:    {no_meta_groups}")
        ready = in_traits == 0 and no_meta_groups == 0
        print("READY — no authz fields in traits and every identity has metadata_public.groups"
              if ready else
              f"NOT ready — traits authz on {in_traits}, missing metadata groups on {no_meta_groups}")
        return 0 if ready else 1

    actions, skipped = plan(identities, src_container, dst_container, forward=not args.reverse)
    mode = "APPLY" if args.apply else "DRY-RUN"
    print(f"[{mode}] {src_container} -> {dst_container}: {len(actions)} to migrate, "
          f"{len(skipped)} skipped (of {len(identities)} identities)\n")

    ok = err = 0
    for a in actions:
        line = f"  {a['email']:40s} move={','.join(a['moved'])}"
        if not args.apply:
            print(line)
            continue
        try:
            apply_action(args.admin_url, a, src_container, dst_container)
            ok += 1
            print(f"OK{line}")
        except urllib.error.HTTPError as e:
            err += 1
            print(f"FAIL {a['email']}: HTTP {e.code} {e.read().decode()[:200]}", file=sys.stderr)

    if args.apply:
        print(f"\napplied: {ok} ok, {err} failed")
        return 1 if err else 0
    print("\n(dry-run — re-run with --apply to write)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
