#!/usr/bin/env python3
"""Block 68D summarize.py — render rapid-bringup drill JSONL into a
criterion-check rollup ready to paste into validation-state.md.

Mirrors the ops/ansible/roles/ory/tests/soak/summarize.py pattern
referenced in CLAUDE.md.

Usage:
  ops/bootstrap/drill/summarize.py docs/reports/2026-MM-DD-rapid-bringup-drill-s1-orin-mgmt.jsonl
"""

import json
import sys
from pathlib import Path


def fmt_seconds(s: int) -> str:
    """5045 -> '1h 24m 5s'"""
    h, rem = divmod(s, 3600)
    m, s = divmod(rem, 60)
    if h:
        return f"{h}h {m}m {s}s"
    if m:
        return f"{m}m {s}s"
    return f"{s}s"


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print(f"usage: {argv[0]} <evidence.jsonl>", file=sys.stderr)
        return 1
    path = Path(argv[1])
    if not path.is_file():
        print(f"file not found: {path}", file=sys.stderr)
        return 1

    records = []
    with path.open() as f:
        for line_no, line in enumerate(f, start=1):
            line = line.strip()
            if not line:
                continue
            try:
                records.append(json.loads(line))
            except json.JSONDecodeError as exc:
                print(f"line {line_no}: malformed JSON: {exc}", file=sys.stderr)
                return 1

    header = next((r for r in records if r["phase"] == "drill.start"), None)
    verdict = next((r for r in records if r["phase"] == "drill.verdict"), None)
    if not header or not verdict:
        print("evidence file missing drill.start or drill.verdict record",
              file=sys.stderr)
        return 1

    extra = header.get("extra", {})
    total_elapsed = verdict["elapsed_s"]
    total_budget = verdict["budget_s"]
    overall_verdict = verdict["extra"]["verdict"]

    print(f"# Rapid-bringup drill summary — {extra.get('scenario')} on {extra.get('hardware_class')}")
    print()
    print(f"- Drill date:     `{header['ts']}`")
    print(f"- Scenario:       `{extra.get('scenario')}`")
    print(f"- Hardware class: `{extra.get('hardware_class')}`")
    print(f"- Target:         `{extra.get('target') or '(self)'}`")
    print(f"- Bandwidth shape: `{'YES (Block 68E)' if extra.get('bandwidth_shape') else 'no'}`")
    print(f"- Total elapsed:  **{fmt_seconds(total_elapsed)}** of **{fmt_seconds(total_budget)}**")
    print(f"- Overall verdict: **{overall_verdict}**")
    print()
    print("## Per-phase budget rollup")
    print()
    print("| Phase | Elapsed | Budget | Δ | Verdict |")
    print("|---|---|---|---|---|")

    for record in records:
        if not record["phase"].startswith("phase."):
            continue
        phase = record["phase"][len("phase."):]
        elapsed = record["elapsed_s"]
        budget = record["budget_s"]
        delta = elapsed - budget
        if elapsed <= budget * 1.1:
            verdict_v = "PASS"
        elif elapsed <= budget * 1.25:
            verdict_v = "WARN"
        else:
            verdict_v = "FAIL"
        delta_s = f"+{delta}s" if delta > 0 else f"{delta}s"
        print(f"| `{phase}` | {fmt_seconds(elapsed)} | {fmt_seconds(budget)} | {delta_s} | **{verdict_v}** |")

    print()
    if overall_verdict == "FAIL":
        print("## FAIL — F-NNN finding to draft")
        print()
        print("Slowest WARN/FAIL phase becomes the candidate F-NNN per ADR 1011:")
        print()
        print("- File: `docs/findings/F-NNN-rapid-bringup-<phase>-budget-overrun.md`")
        print("- INDEX: `docs/findings/INDEX.md`")
        print()
        print("Hardening block backlog item should target the long-pole phase.")
    elif overall_verdict == "WARN":
        print("## WARN — within 25% of budget")
        print()
        print("Within tolerance but trending toward the budget edge. Review the")
        print("WARN-marked phases above; if the trend persists across two consecutive")
        print("drills, draft an F-NNN.")
    else:
        print("## PASS — within 10% of budget")
        print()
        print("Cite this evidence file in `docs/project/validation-state.md`.")
        print("If this is the second consecutive PASS drill (one S1, one S2),")
        print("the design earns its ADR promotion per Block 68F.")

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
