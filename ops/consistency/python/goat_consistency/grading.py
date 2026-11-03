# SPDX-License-Identifier: Apache-2.0
#
# grading.py — THE comparison, and the only place it lives.
#
# A projection may choose which records it can see and how it renders; it may
# NOT grade. A surface that computes its own verdict is a second model, and the
# first divergence between them is silent by construction. Canon:
# docs/design/consistency-foundation.md §4 (ADR 1110 D2/D4).
#
# Everything about WHICH grade wins, WHAT colour it is and WHICH remediation it
# earns comes from ops/consistency/grammar.yaml. What lives here is the shape of
# the comparison itself: lift each cell's resolution state into a grade, take
# the worst, and let a declaration downgrade an undeclared difference into a
# declared one.
#
# Verdicts are computed at READ TIME and stored nowhere. A node knows what it is
# running; it does not know whether that is current, because that is a
# comparison against a tree it may not have.

import datetime

from . import grammar as grammar_mod
from . import intent as intent_mod

# The comparable stand-in for a member that deliberately does not run a
# component. Two absent members agree; absent-vs-present is a difference, and
# since absence is only ever reachable through a declared deviation, that
# difference lands as `declared` rather than `SKEW`.
ABSENT_KEY = "<absent>"

# Lifting a cell STATE into a grade. A `resolved` cell is compared by value and
# an `absent` cell compares as the sentinel above; every other state is already
# an answer and lifts straight to its grade. Kept explicit because the whole
# honesty grammar rests on `unset` and `unreadable` staying apart — nothing to
# read, versus read it and could not evaluate it.
#
# The last three states are LIVE-vantage only and have no producer until CF-3:
# `unreported` is a node that has not published (never compliant, ADR 1109 P15),
# `behind` is a node holding a generation the artifact's own history tail
# accounts for (§3.3a), and `unaccountable` is a node reporting a ref or a
# generation no generation explains. Their grading, precedence and remediation
# are pinned by the corpus NOW so CF-3 adds a producer rather than a second
# opinion.
#
# `behind` lifts to SKEW on its own rather than by comparing values, and that is
# the point: three nodes that agree with each other on a SUPERSEDED generation
# are not `match`. The reference they are behind is the artifact, not each
# other.
STATE_GRADE = {
    "unreadable": "unreadable",
    "unset": "indeterminate",
    "unreported": "indeterminate",
    "unaccountable": "unaccountable",
    "behind": "SKEW",
}


class GradedRow(object):
    """One row of the comparison, in the one shape every caller gets (§4.4).

    `grounding` is not decoration: ADR 1109 P15 requires every fleet reading to
    carry its n, window and vantage, because "100% compliant" that is really
    "0 nodes reported" is the specific failure it exists to make impossible."""

    __slots__ = ("subject", "name", "scope", "cells", "grade", "why",
                 "remediation", "grounding")

    def __init__(self, subject, name, scope, cells, grade, why, remediation, grounding):
        self.subject = subject
        self.name = name
        self.scope = scope
        self.cells = cells
        self.grade = grade
        self.why = why
        self.remediation = remediation
        self.grounding = grounding

    def as_dict(self):
        return {
            "subject": self.subject,
            "name": self.name,
            "scope": self.scope,
            "cells": self.cells,
            "grade": self.grade,
            "why": self.why,
            "remediation": self.remediation,
            "grounding": self.grounding,
        }


def grade_cells(cells, declared=False, grammar=None):
    """The bare verdict for a set of member/node cells.

    `cells` are IntentV1-shaped: {"state": …, "value": …}. Passing the artifact's
    own cells is deliberate — the repo-side renderer and a CF-3 consumer holding
    only the artifact run the identical comparison rather than two that agree
    until they don't."""
    g = grammar or grammar_mod.load()
    cells = list(cells)
    if not cells:
        # Nothing was observed. Absence of signal is never green.
        return "indeterminate"

    lifted = [STATE_GRADE[c.get("state")] for c in cells if c.get("state") in STATE_GRADE]
    if lifted:
        verdict = g.worst(lifted)
    else:
        keys = [ABSENT_KEY if c.get("state") == "absent" else c.get("value") for c in cells]
        if any(k is None for k in keys):
            # A cell that claims `resolved` with no value is not readable,
            # whatever it claims. Fail toward the ungrounded state.
            verdict = "unreadable"
        else:
            verdict = "match" if len(set(keys)) <= 1 else "SKEW"

    # A declaration turns a difference the tree cannot decide into a difference
    # somebody decided. It cannot rescue an ungrounded row: a `reason:` explains
    # a difference, it does not make an unreadable value readable.
    if declared and verdict in ("SKEW", "indeterminate"):
        verdict = "declared"
    return verdict


