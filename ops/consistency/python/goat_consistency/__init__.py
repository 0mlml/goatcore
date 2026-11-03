# SPDX-License-Identifier: Apache-2.0
#
# goat_consistency — the consistency foundation's Python implementation.
#
#   resolve   what a value IS, and how confidently it was read
#   intent    IntentV1: building, hashing, reading, schema-validating
#   signing   verify-at-read for the CF-5 detached signature (ADR 1082's
#             scheme reused verbatim); every read of a committed artifact
#             refuses an unverified body
#   grammar   the six grades, their precedence, colours and remediation kinds,
#             loaded from ops/consistency/grammar.yaml — never defined in code
#   grading   THE comparison, and the only place it lives
#
# Canon: docs/design/consistency-foundation.md §4 (ADR 1110 D2). This package is
# importable on purpose: before CF-2 the resolution semantics were reachable
# only by loading a hyphenated script through importlib.util.spec_from_file_
# location, which was the tell that a library was wearing a script's clothes.
# ops/pins/generate-pins.py and ops/pins/report.py are now thin CLI entry points
# over it, and a second implementation (Rust, on-fabric) is permitted only by
# passing the same golden corpus in ops/consistency/testdata/.
#
# Callers add ops/consistency/python to sys.path (the in-repo convention) and
# `from goat_consistency import ...`.

from . import grading, grammar, intent, resolve, signing  # noqa: F401
from .grading import (ABSENT_KEY, GradedRow, account_for, account_for_render,  # noqa: F401
                      grade_cells, grade_intent, grade_row)
from .grammar import GrammarError  # noqa: F401
from .resolve import Resolution, SchemaError  # noqa: F401
from .signing import IntentVerifyError, Verification  # noqa: F401

__all__ = [
    "ABSENT_KEY",
    "GradedRow",
    "GrammarError",
    "IntentVerifyError",
    "Resolution",
    "SchemaError",
    "Verification",
    "account_for",
    "account_for_render",
    "grade_cells",
    "grade_intent",
    "grade_row",
    "grading",
    "grammar",
    "intent",
    "resolve",
    "signing",
]
