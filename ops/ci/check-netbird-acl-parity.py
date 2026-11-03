#!/usr/bin/env python3
"""
Parity guard between the two halves of a goatnet's NetBird mesh ACL:

  ops/ansible/netbird_acl_taxonomy.yml  — WHICH groups exist (the vocabulary)
  ops/ansible/netbird_acl_matrix.yml    — WHAT is permitted between them

Why this guard exists:

  A goatnet can go live with all its taxonomy groups created and ZERO matrix
  policies: the taxonomy has an entry, the matrix does not, and nothing in the
  standup path notices. NetBird's Default All->All stays the only policy, so
  every group is a label on a flat mesh — peers that look segmented by group
  reach everything. Creating groups reads like configuring access, and nothing
  fails loudly when the permits are never authored.

  A goatnet is only segmented when BOTH halves exist. This guard makes the
  half-configured state unrepresentable in the tree.

Checks (all fail-closed, exit 1 on any violation):

  1. Every non-exercise site in the taxonomy has a matrix entry, and vice
     versa. Missing matrix entry = a flat mesh with labels.
  2. `site_prefix` agrees between the two files. The cutover playbook reads
     the prefix from the TAXONOMY only, so a divergent matrix prefix is
     silently ignored today — it reads as configured but does nothing.
  3. Every group suffix a matrix rule references (sources/destinations)
     is declared in that site's taxonomy. A stale suffix aborts the cutover
     play at its live-group assert, i.e. mid-standup, which is the
     worst time to discover a vocabulary rename was half-applied.
  4. Every matrix site carries a `cutover` block with both keys the cutover
     playbook reads (`default_policy_action`, `default_policy_name_pattern`).
  5. Exercise / site scopes are EXPANDED the way tasks/netbird-exercise-expand.yml
     expands them — base template, plus the ADR 1116 gear class set and its rule
     shapes when the row declares `gear: true` — and then run through check 3.
     Without this, a scope holding gear is one CI never looks at, and a typo
     in the gear rules would surface at the live-group assert of a live apply.

Advisory (reported, does NOT fail): taxonomy groups that no rule references.
Label groups (node-class identification, no policy of their own) are an
explicitly ratified pattern — see the label-group principle in the taxonomy
file — so this cannot be a hard error. It is printed because "declared but
grants nothing once Default is disabled" is precisely how a peer ends up
enrolled-and-cut-off, and the taxonomy's own rule is that a label must never
substitute for coverage.

Scope note: this guard is FILE-level parity only. It cannot see whether a
goatnet's LIVE mesh actually has the matrix applied — a site can be green
here and still be running flat on Default All->All. Live posture is the
cutover playbook's `mode=propose` report
and the standup gate, not CI.

Usage:  python3 ops/ci/check-netbird-acl-parity.py [ops/ansible]
        (default anchored to the script's own location — works from any cwd)
Rollback: rm this file + .github/workflows/netbird-acl-parity.yml
"""

import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("FAIL: PyYAML not available (pip install pyyaml)", file=sys.stderr)
    sys.exit(2)

CUTOVER_REQUIRED_KEYS = ("default_policy_action", "default_policy_name_pattern")
# The playbook branches on this value; anything else falls through its
# `keep`-refusal assert (the cutover pre-flight) — surface the typo here instead.
CUTOVER_ALLOWED_ACTIONS = ("disable", "delete", "keep")

# ADR 1124 scope-qualified references (`HOST:INFRA`). The resolver and its
# constraint set live in ops/ansible/filter_plugins/acl_scope.py and are
# IMPORTED here rather than re-implemented: the defect ADR 1124 fixes is "a rule
# written in one place while enforcement lives somewhere else", so the guard
# must grade the exact logic the cutover playbook applies.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "ansible" / "filter_plugins"))
try:
    from acl_scope import HOST_ALIAS, is_qualified, parse_ref, validate_rule
except ImportError as exc:  # pragma: no cover - a missing resolver is a hard stop
    print(f"FAIL: cannot import acl_scope ({exc}) — ADR 1124 resolver missing", file=sys.stderr)
    sys.exit(2)

# The port set that means "this class can actually carry fabric traffic".
FABRIC_PORTS = {"7447", "7448"}


def split_refs(rules):
    """Partition every source/destination across `rules` into bare vs qualified."""
    bare, qualified = set(), set()
    for rule in rules or []:
        for ref in (rule.get("sources") or []) + (rule.get("destinations") or []):
            (qualified if is_qualified(ref) else bare).add(ref)
    return bare, qualified


