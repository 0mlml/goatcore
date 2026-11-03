#!/usr/bin/env python3
"""Backfill the Kratos identity trait traits.vendor -> traits.org (ADR 1052 Phase 4).

The vendor->org rename finished the portal wire + manifest surfaces but deferred
the Ory identity trait to Phase 4 (see docs/adr/1052-vendor-to-org-machine-rename.md
and docs/design/vendor-to-org-machine-rename.md). This script performs the
data-at-rest half: it moves each identity's legacy `traits.vendor.{short_tag,
display_name}` into `traits.org.{...}` and removes the legacy block.

Deploy order (why this script is safe):
  1. Deploy the org-preferring READERS first (portal + console images that read
     traits.org, fall back to traits.vendor). Old data still resolves.
  2. Deploy the transition SCHEMA with ory_site_vendor_trait_compat=true (the
     default) so BOTH traits.org and traits.vendor validate.
  3. Run this script (--apply) to move every identity vendor -> org.
  4. Re-run with --verify; when it reports 0 identities carrying traits.vendor,
     flip ory_site_vendor_trait_compat=false and redeploy the schema. That drop
     removes the legacy fields from the settings UI (the whole point).

Runs ON the mgmt host (Kratos admin API on 127.0.0.1:4434 by default). Stdlib
only — no pip installs on the Thor. Dry-run by default; pass --apply to write.

Reversibility: --reverse moves traits.org -> traits.vendor (only meaningful while
the compat schema still accepts vendor, i.e. before step 4's flip). Use it to
roll back the data before rolling back the schema.

Examples:
  # on the mgmt host, over the mesh:
  #   ssh <user>@<mgmt-host> 'python3 - --verify' < backfill-vendor-to-org-trait.py
  python3 backfill-vendor-to-org-trait.py --verify
  python3 backfill-vendor-to-org-trait.py                 # dry-run: show planned changes
  python3 backfill-vendor-to-org-trait.py --apply         # perform the migration
  python3 backfill-vendor-to-org-trait.py --reverse --apply   # roll data back
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


def _req(method, url, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Accept", "application/json")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=30) as resp:
        raw = resp.read()
        headers = dict(resp.headers)
        return (json.loads(raw) if raw else None), headers


def _next_link(headers):
    """Extract the rel="next" URL path from the Kratos Link header, or None.

    Kratos admin identity listing is keyset-paginated: the response carries a
    Link header whose rel="next" entry holds the next page_token. The final
    page has no rel="next". (An offset-style `&page=N` does NOT work — page=1
    returns empty — so we must follow the header token.)
    """
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
    seen_tokens = set()
    while url:
        batch, headers = _req("GET", url)
        if batch:
            out.extend(batch)
        nxt = _next_link(headers)
        if not nxt:
            break
        # nxt is a path (/admin/identities?...); resolve against the admin base.
        url = urllib.parse.urljoin(admin, nxt)
        # Guard against a self-referential/looping token (Kratos returns a
        # rel="next" even on the last page in some versions; stop if the token
        # repeats or the batch was short).
        tok = urllib.parse.parse_qs(urllib.parse.urlparse(url).query).get("page_token", [""])[0]
        if tok in seen_tokens or not batch or len(batch) < PAGE_SIZE:
            break
        seen_tokens.add(tok)
    return out


def _nonempty(d):
    """Keep only present, non-empty string sub-keys of an org/vendor block."""
    if not isinstance(d, dict):
        return {}
    return {k: v for k in ("short_tag", "display_name")
            if isinstance((v := d.get(k)), str) and v != ""}


def plan(identities, src_key, dst_key):
    """Return (actions, skipped, conflicts). An action moves src_key -> dst_key."""
    actions, skipped, conflicts = [], [], []
    for i in identities:
        traits = i.get("traits") or {}
        src = _nonempty(traits.get(src_key))
        dst = _nonempty(traits.get(dst_key))
        ident = {"id": i["id"], "email": traits.get("email", "?")}
        if not src:
            # nothing to move; if a stale empty src object exists, still remove it
            if isinstance(traits.get(src_key), dict) and not dst:
                skipped.append({**ident, "why": f"{src_key} present but empty; no {dst_key} to build"})
            else:
                skipped.append({**ident, "why": f"no {src_key} block (already migrated or n/a)"})
            continue
        if dst and dst != src:
            conflicts.append({**ident, dst_key: dst, src_key: src})
            # dst wins (prefer the target noun); still drop the legacy src
            actions.append({**ident, "value": dst, "note": "conflict: kept existing " + dst_key})
            continue
        actions.append({**ident, "value": src})
    return actions, skipped, conflicts


def apply_action(admin, act, src_key, dst_key):
    """JSON Patch: set traits.<dst_key> and remove traits.<src_key>."""
    patch = [
        {"op": "add", "path": f"/traits/{dst_key}", "value": act["value"]},
        {"op": "remove", "path": f"/traits/{src_key}"},
    ]
    _req("PATCH", f"{admin}/admin/identities/{act['id']}", patch)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--admin-url", default=DEFAULT_ADMIN, help=f"Kratos admin API (default {DEFAULT_ADMIN})")
    ap.add_argument("--apply", action="store_true", help="perform writes (default: dry-run)")
    ap.add_argument("--reverse", action="store_true", help="move org -> vendor (rollback direction)")
    ap.add_argument("--verify", action="store_true", help="just report identity counts by trait state and exit")
    args = ap.parse_args()

    src_key, dst_key = ("org", "vendor") if args.reverse else ("vendor", "org")

    try:
        identities = list_identities(args.admin_url)
    except urllib.error.URLError as e:
        print(f"ERROR: cannot reach Kratos admin at {args.admin_url}: {e}", file=sys.stderr)
        return 2

    if args.verify:
        with_vendor = sum(1 for i in identities if (i.get("traits") or {}).get("vendor"))
        with_org = sum(1 for i in identities if (i.get("traits") or {}).get("org"))
        both = sum(1 for i in identities
                   if (i.get("traits") or {}).get("vendor") and (i.get("traits") or {}).get("org"))
        print(f"identities:        {len(identities)}")
        print(f"traits.vendor:     {with_vendor}")
        print(f"traits.org:        {with_org}")
        print(f"both (unmigrated): {both}")
        print("READY to flip ory_site_vendor_trait_compat=false"
              if with_vendor == 0 else
              f"NOT ready — {with_vendor} identities still carry traits.vendor")
        return 0

    actions, skipped, conflicts = plan(identities, src_key, dst_key)
    mode = "APPLY" if args.apply else "DRY-RUN"
    direction = f"{src_key} -> {dst_key}"
    print(f"[{mode}] {direction}: {len(actions)} to migrate, {len(skipped)} skipped, "
          f"{len(conflicts)} conflicts (of {len(identities)} identities)\n")

    for c in conflicts:
        print(f"  CONFLICT {c['email']}: {dst_key}={c[dst_key]} vs {src_key}={c[src_key]} "
              f"-> keeping {dst_key}")
    if conflicts:
        print()

    ok = err = 0
    for a in actions:
        note = f"  ({a['note']})" if a.get("note") else ""
        line = f"  {a['email']:40s} {dst_key}={a['value']}{note}"
        if not args.apply:
            print(line)
            continue
        try:
            apply_action(args.admin_url, a, src_key, dst_key)
            ok += 1
            print(f"OK{line}")
        except urllib.error.HTTPError as e:
            err += 1
            print(f"FAIL {a['email']}: HTTP {e.code} {e.read().decode()[:200]}", file=sys.stderr)

    if args.apply:
        print(f"\napplied: {ok} ok, {err} failed")
        return 1 if err else 0
    print(f"\n(dry-run — re-run with --apply to write {len(actions)} changes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
