# SPDX-License-Identifier: Apache-2.0
#
# grammar.py — the loader for ops/consistency/grammar.yaml.
#
# This module reads the grammar; it does not contain one. Every grade name,
# every precedence position, every remediation pointer and every colour comes
# out of the committed YAML, because the whole point of ADR 1110 D2 is that the
# grammar is DATA that two implementations execute rather than code that one of
# them owns. If you find yourself about to hard-code a grade name here, the
# corpus is where that behaviour belongs.
#
# Canon: docs/design/consistency-foundation.md §4.2.

import os

import yaml

from .paths import GRAMMAR_FILE


class GrammarError(Exception):
    """The grammar file is not a usable grammar. Fail loud: every grade in the
    system is defined by this file, so a malformed one is not a degraded mode."""


class Grammar(object):
    def __init__(self, data):
        self.version = data.get("version")
        self.grades = {g["name"]: g for g in (data.get("grades") or [])}
        self.precedence = list(data.get("precedence") or [])
        rem = data.get("remediation") or {}
        self.kinds = rem.get("kinds") or {}
        self.dispatch = rem.get("dispatch") or {}
        self._validate()

    def _validate(self):
        if not self.grades:
            raise GrammarError("no grades declared")
        if set(self.precedence) != set(self.grades):
            missing = sorted(set(self.grades) - set(self.precedence))
            extra = sorted(set(self.precedence) - set(self.grades))
            raise GrammarError(
                "precedence and grades disagree (missing from precedence: %s; "
                "unknown in precedence: %s) — a grade with no precedence "
                "position cannot be compared, which is worse than not having it"
                % (missing or "none", extra or "none"))
        for name, spec in self.grades.items():
            if spec.get("needs_remediation") is None:
                raise GrammarError("grade '%s' does not say whether it needs a "
                                   "remediation pointer" % name)
            if not spec.get("color"):
                raise GrammarError("grade '%s' has no colour; a grade that "
                                   "cannot render is not a grade" % name)
        for vantage, table in self.dispatch.items():
            for grade, target in table.items():
                if grade not in self.grades:
                    raise GrammarError("dispatch[%s] maps unknown grade '%s'"
                                       % (vantage, grade))
                for kind in self._targets(target):
                    if kind not in self.kinds:
                        raise GrammarError("dispatch[%s][%s] names unknown "
                                           "remediation kind '%s'" % (vantage, grade, kind))

    @staticmethod
    def _targets(target):
        if isinstance(target, str):
            return [target]
        return list((target.get("by_artifact_type") or {}).values())

    # -- the comparison primitives ------------------------------------------

    def rank(self, grade):
        try:
            return self.precedence.index(grade)
        except ValueError:
            raise GrammarError("'%s' is not a grade in this grammar" % grade)

    def worst(self, grades):
        """Worst-wins, the rule for collapsing cells into a row and rows into a
        scope. Empty input is `indeterminate`: a scope with nothing in it has
        not been observed, and absence of signal is never green."""
        grades = list(grades)
        if not grades:
            return "indeterminate"
        return min(grades, key=self.rank)

    def color(self, grade, scope_empty=False):
        spec = self.grades.get(grade)
        if spec is None:
            raise GrammarError("'%s' is not a grade in this grammar" % grade)
        if scope_empty and spec.get("color_when_scope_empty"):
            return spec["color_when_scope_empty"]
        return spec["color"]

    def needs_remediation(self, grade):
        spec = self.grades.get(grade)
        if spec is None:
            raise GrammarError("'%s' is not a grade in this grammar" % grade)
        return bool(spec["needs_remediation"])

    def remediation_kind(self, grade, vantage, artifact_type=None):
        """Which kind of pointer this grade earns from this vantage, or None
        when the grade needs no pointer."""
        if not self.needs_remediation(grade):
            return None
        table = self.dispatch.get(vantage) or {}
        target = table.get(grade)
        if target is None:
            return None
        if isinstance(target, str):
            return target
        by_type = target.get("by_artifact_type") or {}
        return by_type.get(artifact_type)

    def pointer(self, kind, **fields):
        spec = self.kinds.get(kind)
        if spec is None:
            raise GrammarError("'%s' is not a remediation kind in this grammar" % kind)
        pointer = spec["pointer"]
        for key, value in fields.items():
            pointer = pointer.replace("{%s}" % key, str(value))
        return {"kind": kind, "pointer": pointer, "refuses": bool(spec.get("refuses"))}


_CACHE = {}


def load(path=None):
    path = path or GRAMMAR_FILE
    key = os.path.abspath(path)
    if key not in _CACHE:
        with open(path) as f:
            _CACHE[key] = Grammar(yaml.safe_load(f) or {})
    return _CACHE[key]
