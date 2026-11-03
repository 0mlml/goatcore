#!/usr/bin/env python3
"""
Parity guard between the two places the ADR 1116 gear vocabulary lives:

  ops/ansible/netbird_acl_taxonomy.yml                        — the mesh GROUPS
  ops/goat-zenoh-portal/internal/registry/machines.go         — the MINT enum

Why this guard exists. ADR 1116 §9 says "the setup key is the enforcement point,
not this document", and the mint is what makes that true: a custodian picks a
node-class, and the class picks the NetBird groups. That only holds while the two
halves agree. If the taxonomy grows a class the mint has never heard of, the mint
refuses a legitimate machine; if the mint grows one the taxonomy lacks, it derives
a group that does not exist and the enrollment fails at the mgmt API — mid-standup,
which is the worst time to find out a vocabulary change was half-applied.

A vocabulary declared for one goatnet and not another stays silent until somebody
goes looking, so this guard compares them on every PR.

Checks (fail-closed, exit 1 on any violation):

  1. Every gear class in the Go enum is declared as a group suffix on EVERY
     goatnet that declares any of them, and vice versa.
  2. Every class the mint can DERIVE a group for maps to a suffix that actually
     exists in that goatnet's taxonomy.
  3. The mesh-group derivation never yields OPSYSTEM — the class the ADR retires.
  4. EVERY CLASS ADR 1116 §3 NAMES IS DECLARED IN THE TAXONOMY. When the taxonomy
     lacks a class §3 names, a peer with no correct class available gets the
     closest plausible word, which can be intake-only and grant no outbound reach
     at all. A taxonomy is only an enforcement point where it is COMPLETE, and
     completeness is measured against the decision, not against the other copies
     of itself.
  5. The §7 live-membership guard's NODE_CLASS set knows every gear class. It is a
     THIRD copy of the vocabulary: if it lags the taxonomy and the mint, it reports
     a correctly-classed peer as "unclassified". The copy that ENFORCES is the one
     you notice last. Skipped when the file is absent.

  5. Prefix derivation (ADR 1116 §9 mesh wiring): the portal role derives
     portal_mesh_site_prefix as `site_id | upper`, which only coincidentally
     equals the taxonomy's site_prefix. For every goatnet in
     ops/pins/fleet-pins.yaml whose mgmt group_vars declare a site_id, assert
     site_id.upper() == the taxonomy site_prefix for that site entry, so the
     role-default derivation is checked statically in CI, not only by the
     deploy-time preflight assert. Goatnets with no taxonomy entry are skipped
     with a note (their deploys assert mint stays off instead).

Deliberately NOT checked: whether the groups exist on a LIVE mgmt server. That is
netbird-acl-bootstrap.yml's job and the cutover playbook's live-group assert; a file-level
guard that claimed to know live state would be lying.

Usage:  python3 ops/ci/check-gear-class-parity.py [repo-root]
Rollback: rm this file + its workflow step.
"""

import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("FAIL: PyYAML not available (pip install pyyaml)", file=sys.stderr)
    sys.exit(2)

TAXONOMY = "ops/ansible/netbird_acl_taxonomy.yml"
MEMBERSHIP_GUARD = "ops/ci/check-node-class-membership.sh"
ADR = "docs/adr/1116-retire-opsystem-gear-classes.md"
MACHINES_GO = "ops/goat-zenoh-portal/internal/registry/machines.go"
FLEET_PINS = "ops/pins/fleet-pins.yaml"
GROUP_VARS = "ops/ansible/group_vars"

# The Go source is the mint's SoT; parse it rather than re-declaring the list
# here, so this guard cannot itself drift into being a third copy.
GEAR_BLOCK = re.compile(r"gearNodeClasses\s*=\s*map\[string\]bool\{(.*?)\n\t\}", re.S)
MAP_BLOCK = re.compile(r"nodeClassMeshGroups\s*=\s*map\[string\]\[\]string\{(.*?)\n\t\}", re.S)
QUOTED = re.compile(r'"([A-Z][A-Z0-9-]*)"')
MAP_ENTRY = re.compile(r'"([A-Z][A-Z0-9-]*)"\s*:\s*\{([^}]*)\}')


