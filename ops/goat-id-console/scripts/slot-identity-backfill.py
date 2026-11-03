#!/usr/bin/env python3
"""Backfill slots.identity_id — the person→slot linkage (ADR 1051).

Run ON the mgmt host, next to the portal and the console.

WHY THIS EXISTS
    The portal has always accepted `identity_id` on a slot and warns on every
    onboard that lands without one ("ownership for this slot will be derived by
    name join (alias<->handle), the fallback the linkage exists to retire").
    Nothing sent it until 2026-08-07, so slots minted before then carry NULL and
    ownership falls back to a name join. Both mint paths now stamp it going
    forward; this repairs the history.

WHY IT IS DRY-RUN BY DEFAULT, AND WHY IT REFUSES SO MUCH
    identity_id is an OWNERSHIP claim, and ownership is what revocation targets.
    Binding the wrong person to a namespace is materially worse than leaving the
    linkage absent — absent degrades to today's name join, wrong hands one
    person authority over another's data. So this script:

      * proposes only EXACT, UNAMBIGUOUS matches;
      * refuses any handle whose email maps to more than one Kratos identity;
      * refuses anything it cannot tie to a person at all, and says so plainly
        rather than guessing.

    That last case is the common one and is NOT an error. A cert-minted slot with
    no person behind it is usually correct: on the ZAGROS gateway the unlinked set
    is mostly machines and services (octo, azure-goat-client-01, gcp-euw3-fed,
    lab-node, svk-router), test fixtures (m090-*, best-practices-demo) and org
    slots (lithuania). Those have no Kratos person and must stay NULL.

    Free pool slots (never cert-minted) are skipped entirely — an unassigned slot
    correctly has no owner.

WHAT THE FIRST DRY RUN FOUND (ZAGROS gateway, 2026-08-07)
    256 slots; 199 live. Of those, 53 are free pool slots (never cert-minted)
    which correctly have no owner, and 146 are cert-minted — 92 already linked,
    54 not.

    Of the 54, the script proposed ZERO. That is the honest answer, not a
    failure of the script:

      * Many are not people at all — machines and services (octo,
        azure-goat-client-01, gcp-euw3-fed, lab-node, svk-router), test
        fixtures (m090-*, best-practices-demo), org slots (lithuania) and pool
        names (basilisk-alpha, echidna-alpha). These must stay NULL.
      * The rest ARE people (test-person-1, test-person-2, test-person-3,
        maintainer-operator...) but predate BOTH sources: no console proposal row and
        no operator manifest. Nothing in any system records which email that
        handle belongs to, and Kratos cannot answer it because it carries no
        handle trait.

    So the legacy set needs a HUMAN pass: an operator who knows these people
    confirms each handle→person, and the confirmed pairs get stamped. This
    script is the safe half — it identifies exactly who needs deciding, and
    stamps only what a source of truth already proves. Do not extend it to
    match on name similarity; a wrong owner is worse than no owner.

USAGE
    sudo python3 ops/goat-id-console/scripts/slot-identity-backfill.py \
        --registry /opt/goat-zenoh-portal/registry.db \
        --onboard  /var/lib/goat-id-console/onboard.db \
        --portal   http://goat-zenoh-portal:8090 \
        --token    "$PORTAL_OPERATOR_KEY" \
        [--apply]

    On a goatnet whose portal runs on Postgres (portal_store: postgres), pass
    --registry-store postgres instead of --registry; the slots are read through
    `docker exec postgres-a psql` (ops/scripts/portal_registry_pg.py, so run it
    from a repo checkout). --onboard is the console's store and unchanged here.

    Without --apply nothing is written. Review the report, then re-run with
    --apply to stamp ONLY the rows it proposed.
"""
import argparse
import glob
import json
import os
import re
import sqlite3
import sys
import urllib.error
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
import portal_registry_pg  # noqa: E402


def rows(db, sql, args=()):
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    con.row_factory = sqlite3.Row
    try:
        return [dict(r) for r in con.execute(sql, args).fetchall()]
    finally:
        con.close()


UNLINKED_SLOTS_SQL = """
        SELECT slot_id, assigned_to
          FROM slots
         WHERE revoked_at IS NULL
           AND cert_minted_at IS NOT NULL
           AND (identity_id IS NULL OR identity_id = '')
         ORDER BY assigned_to
    """


def unlinked_slots_pg(conn):
    """unlinked_slots() against the Postgres registry."""
    return portal_registry_pg.query(UNLINKED_SLOTS_SQL, **conn)


def unlinked_slots(registry):
    """Cert-minted, live, and carrying no identity — the only repair candidates.

    `cert_minted_at IS NOT NULL` is what separates a provisioned slot from a free
    pool slot; without it this would 'find' dozens of unassigned pool entries and
    report a backlog that does not exist.
    """
    return rows(registry, UNLINKED_SLOTS_SQL)


def handle_to_email(onboard):
    """handle -> email, from the console's onboarding records.

    A handle appearing with two different emails is dropped rather than
    arbitrated: it means the record itself is ambiguous about who this is.
    """
    seen, ambiguous = {}, set()
    for r in rows(onboard, "SELECT handle, email FROM onboard_proposals WHERE email <> ''"):
        h, e = (r["handle"] or "").strip(), (r["email"] or "").strip().lower()
        if not h or not e:
            continue
        if h in seen and seen[h] != e:
            ambiguous.add(h)
        seen[h] = e
    for h in ambiguous:
        seen.pop(h, None)
    return seen, ambiguous


