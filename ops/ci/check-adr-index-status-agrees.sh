#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-adr-index-status-agrees.sh — an ADR's Status line and its INDEX.md row
# must say the same thing.
#
# THE THIRD OF THREE, and the set only makes sense together:
#
#   check-adr-number-uniqueness.sh   is the row DUPLICATED?
#   check-adr-index-complete.sh      does the row EXIST?
#   this one                         is the row TRUE?
#
# The first two shipped and the third did not, so `INDEX.md` — which calls
# itself the live status board — could disagree with the ADRs it indexes and
# nothing would notice. Swept: **8 of 331** did. A reader checking
# whether a decision is binding got the wrong answer for eight of them.
#
# That is the same shape as the guards found running on nothing that week, and
# as F-312 and F-337: **absence and staleness are the states nobody checks.**
# Asking "is it there" is easy and asking "is it right" is the one that rots.
#
# RATCHET, not a gate. The eight known disagreements are baselined because
# WHICH SIDE IS CORRECT DIFFERS PER ADR and belongs to whoever owns each one —
# resolving them by fiat here would be inventing decisions, which is exactly
# the failure the ADR process exists to prevent. It fails on a NEW one.
#
# Compares the FIRST word only, splitting on whitespace/paren/dash/comma AND
# semicolon-colon-period: real rows read 'Accepted; production backend implemented
# Block 22M' and 'Proposed; amended '. Without the semicolon three
# ADRs reported as drifted when both sides plainly agree.
#
# Compares the FIRST word only. INDEX rows carry "Accepted (
# platform)" and files carry "Accepted" — matching the whole string would fail
# on every row for a formatting difference nobody cares about, which is how a
# guard gets disabled instead of fixed.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

INDEX="docs/adr/INDEX.md"
[ -r "$INDEX" ] || { echo "check-adr-index-status-agrees: missing $INDEX" >&2; exit 1; }

# Known disagreements — THIRTEEN, not the eight found by the
# first sweep. The extra five (0321 0827 1039 1040 1131) surfaced only once
# this parser read all four Status formats in use; a narrower parse skipped
# 85 of 331 files and reported those as clean. SHRINK-ONLY — an entry leaves this list when
# its owner reconciles the two sides, and the check says so when that happens.
# Never add to it to silence a failure.
BASELINE="0215 0316 0321 0401 0402 0708 0827 0840 0858 1039 1040 1114 1131"

python3 - "$INDEX" "$BASELINE" <<'PYEOF'
import os, re, sys, glob

index_path, baseline = sys.argv[1], set(sys.argv[2].split())

# An ADR's OWN row: "| 0123 | [title](file.md) | Accepted (date, who) | ..."
row_status = {}
for line in open(index_path, encoding="utf-8"):
    m = re.match(r'^\|\s*(\d{4})\s*\|[^|]*\|([^|]*)\|', line)
    if m:
        num, status = m.group(1), m.group(2)
        # strip markdown emphasis and any parenthetical, keep the first word
        s = re.sub(r'[*_`]', '', status).strip()
        s = re.split(r'[\s(—,;:.]', s)[0] if s else ''
        row_status[num] = s

VOCAB = {"accepted", "proposed", "superseded", "rejected",
         "deprecated", "revised", "draft", "marked", "retired"}
disagree, missing_status, unparseable = [], [], []
for path in sorted(glob.glob("docs/adr/[0-9][0-9][0-9][0-9]-*.md")):
    num = os.path.basename(path)[:4]
    if num not in row_status:
        continue                      # absence is check-adr-index-complete's job
    head = "".join(open(path, encoding="utf-8").readlines()[:40])
    # Files use "- **Status:** Accepted" or a "| Status | Accepted |" table row
    # TABLE FORM FIRST. Trying the bullet form first matched an unrelated
    # line in ADR 1000 and reported its status as 'tracker' — a guard whose
    # own parser invents a disagreement is worse than no guard.
    # FOUR formats are in use across 331 ADRs and all four must parse, or the
    # guard silently skips the files it cannot read — which is the exact
    # "runs on nothing" fault it belongs to the family of. Requiring a leading
    # bullet alone skipped 85 of 331 (a quarter of the corpus) in the first
    # draft: bare `**Status:** Accepted.` and YAML frontmatter
    # `status: Accepted` are both common.
    m = (re.search(r'^\|\s*Status\s*\|\s*([^|]+)\|', head, re.M)
         or re.search(r'^\s*[-*]\s*\*\*Status:?\*\*:?\s*(.+)$', head, re.M)
         or re.search(r'^\*\*Status:?\*\*:?\s*(.+)$', head, re.M)
         or re.search(r'^status:\s*(.+)$', head, re.M | re.I))
    if not m:
        missing_status.append(num)
        continue
    f = re.sub(r'[*_`]', '', m.group(1)).strip()
    f = re.split(r'[\s(—,;:.]', f)[0] if f else ''
    # COMPARE ONLY WHEN BOTH SIDES PARSE TO A KNOWN STATUS WORD.
    # Several ADRs write prose where a status goes — 1045 is
    # "Declare + reveal **Accepted — deployed..." and 1066 is
    # "**Phase 1 deployed + verified...". A first-word comparison on those
    # yields 'Declare' vs 'Proposed' and 'Phase' vs 'Phases', which are not
    # disagreements about the DECISION, they are the parser guessing. A guard
    # that invents findings gets muted, and a muted guard is the thing this
    # whole family exists to prevent — so it declines to judge instead.
    if f.lower() not in VOCAB or row_status[num].lower() not in VOCAB:
        unparseable.append(num)
        continue
    if f.lower() != row_status[num].lower():
        disagree.append((num, f, row_status[num]))

new = [d for d in disagree if d[0] not in baseline]
healed = sorted(baseline - {d[0] for d in disagree})

if new:
    print("check-adr-index-status-agrees: FAIL — ADR(s) whose Status disagrees with INDEX.md:", file=sys.stderr)
    for num, f, r in new:
        print(f"    {num}  file says {f!r}  |  index says {r!r}", file=sys.stderr)
    print("", file=sys.stderr)
    print("  INDEX.md is the live status board. A reader checking whether a", file=sys.stderr)
    print("  decision is binding gets the wrong answer. Fix whichever side is", file=sys.stderr)
    print("  wrong — that is a judgement about the DECISION, not a formatting", file=sys.stderr)
    print("  fix, so it belongs to the ADR's owner.", file=sys.stderr)
    raise SystemExit(1)

extra = (f"; {len(missing_status)} without a Status line, "
         f"{len(unparseable)} whose status is prose on one side") if (missing_status or unparseable) else ""
if healed:
    print(f"check-adr-index-status-agrees: OK — and {' '.join(healed)} now agree; "
          f"drop from BASELINE{extra}")
else:
    print(f"check-adr-index-status-agrees: OK — {len(disagree)} known disagreement(s) carried{extra}")
PYEOF