def _parse_stamp(text):
    """A generation timestamp, or None. The artifact writes exactly one format
    (`%Y-%m-%dT%H:%M:%SZ`); anything else is not parsed into a guess."""
    try:
        parsed = datetime.datetime.strptime(str(text), "%Y-%m-%dT%H:%M:%SZ")
    except (TypeError, ValueError):
        return None
    return parsed.replace(tzinfo=datetime.timezone.utc)


def _age_seconds(generated_at, now):
    """Age of a generation at read time, or None when it cannot be stated. A
    reader whose clock sits BEFORE the generation reports no age rather than a
    negative one — ADR 1037 makes UTC confidence a thing to state, not assume."""
    stamp = _parse_stamp(generated_at)
    if stamp is None:
        return None
    now = now or datetime.datetime.now(datetime.timezone.utc)
    seconds = int((now - stamp).total_seconds())
    return seconds if seconds >= 0 else None


def _age_phrase(seconds):
    """§5.3's sentence: 'converged against 9-day-old intent'."""
    if seconds is None:
        return "intent age unstatable"
    if seconds >= 86400:
        return "%d-day-old intent" % (seconds // 86400)
    if seconds >= 3600:
        return "%d-hour-old intent" % (seconds // 3600)
    return "%d-minute-old intent" % (seconds // 60)


def account_for(intent, observed_content_hash, now=None, observed_serial=None):
    """Classify ONE node's converged generation against the artifact ALONE.

    This is the computation §3.3a's history tail exists for, and the reason that
    tail lives inside the hashed body. A reader that holds one generation and no
    git tree — the CF-3 rollup meter, the CF-6 console, an ADR 1109 FM-2 node —
    can otherwise say only "this is not the current hash", which reads
    identically for a node one bump behind and a node running something nobody
    ever committed. §4.2 gives those two OPPOSITE first actions: re-run the
    refresh play, versus do not re-run it because that erases the evidence. A
    reader that cannot separate them must either drop the grade or point every
    ordinary lagging node at `rollback.sh`.

    `observed_serial` is OPTIONAL and changes no grade. It exists for one
    diagnosis the reader could otherwise not make: a node reporting a serial
    NEWER than this artifact's current generation is not a node nobody can
    explain — it is a node this READER cannot explain, because the reader's own
    copy of the artifact is stale. Both are `unaccountable` (no generation in
    THIS tree explains the value, which is the grade's literal meaning), but the
    operator action inverts: refresh the reader, do not touch the node.
    Measured on zagros-prod 2026-08-19 — the deployed artifact sat nine days
    behind at serial 45 while converges stamped 113, and every beat blamed a
    node that was correct and current.

    Returns an IntentV1-shaped cell, so the answer flows into the same
    grade_row() every other row uses instead of into a second comparison.
    Per §5.3 the age is REPORTED on every cell and GRADED only when the node is
    not on the current generation — a goatnet whose config has not changed in 90
    days is correct, not stale.
    """
    current = intent.get("content_hash")
    serial = intent.get("serial")
    tail = [h for h in (intent.get("history") or []) if isinstance(h, dict)]

    if not observed_content_hash:
        return {"state": "unreported",
                "detail": "no applied-intent stamp"}

    def generation(entry, behind_by):
        seconds = _age_seconds(entry.get("generated_at"), now)
        return {"serial": entry.get("serial"),
                "generated_at": entry.get("generated_at"),
                "behind_by": behind_by,
                "intent_age_seconds": seconds}

    if observed_content_hash == current:
        return {"state": "resolved",
                "value": observed_content_hash,
                "generation": generation(intent, 0)}

    for index, entry in enumerate(tail):
        if entry.get("content_hash") != observed_content_hash:
            continue
        gen = generation(entry, index + 1)
        return {"state": "behind",
                "value": observed_content_hash,
                "detail": "converged against serial %s — %d generation%s behind, %s"
                          % (entry.get("serial"), gen["behind_by"],
                             "" if gen["behind_by"] == 1 else "s",
                             _age_phrase(gen["intent_age_seconds"])),
                "generation": gen}

    # Nowhere in the artifact. Whether it fell off the tail or never existed is
    # a distinction this reader cannot make and must not fake — both are
    # `unaccountable`, and the tail's own reach is stated so an operator can see
    # which one they are looking at.
    serials = sorted(h["serial"] for h in tail if isinstance(h.get("serial"), int))
    if not serials:
        where = "nor a history tail (this generation carries none)"
        note = ""
    else:
        where = "nor its history tail (serials %d-%d)" % (serials[0], serials[-1])
        note = ("; a generation older than serial %d has fallen off the %d-deep tail, "
                "which is unaccountable rather than lost"
                % (serials[0], intent_mod.HISTORY_DEPTH)) if serials[0] > 1 else ""
    # The reader-is-stale case, stated rather than left to be inferred. Same
    # grade (nothing in THIS tree explains the value) but the opposite first
    # action, so it must not render as the node's fault.
    if (isinstance(observed_serial, int) and isinstance(serial, int)
            and observed_serial > serial):
        return {"state": "unaccountable",
                "value": observed_content_hash,
                "detail": "node reports serial %d, NEWER than this artifact's current "
                          "generation %d — THIS READER'S COPY OF THE INTENT ARTIFACT IS "
                          "STALE, the node is ahead of it. Refresh the reader (re-run the "
                          "consistency-health / consistency-console role so it carries the "
                          "current ops/consistency/intent/<goatnet>/intent.json); do NOT "
                          "re-converge the node against an older generation"
                          % (observed_serial, serial)}

    return {"state": "unaccountable",
            "value": observed_content_hash,
            "detail": "matches neither serial %s %s — no generation in this artifact "
                      "explains it%s" % (serial, where, note)}


def _short_hash(value):
    """A hash abbreviated for a generated sentence: the digest half, first 12
    hex chars. Deterministic, because the sentence is pinned by the corpus."""
    return str(value or "").replace("sha256:", "")[:12]


def account_for_render(record):
    """Classify ONE watched render — the CF-4 computation, from a node's own
    facts alone.

    A render's reference point is not a generation: it is what the LAST
    CONVERGE on that node recorded after rendering the file
    (/var/lib/goat/<component>/render-manifest.json, written after the
    templates land — position, not reconciliation, is what makes it a
    post-render fact). The intent artifact carries WHICH paths constitute a
    component's render identity (`render_watch`); it cannot carry their hashes,
    because a per-host render is a function of host vars and env lookups no
    repo-side reader may evaluate (§3.1's no-secrets rule is the same fence).

    So the comparison is current-bytes vs converge-bytes, and the honest states
    are:
      - both hash and they agree            -> resolved (the file is what the
        converge rendered — whether that converge is CURRENT is the generation
        row's question, not this one's)
      - both hash and they differ           -> unaccountable: no converge
        explains the bytes on disk — the hand-edit shape
      - the file is gone                    -> unaccountable: the converge
        rendered it and nothing explains its absence
      - the converge could not hash it, or
        the collector could not read it     -> unreadable, never a verdict

    Returns an IntentV1-shaped cell so the answer flows into the same
    grade_row() as every other row, instead of into a second comparison."""
    recorded = record.get("recorded_sha256") or ""
    current = record.get("sha256") or ""
    note = record.get("note") or ""
    path = record.get("path") or ""

    if not recorded:
        return {"state": "unreadable",
                "detail": "no converge-time hash for %s — the deploying role could not "
                          "hash it when it rendered (%s)"
                          % (path, note or "no reason recorded")}
    if not current:
        if note == "missing":
            return {"state": "unaccountable",
                    "detail": "%s is gone — the last converge rendered it (%s) and no "
                              "converge explains its absence" % (path, _short_hash(recorded))}
        return {"state": "unreadable",
                "detail": "%s could not be read at collection time (%s)"
                          % (path, note or "unreadable")}
    if current == recorded:
        return {"state": "resolved", "value": current}
    return {"state": "unaccountable",
            "value": current,
            "detail": "%s hashes to %s but the last converge rendered %s — no converge "
                      "explains the bytes on disk"
                      % (path, _short_hash(current), _short_hash(recorded))}


def _cell_text(cell):
    state = cell.get("state")
    if state == "resolved":
        return str(cell.get("value"))
    if state == "absent":
        return "absent"
    detail = cell.get("detail")
    return "%s (%s)" % (state, detail) if detail else str(state)


def _why(verdict, cells_by_scope):
    """One generated sentence naming what differs and where. Generated, never
    composed by a renderer, so every surface says the same thing."""
    listing = ", ".join("%s=%s" % (scope, _cell_text(cell))
                        for scope, cell in sorted(cells_by_scope.items()))
    if not listing:
        return "nothing reported"
    clause = {
        "match": "all agree",
        "declared": "the difference is declared with a reason",
        "SKEW": "they differ and nothing declares why",
        "unaccountable": "a reported value is explained by no generation in the tree",
        "unreadable": "read, not evaluable",
        "indeterminate": "there was nothing to read",
    }.get(verdict, verdict)
    return "%s — %s" % (listing, clause)


def _offender(verdict, cells_by_scope):
    """The scope a remediation pointer should name: the first cell, in scope
    order, that is not part of the agreement."""
    ranked = sorted(cells_by_scope.items())
    for scope, cell in ranked:
        if cell.get("state") in STATE_GRADE:
            return scope
    if verdict in ("SKEW", "declared", "unaccountable"):
        keys = [(s, ABSENT_KEY if c.get("state") == "absent" else c.get("value"))
                for s, c in ranked]
        counts = {}
        for _, k in keys:
            counts[k] = counts.get(k, 0) + 1
        majority = max(counts.values()) if counts else 0
        for scope, k in keys:
            if counts[k] < majority:
                return scope
    return ranked[0][0] if ranked else None


def grade_row(subject, name, cells_by_scope, goatnet, vantage="repo",
              declared=False, artifact_type=None, grammar=None):
    """Grade one row and attach its remediation pointer.

    `vantage` is `repo` (members compared inside the committed tree) or `live`
    (an intent generation compared against node records — CF-3). The same grade
    dispatches to a different pointer from each, because a difference in the
    tree is declared or edited while a difference against a node is
    re-converged."""
    g = grammar or grammar_mod.load()
    verdict = grade_cells(cells_by_scope.values(), declared=declared, grammar=g)

    remediation = None
    if g.needs_remediation(verdict):
        kind = g.remediation_kind(verdict, vantage, artifact_type=artifact_type)
        if kind is None:
            # Fail-closed, the same shape as "a deviation without a reason: is a
            # schema error". A grade that needs a pointer and has none must not
            # ship — a surface would have to invent the sentence, and inventing
            # it per surface is exactly what remediation-as-data prevents.
            raise grammar_mod.GrammarError(
                "grade '%s' needs a remediation pointer but ops/consistency/grammar.yaml "
                "declares none for vantage '%s'%s" %
                (verdict, vantage, " / artifact_type '%s'" % artifact_type
                 if artifact_type else ""))
        remediation = g.pointer(
            kind,
            name=name,
            member=_offender(verdict, cells_by_scope) or "",
            env=str(goatnet or "").replace("_", "-"),
        )

    return GradedRow(
        subject=subject,
        name=name,
        scope={"goatnet": goatnet, "columns": sorted(cells_by_scope)},
        cells=dict(cells_by_scope),
        grade=verdict,
        why=_why(verdict, cells_by_scope),
        remediation=remediation,
        grounding={"n": len(cells_by_scope), "vantage": vantage, "window_seconds": None},
    )


def declared_scopes(row, members, goatnet):
    """Whether an IntentV1 row carries a declaration that covers this goatnet."""
    declared = (row or {}).get("declared") or {}
    return bool(set(declared) & (set(members) | {goatnet}))


def grade_intent(intent, vantage="repo", grammar=None):
    """Grade a whole IntentV1 document — the entry point every consumer that
    holds only the artifact uses. Components and capabilities are graded;
    node_shape is NOT, because ADR 1084 makes a federated goatnet's nodes
    deliberately unequal in role and comparing them would manufacture skew."""
    g = grammar or grammar_mod.load()
    goatnet = intent.get("goatnet")
    members = list(intent.get("members") or [])
    rows = []

    for comp in intent.get("components") or []:
        cells = comp.get("members") or {}
        declared = any("declared" in (c or {}) for c in cells.values())
        rows.append(grade_row("component", comp.get("name"), cells, goatnet,
                              vantage=vantage, declared=declared,
                              artifact_type=comp.get("artifact_type"), grammar=g))

    for cap in intent.get("capabilities") or []:
        rows.append(grade_row("capability", cap.get("var"), cap.get("members") or {},
                              goatnet, vantage=vantage,
                              declared=declared_scopes(cap, members, goatnet), grammar=g))

    return rows
