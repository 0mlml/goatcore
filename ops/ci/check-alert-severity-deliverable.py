#!/usr/bin/env python3
"""Refuse an alert rule whose `severity` cannot be DELIVERED.

An alert that fires, routes, and is then rejected by the pager is worse than
no alert: every surface a human checks says it worked. Prometheus shows it
firing, Alertmanager shows it active and routed to the pager receiver, and the
only evidence of the failure is one line in the Alertmanager container log.

The pager's refusal looks like this:

    critical-fanout/pagerduty[0]: notify retry canceled due to unrecoverable
    error after 1 attempts: unexpected status code 400: Event object is
    invalid: 'payload.severity' is invalid (must be one of the following:
    'critical', 'warning', 'error' or 'info')

The rule set uses five severity words; PagerDuty Events API v2 accepts four,
and only three of ours are among them. Every `severity: page` alert must
therefore be mapped, or the whole mesh-liveness group, i.e. every "a declared
always-up peer is gone" alarm, cannot be delivered. The 400 is unrecoverable,
so Alertmanager does not even retry.

WHAT THIS CHECKS, and why it is two checks rather than one:

1. Every `severity:` in the rule files is a word this repo has decided on.
   Catches a typo (`sevrity`, `crticial`) and a freelanced new tier.
2. Every such word is MAPPED in the Alertmanager PagerDuty template. This is
   the half that actually failed: `page` was a perfectly deliberate word, used
   consistently, documented — and simply had no translation at the vendor
   boundary. A vocabulary check alone would have passed it.

Exit codes follow the live-state-guard contract (live-state-guard-scheduling.md
§4): 0 clean, 1 the condition this exists to catch, 2 a source could not be
read — which says nothing and must never be read as clean.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]

RULE_DIRS = [
    REPO / "ops/compose/goat-obs/prometheus/rules",
    REPO / "ops/compose/mgmt-host/monitoring/rules",
]
# Roles ship rule files alongside their guard; find them wherever they live.
ROLE_RULES = sorted(REPO.glob("ops/ansible/roles/*/files/*.rules.yml"))

# BOTH templates, because there are two and they can drift. `observability` serves
# most sites; `obs-host` serves sites with a dedicated obs host. A guard that
# checked only the first would pass while the second stayed broken.
ALERTMANAGER_TEMPLATES = [
    REPO / "ops/ansible/roles/observability/templates/alertmanager.yml.j2",
    REPO / "ops/ansible/roles/obs-host/templates/alertmanager.yml.j2",
]

# The vocabulary this repo has decided on. Adding a word here is a deliberate
# act that must be paired with a mapping in the Alertmanager template — which
# is exactly what check 2 enforces.
KNOWN_SEVERITIES = {"critical", "warning", "info", "page", "ticket", "none"}

# What PagerDuty Events API v2 will accept in `payload.severity`. Anything else
# is a hard 400. This is a vendor fact, not a preference.
PAGERDUTY_ACCEPTS = {"critical", "warning", "error", "info"}

SEVERITY_RE = re.compile(r"^\s*severity:\s*['\"]?([A-Za-z_-]+)['\"]?\s*$", re.M)


def rule_files() -> list[Path]:
    files: list[Path] = []
    for d in RULE_DIRS:
        if d.is_dir():
            files.extend(sorted(d.glob("*.yml")))
    files.extend(ROLE_RULES)
    return files


def main() -> int:
    files = rule_files()
    if not files:
        print("check-alert-severity-deliverable: FOUND NO RULE FILES", file=sys.stderr)
        print("  refusing to report clean on an empty read.", file=sys.stderr)
        return 2

    # ---- check 1: vocabulary -------------------------------------------
    used: dict[str, list[str]] = {}
    for f in files:
        try:
            text = f.read_text()
        except OSError as e:
            print(f"check-alert-severity-deliverable: cannot read {f}: {e}", file=sys.stderr)
            return 2
        for sev in SEVERITY_RE.findall(text):
            used.setdefault(sev, []).append(f.relative_to(REPO).as_posix())

    unknown = {s: v for s, v in used.items() if s not in KNOWN_SEVERITIES}

    # ---- check 2: is each word MAPPED at the vendor boundary? -----------
    #
    # The template maps with Go `eq .CommonLabels.severity "<word>"` arms plus a
    # final `else`. A word is deliverable if it is either a PagerDuty word
    # already, or named in an arm. The `else` catch-all is required separately:
    # without it an unmapped word renders empty and 400s just the same.
    mapped: set[str] | None = None
    has_else = True
    for tplpath in ALERTMANAGER_TEMPLATES:
        try:
            tpl = tplpath.read_text()
        except OSError as e:
            print(f"check-alert-severity-deliverable: cannot read {tplpath}: {e}",
                  file=sys.stderr)
            return 2
        if "pagerduty_configs" not in tpl:
            continue
        pd_block = tpl[tpl.find("pagerduty_configs"):]
        this = set(re.findall(r'eq\s+\.CommonLabels\.severity\s+"([A-Za-z_-]+)"', pd_block))
        # INTERSECTION, not union: a severity is only deliverable if EVERY
        # template that can render a pager maps it. A union would let one
        # template's fix mask the other's gap while the repo looks correct.
        mapped = this if mapped is None else (mapped & this)
        if re.search(r"\{\{\s*else\s*\}\}", pd_block) is None:
            has_else = False
    if mapped is None:
        print("check-alert-severity-deliverable: no pagerduty_configs in any template;",
              file=sys.stderr)
        print("  cannot prove severities are deliverable.", file=sys.stderr)
        return 2

    undeliverable = {
        s: v for s, v in used.items()
        if s not in PAGERDUTY_ACCEPTS and s not in mapped and s != "none"
    }

    failed = False

    if unknown:
        failed = True
        print("UNKNOWN SEVERITY WORDS (typo, or a tier nobody decided on):")
        for s, files_ in sorted(unknown.items()):
            print(f"  {s!r} in {len(files_)} file(s): {', '.join(sorted(set(files_))[:4])}")
        print(f"  known vocabulary: {', '.join(sorted(KNOWN_SEVERITIES))}")
        print()

    if undeliverable:
        failed = True
        print("SEVERITIES THAT PAGERDUTY WILL REJECT WITH HTTP 400:")
        for s, files_ in sorted(undeliverable.items()):
            print(f"  {s!r} used in: {', '.join(sorted(set(files_)))}")
        print()
        print("  These alerts FIRE and ROUTE and then vanish at the vendor. Prometheus")
        print("  and Alertmanager will both show them as delivered-to-receiver.")
        print(f"  PagerDuty accepts only: {', '.join(sorted(PAGERDUTY_ACCEPTS))}")
        print(f"  Currently mapped in the template: {', '.join(sorted(mapped)) or '(none)'}")
        print(f"  Fix: add an `eq .CommonLabels.severity \"<word>\"` arm in EVERY:")
        for t in ALERTMANAGER_TEMPLATES:
            print(f"       {t.relative_to(REPO)}")
        print()

    # ---- check 3: the mapping must not be a FOLDED scalar ---------------
    #
    # A `severity: >-` block absorbs every more-indented line that follows it,
    # COMMENTS INCLUDED. The rendered value becomes the template plus whatever
    # prose came after, which PagerDuty rejects with the same HTTP 400 the
    # mapping exists to prevent — while the template file reads correctly and
    # alertmanager reports the alerts routed.
    #
    # A verification pass right after a restart will not see this: nothing has
    # been re-notified yet, so it reports zero errors.
    for tplpath in ALERTMANAGER_TEMPLATES:
        try:
            tpl = tplpath.read_text()
        except OSError:
            continue
        for m in re.finditer(r"^\s*severity:\s*([>|][-+]?)\s*$", tpl, re.M):
            failed = True
            print("SEVERITY IS A FOLDED/LITERAL BLOCK SCALAR:")
            print(f"  {tplpath.relative_to(REPO)} uses `severity: {m.group(1)}`")
            print("  A block scalar swallows the comments that follow it into the value.")
            print("  Put the template on ONE line as a single-quoted scalar, comments ABOVE.")
            print()

    if not has_else:
        failed = True
        print("THE PAGERDUTY SEVERITY TEMPLATE HAS NO `else` ARM.")
        print("  A group whose alerts do not share one severity renders")
        print("  `.CommonLabels.severity` EMPTY, which is also a 400. The else arm")
        print("  must default to a real word — and to a LOUD one, so an unmapped")
        print("  severity wakes someone instead of disappearing.")
        print()

    if failed:
        return 1

    print(f"check-alert-severity-deliverable: PASS — {len(files)} rule file(s), "
          f"severities {{{', '.join(sorted(used))}}} all deliverable")
    return 0


if __name__ == "__main__":
    sys.exit(main())
