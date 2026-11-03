#!/usr/bin/env bash
# check-apt-steps-are-bounded.sh — a CI step that shells out to apt must be
# bounded in time.
#
# THE FAILURE. `sudo apt-get update` on a GitHub runner reaches out to the
# Azure/Ubuntu mirrors. When a mirror stalls, apt does not fail — it waits, with
# no default timeout and no retry. The step sits `in_progress` until something
# outside it intervenes.
#
# MEASURED . The `go-vuln (ops/enrollment)` matrix leg hung. Job step
# timeline from the Actions API:
#
#   Set up job                                          1s   success
#   actions/checkout@v4                                11s   success
#   actions/setup-go@v5                                10s   success
#   Install CGO deps for enrollment (libpcsclite)      ...   IN PROGRESS  <-- here
#   govulncheck                                              never started
#
# It was NOT govulncheck. `ops/enrollment` is the only module in that matrix
# carrying an apt step (it needs libpcsclite-dev for the go-piv CGO binding),
# which is exactly why it was the only leg that ever hung. Every other module
# finished in seconds. Earlier sightings of the same shape: 129.4 minutes on one
# PR, and one run that burned 360 minutes — 95% of that job's billed minutes in
# a single wedge. The docs-i18n job hung the same way inside "Install gettext".
#
# WHY THE JOB CAP IS NOT ENOUGH. `timeout-minutes` at JOB level (added fleet-wide
# by #284) bounds the damage but not the diagnosis: the job burns its whole cap
# and then reports a generic timeout, so the reader cannot see that apt was the
# stall. A STEP-level bound fails in minutes, names the step, and leaves the rest
# of the job's budget for the work it was actually there to do.
#
# THE RULE. Any workflow step whose `run:` invokes apt-get must carry its own
# `timeout-minutes`. The retry/timeout options on the apt invocation itself
# (`Acquire::Retries`, `Acquire::*::Timeout`) are the belt — they let a transient
# mirror stall self-heal — and the step bound is the braces.
#
# --self-test proves it RED against an unbounded fixture first.
set -euo pipefail

scan() {
  local root="$1"
  python3 - "$root" <<'PY'
import glob, os, re, sys

root = sys.argv[1]
bad = []
checked = 0

for path in sorted(glob.glob(os.path.join(root, "*.yml"))
                   + glob.glob(os.path.join(root, "*.yaml"))):
    lines = open(path, encoding="utf-8").read().split("\n")
    i = 0
    while i < len(lines):
        m = re.match(r"^(\s*)-\s+(name|uses):", lines[i])
        if not m:
            i += 1
            continue
        indent = m.group(1)
        block = [lines[i]]
        j = i + 1
        while j < len(lines):
            nxt = lines[j]
            if nxt.strip() == "":
                block.append(nxt); j += 1; continue
            if re.match(r"^" + re.escape(indent) + r"-\s", nxt):
                break
            if len(nxt) - len(nxt.lstrip()) <= len(indent):
                break
            block.append(nxt); j += 1
        btxt = "\n".join(block)
        if re.search(r"\bapt-get\b", btxt):
            checked += 1
            if not re.search(r"^\s+timeout-minutes:", btxt, re.M):
                name = re.sub(r"^\s*-\s+(name|uses):\s*", "", block[0]).strip()
                bad.append((os.path.basename(path), name))
        i = j

if bad:
    print("FAIL: apt step(s) with no timeout-minutes — an apt mirror stall has no")
    print("      timeout of its own, so these can sit in_progress indefinitely:")
    for f, n in bad:
        print(f"        {f}: {n}")
    print()
    print("      Add `timeout-minutes: 5` to the step, and pass retry/timeout")
    print("      options on the apt call itself:")
    print("        sudo apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=20 \\")
    print("                     -o Acquire::https::Timeout=20 update -qq")
    print("      See F-368 for the measured 360-minute wedge this prevents.")
    sys.exit(1)

print(f"OK: every apt step is time-bounded ({checked} checked)")
PY
}

if [ "${1:-}" = "--self-test" ]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  mkdir -p "$tmp/red" "$tmp/green"
  cat > "$tmp/red/w.yml" <<'EOF'
jobs:
  a:
    steps:
      - name: Install deps
        run: |
          sudo apt-get update -qq
          sudo apt-get install -y thing
EOF
  cat > "$tmp/green/w.yml" <<'EOF'
jobs:
  a:
    steps:
      - name: Install deps
        timeout-minutes: 5
        run: |
          sudo apt-get -o Acquire::Retries=3 update -qq
          sudo apt-get install -y thing
      - name: No apt here
        run: echo hi
EOF
  if scan "$tmp/red" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed an unbounded apt step"
    exit 1
  fi
  echo "self-test: RED proven — an apt step with no timeout-minutes fails"
  scan "$tmp/green" >/dev/null
  echo "self-test: GREEN proven — bounded apt step passes, non-apt steps ignored"
  echo "OK: apt-steps-are-bounded self-test — red-first proof holds"
  exit 0
fi

[ $# -eq 0 ] && cd "$(git rev-parse --show-toplevel)"
scan "${1:-.github/workflows}"
