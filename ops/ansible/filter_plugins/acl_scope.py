#!/usr/bin/env python3
"""
Scope-qualified group references for the NetBird ACL matrix (ADR 1124).

WHY THIS EXISTS. `netbird_acl_matrix.yml` rules carry group SUFFIXES only, and
the cutover playbook prefixes every one with the target scope's own
`site_prefix` before resolving it to a group UUID. That makes a rule naming a
group in another scope *unrepresentable* — not merely unwritten. A goatnet that
hosts a tactical site below it (`SITE_ONE-` under `ZAGROS-PROD-`) therefore has no
way to say "this site's gear may reach the host site's substrate", and the only
workaround available was to give the machine a second, site-lying membership.
F-316 has the live evidence; ADR 1124 is the decision.

THE MECHANISM. A reference may be a bare suffix (resolved in the rule's own
scope, unchanged behaviour) or a qualified `<scope>:<SUFFIX>`:

    INFRA                 -> <own_prefix>-INFRA
    HOST:INFRA            -> <host_site's prefix>-INFRA
    zagros-prod:INFRA       -> ZAGROS-PROD-INFRA

`HOST` is a reserved alias for the scope named by this scope's `host_site`,
which `netbird_exercises` already declares and `tasks/netbird-exercise-expand.yml`
already asserts is present. No new group is created and none is joined.

ONE IMPLEMENTATION, TWO CONSUMERS. The defect being fixed is "a rule written in
one place while enforcement lives somewhere else", so the fix must not reproduce
it. `netbird-acl-cutover.yml` calls these as Jinja filters;
`ops/ci/check-netbird-acl-parity.py` imports the same functions by path. There is
no second copy of the rules.

THE INVARIANT (ADR 1124). Any-port ADMINISTRATIVE reach may flow from a broader
scope into a narrower one, never the reverse. Port-scoped SERVICE reach may flow
either way. That is why `HOST:PLATFORM-ADMIN -> gear` (a goatnet admin
administering site gear) is permitted while `PLATFORM-ADMIN -> HOST:INFRA` (a
site admin administering the goatnet substrate) is refused: the second is the
exact boundary the tactical-site scope exists to hold.
"""

# Suffixes a rule may name in ANOTHER scope, by direction.
#
# UPWARD  = this scope's groups reaching out to another scope's group
#           (qualified DESTINATION). Only the substrate tier, and only on
#           explicit ports — this is service reach, never administration.
# DOWNWARD = another scope's groups reaching into this scope
#           (qualified SOURCE). The support / dev-ops path; may be any-port,
#           matching what `platform-admin -> gear` already grants intra-scope.
QUALIFIED_DEST_SUFFIXES = frozenset({"INFRA"})
QUALIFIED_SOURCE_SUFFIXES = frozenset({"PLATFORM-ADMIN"})

HOST_ALIAS = "HOST"


class AclScopeError(ValueError):
    """A qualified reference that must not be resolved. Always fail closed."""


def parse_ref(ref):
    """'HOST:INFRA' -> ('HOST', 'INFRA'); 'INFRA' -> (None, 'INFRA')."""
    if ":" not in ref:
        return None, ref
    scope, _, suffix = ref.partition(":")
    return scope, suffix


def is_qualified(ref):
    return ":" in ref


def resolve_ref(ref, own_prefix, host_prefix=None, scope_prefixes=None):
    """Resolve one reference to a full `<PREFIX>-<SUFFIX>` group name."""
    scope, suffix = parse_ref(ref)
    if scope is None:
        return "{}-{}".format(own_prefix, suffix)
    if not suffix:
        raise AclScopeError("qualified reference {!r} has an empty suffix".format(ref))
    if scope == HOST_ALIAS:
        if not host_prefix:
            raise AclScopeError(
                "{!r} names HOST but this scope declares no host_site (or its "
                "prefix could not be resolved). A qualified reference must never "
                "silently fall back to the local scope.".format(ref)
            )
        return "{}-{}".format(host_prefix, suffix)
    prefixes = scope_prefixes or {}
    if scope not in prefixes:
        raise AclScopeError(
            "{!r} names scope {!r}, which is not declared in the taxonomy. "
            "Known scopes: {}".format(ref, scope, ", ".join(sorted(prefixes)) or "(none)")
        )
    return "{}-{}".format(prefixes[scope], suffix)


def resolve_refs(refs, own_prefix, host_prefix=None, scope_prefixes=None):
    """Resolve a rule's sources or destinations list."""
    return [resolve_ref(r, own_prefix, host_prefix, scope_prefixes) for r in refs or []]


