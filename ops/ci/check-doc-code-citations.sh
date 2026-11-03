#!/usr/bin/env bash
# ops/ci/check-doc-code-citations.sh — assert that every `path:line` citation in
# a code-cited design doc points at a line that EXISTS.
#
# WHY: docs/design/identity-access-and-onboarding-reference.md is the cross-plane
# ground-truth map. Its whole contract is that every load-bearing claim cites
# `path:line`, and its own closing line says a stale citation is worse than none
# — because a reader who follows one lands on unrelated code and either believes
# it or loses trust in the doc. Nothing fenced that contract, so accuracy decayed
# silently between audits. all 17 of its `admin.rs` citations were
# wrong: most had drifted 1-3 lines when #2043 landed mid-review, and two had
# been pointing at unrelated code for weeks (the `goat admin operator onboard`
# citation resolved to a `--groups` flag doc comment; the Ansible-onboard-leg
# citation resolved to `run_role_state`).
#
# THIS IS A FLOOR, NOT A SOLUTION. Read this before trusting a green run:
#
#   CAUGHT      a citation whose line number is past the end of the file, or
#               whose path no longer exists. That is the crude half of decay,
#               and it is what happens when a cited file SHRINKS or moves.
#
#   NOT CAUGHT  a citation that points at a wrong-but-existing line. Both of the
#               weeks-stale citations above were this shape — the file was long
#               enough, the line simply described something else. A fence cannot
#               know what a claim MEANT without reading the prose.
#
# So: green here means "no citation is out of range", never "the citations are
# correct". Re-verifying a claim against the code it cites is still a human (or
# agent) act, and the doc still needs periodic re-anchoring after any large
# refactor of a cited file. What this buys is that the crude failures stop
# reaching main, and that the cost of the re-anchoring pass drops.
#
# SHORTHAND. The doc cites a full path on first mention and a shorthand suffix
# thereafter (`ops/goat-zenoh-portal/internal/api/auth.go:9-11`, later just
# `auth.go:148`). Shorthand decays identically, so it is resolved by path-suffix
# match: first against full paths named elsewhere in the SAME doc (a local
# mention is the best evidence of intent), then against the tracked file list.
# A shorthand that matches nothing, or matches MORE THAN ONE file, is reported
# as UNRESOLVED and does not fail the run — guessing which `config.go` was meant
# is exactly the inference this guard exists to avoid. Unresolved citations are
# printed so the count is visible rather than quietly zero.
#
# Usage: ./ops/ci/check-doc-code-citations.sh [doc ...]
# Default doc: the identity reference map. Add others as they earn the same
# discipline; a doc without `path:line` citations passes trivially.
set -euo pipefail

DOCS=("$@")
if [[ ${#DOCS[@]} -eq 0 ]]; then
  DOCS=("docs/design/identity-access-and-onboarding-reference.md")
fi

# Citations are repo-relative, so resolve them from the repo root regardless of
# where this was invoked from.
cd "$(git rev-parse --show-toplevel)"

python3 - "${DOCS[@]}" <<'PY'
import pathlib, re, subprocess, sys

# A citation: a backticked token ending `<path>:<spec>` where spec is one or
# more comma-separated line numbers / ranges. `path` may be a full repo path or
# a shorthand suffix of one.
CITE = re.compile(
    r'`(?P<path>[A-Za-z0-9_][A-Za-z0-9_./+-]*\.[A-Za-z0-9]+)'
    r':(?P<spec>\d+(?:-\d+)?(?:\s*,\s*\d+(?:-\d+)?)*)`'
)
# Any backticked path-shaped token, cited with a line number or not — the pool
# shorthand is resolved against.
PATHLIKE = re.compile(r'`(?P<path>[A-Za-z0-9_][A-Za-z0-9_./+-]*\.[A-Za-z0-9]+)(?::[\d,\s-]+)?`')

def line_count(p: pathlib.Path) -> int:
    # Count lines the way an editor does: a trailing newline does not create a
    # further addressable line, so `sed -n '$p'` and this agree.
    text = p.read_text(encoding="utf-8", errors="replace")
    if text == "":
        return 0
    n = text.count("\n")
    return n if text.endswith("\n") else n + 1

# Tracked files, for resolving shorthand that the doc never spells out in full.
# git ls-files (not a walk) so build output, vendored trees and gitignored
# operator dirs cannot create phantom matches.
TRACKED = subprocess.run(
    ["git", "ls-files"], capture_output=True, text=True, check=True
).stdout.splitlines()

def suffix_matches(raw: str, candidates) -> list:
    return [c for c in candidates if c == raw or c.endswith("/" + raw)]

failures, unresolved, missing_docs, checked, docs_seen = [], [], [], 0, 0

for doc_arg in sys.argv[1:]:
    doc = pathlib.Path(doc_arg)
    if not doc.is_file():
        missing_docs.append(doc_arg)
        continue
    docs_seen += 1
    text = doc.read_text(encoding="utf-8", errors="replace")

    # Full repo paths named anywhere in this doc — the shorthand resolution pool.
    pool = sorted({
        m.group("path") for m in PATHLIKE.finditer(text)
        if pathlib.Path(m.group("path")).is_file()
    })

    for m in CITE.finditer(text):
        raw, spec = m.group("path"), m.group("spec")
        cite = f"`{raw}:{spec}`"
        line_no = text.count("\n", 0, m.start()) + 1

        target = pathlib.Path(raw)
        if not target.is_file():
            # Shorthand. Prefer a full path this doc names itself; fall back to
            # the tracked file list. Refuse on ambiguity in both tiers.
            hits = suffix_matches(raw, pool) or suffix_matches(raw, TRACKED)
            if len(hits) != 1:
                why = "matches no tracked file" if not hits \
                      else f"is ambiguous — matches {len(hits)}: {', '.join(sorted(hits)[:4])}"
                unresolved.append(f"{doc}:{line_no}  {cite} — {why}")
                continue
            target = pathlib.Path(hits[0])

        total = line_count(target)
        for part in (p.strip() for p in spec.split(",")):
            lo, _, hi = part.partition("-")
            lo, hi = int(lo), int(hi or lo)
            checked += 1
            if lo == 0:
                failures.append(f"{doc}:{line_no}  {cite} -> {target}: line 0 does not exist (files are 1-indexed)")
            elif hi < lo:
                failures.append(f"{doc}:{line_no}  {cite} -> {target}: inverted range {lo}-{hi}")
            elif max(lo, hi) > total:
                failures.append(
                    f"{doc}:{line_no}  {cite} -> {target}: cites line "
                    f"{max(lo, hi)} but the file has {total}"
                )

for u in unresolved:
    print(f"note: UNRESOLVED {u}")

if missing_docs:
    print()
    print("FAIL: doc(s) not found — was one renamed without updating this guard's default?")
    for d in missing_docs:
        print(f"  {d}")
    sys.exit(2)

if failures:
    print()
    print(f"FAIL: {len(failures)} out-of-range code citation(s):")
    print()
    for f in failures:
        print(f"  {f}")
    print()
    print("A citation that points past the end of its file is stale — the code moved")
    print("or shrank underneath the doc. Re-anchor it by finding the CLAIM in the")
    print("current file (grep the symbol, not the old line number) and citing where it")
    print("lives now. Do not simply clamp the number into range: that produces a")
    print("citation this guard accepts and a reader cannot trust.")
    sys.exit(1)

print(f"ok: {checked} code citation(s) in {docs_seen} doc(s) are within their files"
      + (f"; {len(unresolved)} shorthand citation(s) unresolved (not checked)" if unresolved else ""))
PY