def parse_go(path: Path):
    src = path.read_text()
    gm = GEAR_BLOCK.search(src)
    if not gm:
        print(f"FAIL: could not find gearNodeClasses in {path}", file=sys.stderr)
        sys.exit(2)
    gear = set(QUOTED.findall(gm.group(1)))

    mm = MAP_BLOCK.search(src)
    if not mm:
        print(f"FAIL: could not find nodeClassMeshGroups in {path}", file=sys.stderr)
        sys.exit(2)
    # Strip comments so a class named only in prose is not read as an entry.
    body = "\n".join(l for l in mm.group(1).splitlines() if not l.strip().startswith("//"))
    derivation = {cls: set(QUOTED.findall(vals)) for cls, vals in MAP_ENTRY.findall(body)}
    return gear, derivation


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
    gear, derivation = parse_go(root / MACHINES_GO)

    doc = yaml.safe_load((root / TAXONOMY).read_text()) or {}
    taxonomy = doc.get("netbird_acl_taxonomy") or {}
    # An opted-in exercise/site scope expands to base + gear (see
    # tasks/netbird-exercise-expand.yml), so check the UNION — the add-on alone
    # legitimately lacks INFRA/NETWORK/OBSERVER, which the base template carries.
    gear_addon = {g["suffix"] for g in (doc.get("netbird_exercise_gear_template") or {}).get("groups") or []}
    gear_addon |= {g["suffix"] for g in (doc.get("netbird_exercise_template") or {}).get("groups") or []}

    errors: list[str] = []

    # Which goatnets carry the gear vocabulary at all? A site with none of it
    # is not in scope — this guard checks agreement, not adoption.
    scopes = {"netbird_exercise_gear_template": gear_addon}
    for site, entry in taxonomy.items():
        suffixes = {g.get("suffix") for g in (entry or {}).get("groups") or []}
        if gear & suffixes:
            scopes[site] = suffixes

    if len(scopes) == 1:
        errors.append(
            "no goatnet declares the gear vocabulary, but the mint has a gear enum.\n"
            "        Either the taxonomy lost it or the enum was added first — the mint would "
            "derive groups nothing creates."
        )

    for scope, suffixes in sorted(scopes.items()):
        missing = sorted(gear - suffixes)
        if missing:
            errors.append(
                f"[{scope}] the mint knows gear class(es) this scope does not declare: "
                f"{', '.join(missing)}.\n"
                f"        A machine enrolled as one derives a group that does not exist, and the "
                f"enrollment fails at the mgmt API."
            )
        # The reverse: a declared gear-shaped group the mint cannot mint into.
        # Reserved classes (SENSOR/EFFECTOR) and the probation lane are declared
        # on purpose and are not mintable, so they are exempt by name.
        exempt = {"SENSOR", "EFFECTOR", "PROBATION-GEAR"}
        gearish = {s for s in suffixes if s and (s.startswith(("SENSOR", "EFFECTOR", "EXTERNAL-")) or s in {"KID", "FUSION"})}
        unknown = sorted(gearish - gear - exempt - {"EXTERNAL-INFRA"})
        if unknown:
            errors.append(
                f"[{scope}] declares gear-shaped group(s) the mint has never heard of: "
                f"{', '.join(unknown)}.\n"
                f"        Minting a machine into one is impossible, so the group can only be "
                f"populated by hand — the drift ADR 1116 exists to stop."
            )

        for cls, groups in sorted(derivation.items()):
            # Only check derivations whose target vocabulary this scope carries.
            unresolvable = sorted(g for g in groups if g not in suffixes)
            if unresolvable and (gear & suffixes):
                errors.append(
                    f"[{scope}] node-class {cls} derives group(s) not declared here: "
                    f"{', '.join(unresolvable)}."
                )

    # --- check 4: the taxonomy carries every class the ADR names -----------
    adr_path = root / ADR
    if adr_path.is_file():
        adr_src = adr_path.read_text()
        blocks = re.findall(r"\*\*(?:Sensors|Effectors|Other gear)\*\*\s*\n\n```\n(.*?)```", adr_src, re.S)
        if not blocks:
            errors.append(
                f"could not parse the class list out of {ADR} §3.\n"
                f"        The fenced Sensors/Effectors/Other-gear blocks are the authoritative\n"
                f"        vocabulary; if their shape changed, this guard must be taught the new one\n"
                f"        rather than silently checking nothing."
            )
        else:
            adr_classes = sorted({
                m.group(1)
                for b in blocks
                for line in b.splitlines()
                if (m := re.match(r"^([A-Z][A-Z0-9-]*)\s", line))
            })
            for scope, suffixes in sorted(scopes.items()):
                if scope == "netbird_exercise_gear_template":
                    suffixes = suffixes | gear_addon
                undeclared = sorted(c for c in adr_classes if c not in suffixes)
                if undeclared:
                    errors.append(
                        f"[{scope}] ADR 1116 §3 names class(es) this scope does not declare: "
                        f"{', '.join(undeclared)}.\n"
                        f"        A box whose correct class is undeclared gets the closest plausible\n"
                        f"        word instead — which is exactly how the radar-one control laptop ended\n"
                        f"        up in SENSOR-TASKING, holding no outbound reach at all."
                    )

    guard_path = root / MEMBERSHIP_GUARD
    if guard_path.is_file():
        guard_src = guard_path.read_text()
        block = re.search(r"NODE_CLASS\s*=\s*\{(.*?)\}", guard_src, re.S)
        if not block:
            errors.append(
                f"{MEMBERSHIP_GUARD} exists but has no NODE_CLASS set to check.\n"
                f"        The §7 guard cannot assert 'gear is not people' without one."
            )
        else:
            known = set(QUOTED.findall(block.group(1)))
            unknown = sorted(gear - known)
            if unknown:
                errors.append(
                    f"the §7 membership guard ({MEMBERSHIP_GUARD}) does not know gear class(es): "
                    f"{', '.join(unknown)}.\n"
                    f"        A peer correctly classed into one reads as UNCLASSIFIED and fails the "
                    f"guard for the wrong reason —\n"
                    f"        which is what happened on 2026-08-10 when CONTROL landed without it."
                )

    # Check 5 — prefix derivation. The portal role's default is
    # `portal_mesh_site_prefix: "{{ site_id | upper }}"` (hyphen KEPT); assert
    # that derivation lands on the taxonomy's site_prefix for every goatnet
    # fleet-pins governs. The first fleet-pins member is the mgmt group — where
    # a goatnet's identity (site_id) and its portal live; obs-only groups are
    # deliberately not members (see fleet-pins.yaml).
    fleet_path = root / FLEET_PINS
    if fleet_path.is_file():
        goatnets = (yaml.safe_load(fleet_path.read_text()) or {}).get("goatnets") or {}
        for goatnet, entry in sorted(goatnets.items()):
            members = (entry or {}).get("members") or []
            site_id = None
            for member in members[:1]:
                gv = root / GROUP_VARS / member / "vars.yml"
                if not gv.is_file():
                    continue
                site_id = (yaml.safe_load(gv.read_text()) or {}).get("site_id")
            if not site_id:
                print(f"  note: goatnet {goatnet} has no member group_vars site_id — prefix derivation not checked")
                continue
            site_entry = taxonomy.get(site_id)
            if site_entry is None:
                print(f"  note: goatnet {goatnet} (site_id {site_id}) has no taxonomy entry — prefix derivation skipped (its portal deploy asserts mint stays off)")
                continue
            declared = site_entry.get("site_prefix") or ""
            derived = site_id.upper()
            if derived != declared:
                errors.append(
                    f"[{goatnet}] the portal role would derive mesh prefix {derived} "
                    f"(site_id | upper) but the taxonomy declares site_prefix {declared}.\n"
                    f"        The mint would name groups nothing creates. Align "
                    f"{TAXONOMY} or override portal_mesh_site_prefix in group_vars."
                )
    else:
        print(f"  note: {FLEET_PINS} absent — prefix derivation not checked")

    for cls, groups in sorted(derivation.items()):
        if "OPSYSTEM" in groups:
            errors.append(
                f"node-class {cls} derives OPSYSTEM — the catch-all ADR 1116 retires. "
                f"The whole point of the derivation is that nothing lands there again."
            )

    if errors:
        print("gear class parity (ADR 1116): FAIL", file=sys.stderr)
        for e in errors:
            print(f"  ERROR {e}", file=sys.stderr)
        print(
            f"\n  Files: {TAXONOMY} + {MACHINES_GO}\n"
            f"  The mint and the mesh must share one vocabulary, or the class stops being\n"
            f"  the enforcement point. See docs/adr/1116-retire-opsystem-gear-classes.md §9.",
            file=sys.stderr,
        )
        return 1

    print(
        f"gear class parity (ADR 1116): PASS — {len(gear)} gear classes, "
        f"{len(derivation)} node-class→group derivations, agreed across "
        f"{len(scopes)} scope(s): {', '.join(sorted(scopes))}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