def manifest_emails(manifest_dir):
    """handle -> email from the operator manifests (`identity.email`).

    The SECOND source, and for older slots the only one. Everyone onboarded
    before the console's three-stage system has no proposal row — which is
    exactly the population this backfill exists for, so reading only the console
    DB finds nothing for them. The manifest is the authoritative record of who a
    handle is; Kratos cannot answer it because it carries no handle trait.

    Deliberately a narrow regex rather than a YAML dependency: this runs on a
    mgmt host with a bare interpreter, and the field is a flat scalar directly
    under `identity:`.
    """
    out = {}
    if not manifest_dir:
        return out
    for path in glob.glob(os.path.join(manifest_dir, "*.yml")):
        handle = os.path.basename(path)[:-4]
        try:
            with open(path, encoding="utf-8") as fh:
                text = fh.read()
        except OSError:
            continue
        m = re.search(r"^identity:\s*$(.*?)(?=^\S|\Z)", text, re.M | re.S)
        if not m:
            continue
        em = re.search(r"^\s+email:\s*[\"']?([^\"'\s]+)", m.group(1), re.M)
        if em:
            out[handle] = em.group(1).strip().lower()
    return out


def kratos_identities(kratos_url):
    """email -> identity id. An email held by two identities is dropped."""
    out, dupes = {}, set()
    url = f"{kratos_url.rstrip('/')}/identities?per_page=1000"
    with urllib.request.urlopen(url, timeout=30) as resp:
        for ident in json.load(resp):
            addrs = ident.get("verifiable_addresses") or []
            email = (ident.get("traits", {}).get("email")
                     or (addrs[0].get("value") if addrs else "") or "").strip().lower()
            if not email:
                continue
            if email in out and out[email] != ident["id"]:
                dupes.add(email)
            out[email] = ident["id"]
    for e in dupes:
        out.pop(e, None)
    return out, dupes


def stamp(portal, token, anchor, identity_id):
    req = urllib.request.Request(
        f"{portal.rstrip('/')}/api/slots/{anchor}/identity",
        data=json.dumps({"identity_id": identity_id}).encode(),
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        method="PUT")
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.status


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--registry", default="", help="portal registry.db (sqlite store)")
    portal_registry_pg.add_arguments(ap, "--registry")
    ap.add_argument("--onboard", required=True, help="console onboard.db")
    ap.add_argument("--manifests", default="",
                    help="operator manifest dir (ops/operators/<env>) — the only "
                         "handle->email source for pre-three-stage onboards")
    ap.add_argument("--kratos", default="http://127.0.0.1:4434", help="Kratos ADMIN base")
    ap.add_argument("--portal", default="http://goat-zenoh-portal:8090")
    ap.add_argument("--token", default="", help="portal operator key (needed for --apply)")
    ap.add_argument("--apply", action="store_true", help="write the proposed linkages")
    a = ap.parse_args()

    if a.registry_store == "postgres":
        slots = unlinked_slots_pg(portal_registry_pg.kwargs(a))
    elif not a.registry:
        return ap.error("--registry is required with --registry-store sqlite")
    else:
        slots = unlinked_slots(a.registry)
    h2e, ambiguous_handles = handle_to_email(a.onboard)
    # Manifests fill in handles the console never recorded. The console wins on
    # conflict: it is the live system of record, the manifest can be stale.
    for handle, email in manifest_emails(a.manifests).items():
        h2e.setdefault(handle, email)
    e2id, dupe_emails = kratos_identities(a.kratos)

    propose, no_person, refused = [], [], []
    for s in slots:
        handle = (s["assigned_to"] or "").strip()
        email = h2e.get(handle)
        if not email:
            reason = ("handle is ambiguous in the onboarding records"
                      if handle in ambiguous_handles
                      else "no onboarding record and no manifest — machine, "
                           "service, org or fixture slot")
            no_person.append((handle, s["slot_id"], reason))
            continue
        ident = e2id.get(email)
        if not ident:
            refused.append((handle, email, "email held by MORE THAN ONE identity"
                            if email in dupe_emails else "no Kratos identity for this email"))
            continue
        propose.append((handle, s["slot_id"], email, ident))

    print(f"cert-minted live slots with no linkage: {len(slots)}\n")
    print(f"PROPOSED ({len(propose)}) — exact, unambiguous person matches")
    for h, sid, email, ident in propose:
        print(f"  {h:<28} slot {sid}  ->  {email}  ({ident})")

    print(f"\nNO PERSON ({len(no_person)}) — correctly stay NULL, not failures")
    for h, sid, why in no_person:
        print(f"  {h:<28} slot {sid}  — {why}")

    if refused:
        print(f"\nREFUSED ({len(refused)}) — a wrong binding is worse than none")
        for h, email, why in refused:
            print(f"  {h:<28} {email}  — {why}")

    if not a.apply:
        print("\nDRY RUN — nothing written. Re-run with --apply to stamp the PROPOSED rows.")
        return 0
    if not a.token:
        print("\n--apply needs --token (the portal operator key).", file=sys.stderr)
        return 2

    ok = 0
    for h, sid, _email, ident in propose:
        try:
            stamp(a.portal, a.token, sid, ident)
            ok += 1
            print(f"  stamped {h} ({sid})")
        except urllib.error.HTTPError as e:
            print(f"  FAILED {h} ({sid}): HTTP {e.code}", file=sys.stderr)
    print(f"\nstamped {ok}/{len(propose)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
