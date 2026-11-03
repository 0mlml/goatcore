#!/usr/bin/env python3
"""sync-org-catalog — materialize the console org registry into the committed
catalog (ADR 1112, docs/design/org-hierarchy-and-dedup.md §3).

The goat-id-console org store (orgs + effective-dated parent edges + aliases)
is authoritative; this script fetches GET /api/org-registry/export and writes
`ops/identity/orgs/<env>.yaml` — the diffable, auditable, committed projection
that `gen-roster.py --org-source store` reads. Same split-brain discipline as
sync-onboarded-manifests.py: the live store is the record, the repo carries a
deterministic materialization.

Auth: $<ENV>_CONSOLE_ONBOARD_KEY / $CONSOLE_ONBOARD_KEY (an
ONBOARD_OPERATOR_API_KEYS value — the org-registry realm's bearer).
URL: $<ENV>_CONSOLE_URL / $CONSOLE_URL.

Usage:
  ops/scripts/sync-org-catalog.py --env zagros [--check]

--check exits 1 when the committed catalog differs from the live export
(drift gate; writes nothing).
"""

from __future__ import annotations

import argparse
import json
import os
import re
import ssl
import sys
import urllib.error
import urllib.request
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parent.parent.parent
CATALOG_DIR = REPO / "ops" / "identity" / "orgs"


def env_slug(env: str) -> str:
    """Goatnet name -> shell-identifier prefix.

    `--env zagros-prod` used to look for `ZAGROS-PROD_CONSOLE_ONBOARD_KEY`, which is
    not a valid shell identifier -- bash refuses to export it ("not a valid
    identifier"), so the per-env lookup could never resolve for any hyphenated
    goatnet and silently fell through to the bare fallback.
    """
    return re.sub(r"[^A-Z0-9]", "_", env.upper())


def env_first(env: str, *names: str) -> str:
    for var in [f"{env_slug(env)}_{n}" for n in names] + list(names):
        v = os.environ.get(var, "").strip()
        if v:
            return v
    return ""


def console_base(env: str) -> str:
    v = env_first(env, "CONSOLE_URL")
    if v:
        return v.rstrip("/")
    sys.exit(f"no console URL in ${env_slug(env)}_CONSOLE_URL / $CONSOLE_URL")


def console_key(env: str) -> str:
    v = env_first(env, "CONSOLE_ONBOARD_KEY")
    if v:
        return v
    sys.exit(f"no console onboard key in ${env_slug(env)}_CONSOLE_ONBOARD_KEY / $CONSOLE_ONBOARD_KEY")


def fetch_catalog(base: str, key: str) -> dict:
    req = urllib.request.Request(
        base + "/api/org-registry/export",
        headers={"Authorization": "Bearer " + key},
    )
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE  # mesh-internal, transport pinned elsewhere
    # Exit 2 = "the source could not be read", never taken as clean, and never
    # confused with exit 1 = "the condition the guard exists to catch". Without
    # this an unreachable console raised URLError and Python exited 1 -- exactly
    # what --check returns for real drift -- so a dead console read as a drifted
    # catalog and inverted the guard. Contract: live-state-guard-scheduling.md.
    try:
        with urllib.request.urlopen(req, timeout=30, context=ctx) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as exc:
        print(f"UNREADABLE: console answered HTTP {exc.code} for /api/org-registry/export "
              f"({exc.reason}) -- not a drift verdict", file=sys.stderr)
        raise SystemExit(2)
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        print(f"UNREADABLE: console at {base} is not reachable ({exc}) -- not a drift verdict",
              file=sys.stderr)
        raise SystemExit(2)
    except json.JSONDecodeError as exc:
        print(f"UNREADABLE: console returned a non-JSON body ({exc}) -- not a drift verdict",
              file=sys.stderr)
        raise SystemExit(2)


def render_catalog(cat: dict, env: str) -> str:
    """Deterministic YAML: stable key order, orgs sorted by id (the export is
    already sorted; sort again so the committed artifact never depends on
    server behavior)."""
    orgs = sorted(cat.get("orgs", []), key=lambda o: o["id"])
    doc = {
        "_generated": (
            "by ops/scripts/sync-org-catalog.py — DO NOT hand-edit. "
            "The goat-id-console org store is authoritative (ADR 1112); "
            "re-run the sync to update."
        ),
        "schema_version": cat.get("schema_version", 1),
        "env": env,
        "orgs": [
            {
                k: v
                for k, v in (
                    ("id", o.get("id")),
                    ("display_name", o.get("display_name") or None),
                    ("kind", o.get("kind") or None),
                    # ADR 1151's target block. Omitted here, the committed
                    # catalog could not round-trip the store it exists to make
                    # recoverable: sector/gov_branch lived only in the live
                    # SQLite, and a rebuild from the repo would have silently
                    # re-derived sector from the deprecated kind and dropped
                    # gov_branch entirely.
                    ("sector", o.get("sector") or None),
                    ("gov_branch", o.get("gov_branch") or None),
                    ("nation", o.get("nation") or None),
                    # ADR 1171. Same round-trip argument as sector/gov_branch
                    # above: composition lives only in the live SQLite, and a
                    # catalog that omitted it could not rebuild the store — and,
                    # worse, the org-addressed wrap intersection would read every
                    # multinational org as national and refuse its members.
                    ("composition", o.get("composition") or None),
                    ("echelon_hint", o.get("echelon_hint") or None),
                    ("provenance", o.get("provenance")),
                    ("status", o.get("status")),
                    ("parent", o.get("parent") or None),
                    ("aliases", sorted(o.get("aliases") or []) or None),
                )
                if v is not None
            }
            for o in orgs
        ],
    }
    return yaml.safe_dump(doc, sort_keys=False, allow_unicode=True, default_flow_style=False)


def main() -> int:
    ap = argparse.ArgumentParser(description="Materialize the console org registry into ops/identity/orgs/<env>.yaml")
    ap.add_argument("--env", required=True, help="goatnet (zagros / zagros-prod / ...)")
    ap.add_argument("--check", action="store_true", help="exit 1 if the committed catalog drifts from the live export")
    args = ap.parse_args()

    cat = fetch_catalog(console_base(args.env), console_key(args.env))
    rendered = render_catalog(cat, args.env)
    out = CATALOG_DIR / f"{args.env}.yaml"

    if args.check:
        current = out.read_text() if out.exists() else ""
        if current != rendered:
            print(f"DRIFT: {out} differs from the live org registry — run sync-org-catalog.py --env {args.env}", file=sys.stderr)
            return 1
        print(f"OK: {out} matches the live org registry ({len(cat.get('orgs', []))} orgs)")
        return 0

    CATALOG_DIR.mkdir(parents=True, exist_ok=True)
    out.write_text(rendered)
    print(f"wrote {out} ({len(cat.get('orgs', []))} orgs)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