def has_substrate_path(rules, local_infra_suffix="INFRA"):
    """Does this scope's rule set give anything a route to *a* substrate?

    True when some rule names the scope's own INFRA as a destination, or names
    any qualified `HOST:` destination. A `gear: true` scope with neither is a
    site that severs every machine that arrives at it; this check catches that
    at stamp time rather than in the field.
    """
    for rule in rules or []:
        for dest in rule.get("destinations") or []:
            if dest == local_infra_suffix:
                return True
            if is_qualified(dest):
                return True
    return False


def load(path: Path, top_key: str):
    if not path.is_file():
        print(f"FAIL: {path} not found", file=sys.stderr)
        sys.exit(2)
    doc = yaml.safe_load(path.read_text()) or {}
    if top_key not in doc:
        print(f"FAIL: {path} has no top-level `{top_key}`", file=sys.stderr)
        sys.exit(2)
    return doc


def main() -> int:
    if len(sys.argv) > 1:
        base = Path(sys.argv[1])
    else:
        base = Path(__file__).resolve().parents[2] / "ops" / "ansible"
    tax_path = base / "netbird_acl_taxonomy.yml"
    mat_path = base / "netbird_acl_matrix.yml"

    tax_doc = load(tax_path, "netbird_acl_taxonomy")
    mat_doc = load(mat_path, "netbird_acl_matrix")

    taxonomy = tax_doc["netbird_acl_taxonomy"] or {}
    matrix = mat_doc["netbird_acl_matrix"] or {}

    # Exercise group-sets are expanded into synthetic site entries at run time
    # by tasks/netbird-exercise-expand.yml (rules come from the shared
    # netbird_exercise_rules_template), so they legitimately have no static
    # matrix entry and are excluded from check 1.
    exercises = set((tax_doc.get("netbird_exercises") or {}).keys())

    errors: list[str] = []
    notes: list[str] = []

    # `_default` (and any other underscore-prefixed key) is a TEMPLATE a
    # not-explicitly-declared site inherits at runtime (operator-onboard.yml
    # combine-loop), never a deployable scope — exclude it from parity the same
    # way exercises are excluded, or it reads as "declared but not in matrix".
    tax_sites = {s for s in taxonomy if not s.startswith("_")} - exercises
    mat_sites = {s for s in matrix if not s.startswith("_")} - exercises

    for site in sorted(tax_sites - mat_sites):
        errors.append(
            f"[{site}] declared in netbird_acl_taxonomy.yml but NOT in "
            f"netbird_acl_matrix.yml.\n"
            f"        Its groups would be created with no policy using them — the "
            f"goatnet runs FLAT on\n"
            f"        NetBird's Default All->All (the zagros-prod 2026-08-04 / F-250 bug). "
            f"Add a matrix entry;\n"
            f"        alias an existing site's ruleset (e.g. `rules: *zagros_rules`) unless a "
            f"delta is intended."
        )

    for site in sorted(mat_sites - tax_sites):
        errors.append(
            f"[{site}] declared in netbird_acl_matrix.yml but NOT in "
            f"netbird_acl_taxonomy.yml.\n"
            f"        The cutover playbook resolves rule suffixes against the taxonomy's "
            f"site_prefix and\n"
            f"        refuses a site missing from either file (its pre-flight)."
        )

    for site in sorted(tax_sites & mat_sites):
        tax_entry = taxonomy[site] or {}
        mat_entry = matrix[site] or {}

        tax_prefix = tax_entry.get("site_prefix")
        mat_prefix = mat_entry.get("site_prefix")
        if tax_prefix != mat_prefix:
            errors.append(
                f"[{site}] site_prefix disagrees: taxonomy={tax_prefix!r} "
                f"matrix={mat_prefix!r}.\n"
                f"        The cutover playbook reads the prefix from the TAXONOMY only, so "
                f"the matrix value is\n"
                f"        silently ignored — it reads as configured while doing nothing."
            )

        declared = {g.get("suffix") for g in (tax_entry.get("groups") or [])}

        # A suffix carrying ':' would make the ADR 1124 qualifier namespace
        # ambiguous — refuse it in the vocabulary rather than at resolve time.
        colon_suffixes = sorted(s for s in declared if s and ":" in s)
        if colon_suffixes:
            errors.append(
                f"[{site}] taxonomy declares group suffix(es) containing ':': "
                f"{', '.join(colon_suffixes)}.\n"
                f"        ':' is reserved for ADR 1124 scope-qualified references "
                f"(`HOST:INFRA`); a suffix may not contain it."
            )

        referenced, qualified = split_refs(mat_entry.get("rules") or [])

        # ADR 1124 constraints (never-bidirectional, direction-scoped
        # allowlists, ports). A CONCRETE site matrix may name a scope, so
        # template=False here.
        for rule in mat_entry.get("rules") or []:
            for violation in validate_rule(rule, template=False):
                errors.append(f"[{site}] {violation}")

        for ref in sorted(qualified):
            scope_tok, suffix = parse_ref(ref)
            if scope_tok == HOST_ALIAS:
                errors.append(
                    f"[{site}] rule references {ref!r}, but {site} is a concrete site "
                    f"with no `host_site`.\n"
                    f"        `{HOST_ALIAS}:` is only meaningful inside a scope declared "
                    f"under netbird_exercises."
                )
                continue
            target = taxonomy.get(scope_tok)
            if target is None:
                errors.append(
                    f"[{site}] rule references {ref!r}, naming scope {scope_tok!r} which is "
                    f"not declared in the taxonomy."
                )
            elif suffix not in {g.get("suffix") for g in (target.get("groups") or [])}:
                errors.append(
                    f"[{site}] rule references {ref!r}, but scope {scope_tok!r} declares no "
                    f"{suffix!r} group."
                )

        unknown = sorted(referenced - declared)
        if unknown:
            errors.append(
                f"[{site}] matrix rules reference group suffix(es) not declared in the "
                f"taxonomy: {', '.join(unknown)}.\n"
                f"        The cutover playbook asserts every referenced group exists on the "
                f"live mgmt-server\n"
                f"        (its live-group assert) and aborts — mid-standup. Usually a half-applied "
                f"vocabulary rename."
            )

        cutover = mat_entry.get("cutover") or {}
        missing_keys = [k for k in CUTOVER_REQUIRED_KEYS if k not in cutover]
        if missing_keys:
            errors.append(
                f"[{site}] matrix entry missing cutover key(s): "
                f"{', '.join(missing_keys)}.\n"
                f"        The cutover playbook reads both to find and act on the Default "
                f"policy."
            )
        action = cutover.get("default_policy_action")
        if action is not None and action not in CUTOVER_ALLOWED_ACTIONS:
            errors.append(
                f"[{site}] cutover.default_policy_action is {action!r}; must be one of "
                f"{', '.join(CUTOVER_ALLOWED_ACTIONS)}.\n"
                f"        The cutover playbook's pre-flight treats an "
                f"unrecognized value like `keep`\n"
                f"        and refuses mode=enforce — a typo here silently makes the site "
                f"un-enforceable."
            )

        unused = sorted(declared - referenced)
        if unused:
            notes.append(
                f"[{site}] taxonomy group(s) referenced by no rule: {', '.join(unused)}. "
                f"Legitimate for label groups; a peer holding ONLY one of these is cut off "
                f"once Default is disabled."
            )

    # ---- check 5: the expanded exercise / site scopes --------------------
    # Mirror of tasks/netbird-exercise-expand.yml. Kept in step by hand; if the
    # expansion there grows another opt-in, add it here or the new scopes go
    # unchecked, which is the exact gap this check was added to close.
    base_groups = (tax_doc.get("netbird_exercise_template") or {}).get("groups") or []
    gear_groups = (tax_doc.get("netbird_exercise_gear_template") or {}).get("groups") or []
    base_rules = (mat_doc.get("netbird_exercise_rules_template") or {}).get("rules") or []
    gear_rules = (mat_doc.get("netbird_exercise_gear_rules_template") or {}).get("rules") or []

    for scope, row in sorted((tax_doc.get("netbird_exercises") or {}).items()):
        # ADR 1158: `kind:` is what the teardown guard reads, so it is required
        # and closed. Checked statically as well as in the Ansible expand task
        # because a missing `kind:` should fail the PR that writes it, not the
        # deploy that first reads it.
        kind = (row or {}).get("kind")
        if kind not in {"exercise", "operational"}:
            errors.append(
                f"[{scope}] declares kind={kind!r}; must be 'exercise' (has an end date) "
                f"or 'operational' (does not).\n"
                f"        netbird-acl-cutover.yml's teardown refuses `operational` and "
                f"accepts everything else,\n"
                f"        so an unset or misspelled kind makes a live site tearable "
                f"(ADR 1158)."
            )

        wants_gear = bool((row or {}).get("gear", False))
        if wants_gear and not gear_groups:
            errors.append(
                f"[{scope}] declares `gear: true` but netbird_acl_taxonomy.yml has no "
                f"`netbird_exercise_gear_template`.\n"
                f"        The scope would expand to the base group set with no sign anything "
                f"was dropped."
            )
            continue
        if wants_gear and not gear_rules:
            errors.append(
                f"[{scope}] declares `gear: true` but netbird_acl_matrix.yml has no "
                f"`netbird_exercise_gear_rules_template`.\n"
                f"        The ADR 1116 gear classes would be created for this scope with no "
                f"rule covering them."
            )
            continue

        declared = {g.get("suffix") for g in base_groups + (gear_groups if wants_gear else [])}
        scope_rules = base_rules + (gear_rules if wants_gear else [])
        referenced, qualified = split_refs(scope_rules)

        # ADR 1124 constraints. These rules come from a shared TEMPLATE, so
        # template=True — a template may use `HOST:` but never a concrete scope
        # name, because it is instantiated for every scope and each has a
        # different host.
        for rule in scope_rules:
            for violation in validate_rule(rule, template=True):
                errors.append(f"[{scope}] {violation}")

        # `HOST:` must resolve: the scope declares a host_site, that host is in
        # the taxonomy, and it declares the named suffix. Today a dangling
        # reference aborts the cutover mid-standup at its live-group
        # assert; this moves that failure to PR time.
        host_site = (row or {}).get("host_site")
        host_entry = taxonomy.get(host_site) if host_site else None
        host_suffixes = {g.get("suffix") for g in (host_entry.get("groups") or [])} if host_entry else set()
        for ref in sorted(qualified):
            scope_tok, suffix = parse_ref(ref)
            if scope_tok != HOST_ALIAS:
                # validate_rule already reported the template violation; do not
                # double-report, but do not resolve it either.
                continue
            if host_entry is None:
                errors.append(
                    f"[{scope}] rule references {ref!r} but its host_site "
                    f"{host_site!r} is not a declared taxonomy site."
                )
            elif suffix not in host_suffixes:
                errors.append(
                    f"[{scope}] rule references {ref!r}, but host site {host_site!r} "
                    f"declares no {suffix!r} group."
                )

        # A gear-bearing scope must have SOME route to a substrate. Without
        # this, a scope can be stamped, look configured, and sever every machine
        # that arrives at it — caught here before hardware.
        if wants_gear and not has_substrate_path(scope_rules):
            errors.append(
                f"[{scope}] declares `gear: true` but no rule reaches a substrate — "
                f"neither its own INFRA nor any {HOST_ALIAS}: destination.\n"
                f"        Every machine enrolled at this scope would arrive severed with no "
                f"rule to point at."
            )

        unknown = sorted(referenced - declared)
        if unknown:
            errors.append(
                f"[{scope}] expanded scope's rules reference group suffix(es) the template "
                f"does not declare: {', '.join(unknown)}.\n"
                f"        Expansion is base{' + gear' if wants_gear else ''}; the cutover "
                f"playbook asserts these exist live (its live-group assert) and aborts."
            )

        unused = sorted(declared - referenced)
        if unused:
            notes.append(
                f"[{scope}] expanded scope group(s) referenced by no rule: "
                f"{', '.join(unused)}. A peer holding ONLY one of these is cut off once "
                f"Default is disabled."
            )

    checked = sorted(tax_sites | mat_sites)
    if notes:
        print("netbird ACL parity — advisory:")
        for n in notes:
            print(f"  NOTE {n}")

    if errors:
        print("\nnetbird ACL taxonomy/matrix parity: FAIL", file=sys.stderr)
        for e in errors:
            print(f"  ERROR {e}", file=sys.stderr)
        print(
            f"\n  Files: {tax_path} + {mat_path}\n"
            f"  Both halves are required for a goatnet to be segmented at all. See\n"
            f"  docs/design/goatnet-turnkey-standup.md and docs/design/mesh-acl.md.",
            file=sys.stderr,
        )
        return 1

    # Name the excluded scopes by KIND, not by the key they happen to live
    # under. `netbird_exercises` also holds persistent OPERATIONAL sites;
    # reporting those as "exercise set(s) excluded" is how a live site comes
    # to receive less checking than a sandbox while the summary still reads PASS.
    # A row is operational when it says so, or when it lacks the `ex-` prefix
    # its genuine-exercise siblings all carry.
    ex_rows = tax_doc.get("netbird_exercises") or {}
    operational = sorted(
        s for s in exercises
        if (ex_rows.get(s) or {}).get("kind") == "operational"
        or not s.startswith("ex-")
    )
    pure_ex = sorted(s for s in exercises if s not in operational)

    # "Excluded" means excluded from CHECK 1 (taxonomy↔matrix parity) only —
    # a template-expanded scope legitimately has no static matrix entry. Check 5
    # above DOES validate every scope, so the earlier wording ("this guard does
    # not check them") understated the coverage in the opposite direction from
    # the usual failure and is not repeated.
    tail = ""
    if pure_ex:
        tail += f"; {len(pure_ex)} exercise scope(s) checked as templates, not against a matrix entry"
    if operational:
        tail += (
            f"; {len(operational)} OPERATIONAL scope(s) likewise "
            f"({', '.join(operational)}) — live sites, not exercises"
        )

    print(
        f"netbird ACL taxonomy/matrix parity: PASS "
        f"({len(checked)} site(s): {', '.join(checked)}" + tail + ")"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