def validate_rule(rule, *, template=False):
    """Return a list of human-readable violations for one matrix rule.

    Empty list means the rule is legal. Every message names the offending rule
    so a failure is actionable without opening the resolver.
    """
    name = rule.get("name", "<unnamed>")
    sources = rule.get("sources") or []
    destinations = rule.get("destinations") or []
    protocol = rule.get("protocol")
    ports = rule.get("ports") or rule.get("port_ranges")
    bidirectional = rule.get("bidirectional", True)

    q_src = [r for r in sources if is_qualified(r)]
    q_dst = [r for r in destinations if is_qualified(r)]
    errs = []

    if not q_src and not q_dst:
        return errs

    # 1. Never bidirectional. THE load-bearing constraint: a bidirectional
    #    qualified rule silently creates the reverse grant, which is how a
    #    tactical scope would escalate into its parent.
    if bidirectional:
        errs.append(
            "rule {!r}: a rule with a scope-qualified reference must set "
            "bidirectional: false — a bidirectional qualified rule silently "
            "creates the reverse grant (ADR 1124).".format(name)
        )

    # 2. Never qualified on both sides — that is a rule about two other scopes,
    #    authored by a third. Whoever owns those scopes should write it.
    if q_src and q_dst:
        errs.append(
            "rule {!r}: qualified references on BOTH sides ({} -> {}). A scope "
            "may relate itself to another scope, not two others to each "
            "other.".format(name, ", ".join(q_src), ", ".join(q_dst))
        )

    for ref in q_src + q_dst:
        scope, suffix = parse_ref(ref)
        if not scope or not suffix:
            errs.append("rule {!r}: malformed qualified reference {!r}.".format(name, ref))
            continue
        # 3. Templates are shared across scopes, so they cannot name one.
        if template and scope != HOST_ALIAS:
            errs.append(
                "rule {!r}: template rules may only use the {}: alias, not the "
                "concrete scope {!r} — a shared template is instantiated for "
                "every scope and each has a different host.".format(name, HOST_ALIAS, scope)
            )

    # 4. UPWARD: substrate tier only, and always port-scoped.
    for ref in q_dst:
        _, suffix = parse_ref(ref)
        if suffix not in QUALIFIED_DEST_SUFFIXES:
            errs.append(
                "rule {!r}: {!r} is not a permitted qualified DESTINATION. "
                "Reaching out of a scope is allowed only to {} (ADR 1124); "
                "widening this set is an ADR amendment, not a matrix "
                "edit.".format(name, ref, "/".join(sorted(QUALIFIED_DEST_SUFFIXES)))
            )
        if protocol == "all" or (protocol != "icmp" and not ports):
            errs.append(
                "rule {!r}: a qualified DESTINATION must carry an explicit port "
                "list or be icmp — never protocol: all. Reaching into another "
                "scope is service reach, never administration (ADR 1124).".format(name)
            )

    # 5. DOWNWARD: admin tier only. Any-port is permitted here on purpose —
    #    this is the support path, and privilege flows from broader to
    #    narrower.
    for ref in q_src:
        _, suffix = parse_ref(ref)
        if suffix not in QUALIFIED_SOURCE_SUFFIXES:
            errs.append(
                "rule {!r}: {!r} is not a permitted qualified SOURCE. Only {} "
                "may be granted reach inward (ADR 1124) — INFRA is excluded "
                "deliberately: machines get function, not any-port.".format(
                    name, ref, "/".join(sorted(QUALIFIED_SOURCE_SUFFIXES))
                )
            )

    return errs


def rule_uses_qualified(rule):
    """True if this rule names any group outside its own scope."""
    refs = (rule.get("sources") or []) + (rule.get("destinations") or [])
    return any(is_qualified(r) for r in refs)


class FilterModule(object):
    """Expose the resolver to the cutover playbook.

    Thin by design — every decision lives in the functions above so the CI
    guard grades the same logic the playbook applies.
    """

    def filters(self):
        return {
            "acl_resolve_refs": self._resolve,
            "acl_rule_violations": self._violations,
        }

    @staticmethod
    def _resolve(refs, own_prefix, host_prefix=None, scope_prefixes=None):
        try:
            return resolve_refs(refs, own_prefix, host_prefix, scope_prefixes)
        except AclScopeError as exc:
            # Surface as a template error rather than resolving to something
            # plausible-but-wrong; the cutover must not apply a guessed group.
            raise AclScopeError(str(exc))

    @staticmethod
    def _violations(rule, template=False):
        return validate_rule(rule, template=template)
