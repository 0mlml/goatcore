#!/usr/bin/env bash
# drift-scan.sh — runnable drift critic, executable companion to
# ops/work/roles/drift-critic.md.
#
# Scans a candidate worker branch for cwd / branch / HEAD slippage
# symptoms before integration or PR review. Produces a structured
# report that the captain or operator reads to decide on action.
#
# Usage:
#   ops/scripts/drift-scan.sh <track-name>
#   ops/scripts/drift-scan.sh --branch track/<name>
#
# Output: writes a markdown report to docs/reports/drift-critic-<track>-<date>.md
# AND prints the same to stdout. Exit code: 0 if no drift signals, 1 if any
# flagged.
#
# Composes with — does not replace:
#   - ops/git-hooks/{post-checkout,pre-commit} (mechanical, per-action)
#   - ops/work/roles/autonomous-worker.md (per-worker discipline)
#   - .github/workflows/pr-hygiene.yml (PR-time CI gate)
#   - Operator review at PR merge (final backstop)
set -euo pipefail

if [ $# -eq 0 ]; then
  cat >&2 <<'EOF'
usage: drift-scan.sh <track-name>
       drift-scan.sh --branch track/<name>

Scans a candidate worker branch for drift symptoms. Reports to
docs/reports/drift-critic-<track>-<date>.md and stdout.
Exit code: 0 = no drift signals; 1 = any flagged.
EOF
  exit 1
fi

# Parse args
BRANCH=""
TRACK=""
case "$1" in
  --branch)
    BRANCH="$2"
    TRACK="${BRANCH#track/}"
    ;;
  *)
    TRACK="$1"
    BRANCH="track/${TRACK}"
    ;;
esac

REPO_ROOT="$(git rev-parse --show-toplevel)"
DATE="$(date -u +%Y-%m-%dT%H%M%SZ)"
REPORT="${REPO_ROOT}/docs/reports/drift-critic-${TRACK}-${DATE}.md"
mkdir -p "$(dirname "$REPORT")"

# Find merge base of branch with origin/main
if ! git rev-parse --quiet --verify "$BRANCH" >/dev/null 2>&1; then
  echo "error: branch '$BRANCH' not found locally; fetch first or pass --branch <full-ref>" >&2
  exit 2
fi
git fetch origin main --quiet 2>/dev/null || true
MERGE_BASE="$(git merge-base "$BRANCH" origin/main 2>/dev/null || git merge-base "$BRANCH" main)"
COMMITS="$(git log --format='%H' "${MERGE_BASE}..${BRANCH}")"
COMMIT_COUNT="$(echo "$COMMITS" | grep -c . || true)"

# Track verdict + flagged items across checks
VERDICT="✅"
FLAGGED=()

flag() {
  FLAGGED+=("$1")
  if [ "$VERDICT" = "✅" ]; then VERDICT="⚠️"; fi
}

flag_hard() {
  FLAGGED+=("**[HARD]** $1")
  VERDICT="❌"
}

# --- Check 1: Track-trailer presence on each commit ---
TRAILER_VERDICT="✅"
TRAILER_NOTES=""
while IFS= read -r sha; do
  [ -z "$sha" ] && continue
  msg="$(git log -1 --format='%B' "$sha")"
  if echo "$msg" | grep -qE "\[track:[[:space:]]*${TRACK}\]"; then
    TRAILER_NOTES+="  ✓ ${sha:0:7} carries [track: ${TRACK}]"$'\n'
  elif echo "$msg" | grep -qE "\[track:"; then
    other_track="$(echo "$msg" | grep -oE '\[track:[^]]+\]' | head -1)"
    flag_hard "Commit ${sha:0:7} carries wrong track trailer: ${other_track}"
    TRAILER_NOTES+="  ❌ ${sha:0:7} wrong track trailer: ${other_track}"$'\n'
    TRAILER_VERDICT="❌"
  else
    flag "Commit ${sha:0:7} missing [track:] trailer"
    TRAILER_NOTES+="  ⚠️ ${sha:0:7} no [track:] trailer"$'\n'
    if [ "$TRAILER_VERDICT" != "❌" ]; then TRAILER_VERDICT="⚠️"; fi
  fi
done <<< "$COMMITS"

# --- Check 2: Diff symptoms — cross-track / master-only / unexpected absolute paths ---
DIFF_VERDICT="✅"
DIFF_NOTES=""
DIFF_FILES="$(git diff --name-only "${MERGE_BASE}..${BRANCH}")"
# Patterns that suggest cross-track contamination (other tracks' active files)
SUSPICIOUS_PATTERNS=(
  "^docs/operations/wg-cp0-bundle-ceremony.md$"
  "^ops/admit-audit/"
  "^ops/enrollment/"
  "^.iso-mode.env$"
  "^.claude/settings.local.json$"
  "^.claude/worktrees/"
)
for path in $DIFF_FILES; do
  for pat in "${SUSPICIOUS_PATTERNS[@]}"; do
    if [[ "$path" =~ $pat ]]; then
      flag "Suspicious diff path (possible cross-track or per-worktree leak): $path"
      DIFF_NOTES+="  ⚠️ $path matches suspicious pattern: $pat"$'\n'
      DIFF_VERDICT="⚠️"
    fi
  done
done

# Look for hardcoded master-worktree absolute paths in source diff
ABS_PATH_HITS="$(git diff "${MERGE_BASE}..${BRANCH}" 2>/dev/null | grep -E '^\+.*/Users/[a-z]+/src/' | head -5 || true)"
if [ -n "$ABS_PATH_HITS" ]; then
  flag "Diff contains hardcoded /Users/.../src/ absolute paths"
  DIFF_NOTES+="  ⚠️ hardcoded absolute paths in diff (sample):"$'\n'
  while IFS= read -r line; do
    DIFF_NOTES+="     $line"$'\n'
  done <<< "$ABS_PATH_HITS"
  DIFF_VERDICT="⚠️"
fi

if [ -z "$DIFF_NOTES" ]; then
  DIFF_NOTES="  ✓ No suspicious paths in diff."$'\n'
fi

# --- Check 3: Worktree state on disk ---
WT_VERDICT="✅"
WT_NOTES=""
WT_PATH=".claude/worktrees/${TRACK}"
if [ -d "${REPO_ROOT}/${WT_PATH}" ]; then
  WT_BRANCH="$(git -C "${REPO_ROOT}/${WT_PATH}" symbolic-ref --quiet --short HEAD 2>/dev/null || echo 'DETACHED')"
  WT_DIRTY="$(git -C "${REPO_ROOT}/${WT_PATH}" status --short 2>/dev/null | head -1)"
  if [ "$WT_BRANCH" = "$BRANCH" ]; then
    WT_NOTES+="  ✓ Worktree at ${WT_PATH} on branch ${WT_BRANCH}"$'\n'
  else
    flag "Worktree at ${WT_PATH} on UNEXPECTED branch ${WT_BRANCH} (expected ${BRANCH})"
    WT_NOTES+="  ❌ Worktree on unexpected branch: ${WT_BRANCH}"$'\n'
    WT_VERDICT="❌"
  fi
  if [ -n "$WT_DIRTY" ]; then
    WT_NOTES+="  ℹ️  Worktree is dirty (uncommitted state):"$'\n'
    git -C "${REPO_ROOT}/${WT_PATH}" status --short | head -10 | while IFS= read -r line; do
      WT_NOTES+="       $line"$'\n'
    done
  else
    WT_NOTES+="  ✓ Worktree clean."$'\n'
  fi
else
  WT_NOTES+="  ℹ️  Worktree at ${WT_PATH} does not exist on this clone (may live elsewhere or be cleaned up)."$'\n'
fi

# --- Check 4: Bus events (when ADR 0012 ships) ---
BUS_VERDICT="N/A"
BUS_NOTES="  (ADR 0012 session-bus not yet wired; this check is a no-op until it lands)"$'\n'
if command -v "${REPO_ROOT}/ops/work/work" >/dev/null 2>&1; then
  if "${REPO_ROOT}/ops/work/work" bus tail --help >/dev/null 2>&1; then
    BUS_VERDICT="✅"
    BUS_NOTES="  ✓ Bus is wired; manual scan via 'work bus tail --track ${TRACK}' recommended."$'\n'
  fi
fi

# --- Check 5: active-work per-track-file sanity ---
# Post-migration layout: each track is its own yaml file at
# docs/project/active-work/tracks/<name>.yaml (split per ADR — see
# ops/work/README.md). The legacy monolithic active-work.yaml is gone.
AW_VERDICT="✅"
AW_NOTES=""
AW_FILE="${REPO_ROOT}/docs/project/active-work/tracks/${TRACK}.yaml"
if [ -f "$AW_FILE" ]; then
  AW_NOTES="  ✓ Track file found at docs/project/active-work/tracks/${TRACK}.yaml"$'\n'
  if grep -q "^mode: worktree" "$AW_FILE"; then
    AW_NOTES+="  ✓ mode: worktree present"$'\n'
  elif grep -q "^mode:" "$AW_FILE"; then
    TRACK_MODE="$(grep "^mode:" "$AW_FILE" | head -1 | awk '{print $2}')"
    AW_NOTES+="  ℹ️  mode: ${TRACK_MODE} (not worktree — is this intentional?)"$'\n'
  else
    AW_NOTES+="  ℹ️  no mode field on track file (legacy track or pre-Block-48O pickup)"$'\n'
  fi
else
  flag "Track file not found at docs/project/active-work/tracks/${TRACK}.yaml"
  AW_NOTES+="  ⚠️ no track file for '${TRACK}' under docs/project/active-work/tracks/"$'\n'
  AW_VERDICT="⚠️"
fi

# --- Compose report ---
{
  echo "# Drift critic report — ${TRACK} — ${DATE}"
  echo ""
  echo "**Branch**: \`${BRANCH}\`"
  echo "**Merge base with origin/main**: \`${MERGE_BASE}\`"
  echo "**Commits since merge base**: ${COMMIT_COUNT}"
  echo ""
  echo "| Check | Verdict | Evidence |"
  echo "|---|---|---|"
  echo "| Commit shape (track trailers) | ${TRAILER_VERDICT} | see notes below |"
  echo "| Diff symptoms (cross-track / master-only / abs-paths) | ${DIFF_VERDICT} | see notes below |"
  echo "| Worktree state on disk | ${WT_VERDICT} | see notes below |"
  echo "| Bus events (ADR 0012 wire-up) | ${BUS_VERDICT} | see notes below |"
  echo "| active-work per-track file | ${AW_VERDICT} | see notes below |"
  echo ""
  echo "**Overall verdict**: ${VERDICT}"
  echo ""
  if [ ${#FLAGGED[@]} -gt 0 ]; then
    echo "## Flagged items"
    echo ""
    for item in "${FLAGGED[@]}"; do
      echo "- $item"
    done
    echo ""
  fi
  echo "## Detailed notes"
  echo ""
  echo "### Commit shape"
  echo '```'
  echo "$TRAILER_NOTES"
  echo '```'
  echo ""
  echo "### Diff symptoms"
  echo '```'
  echo "$DIFF_NOTES"
  echo '```'
  echo ""
  echo "### Worktree state"
  echo '```'
  echo "$WT_NOTES"
  echo '```'
  echo ""
  echo "### Bus events"
  echo '```'
  echo "$BUS_NOTES"
  echo '```'
  echo ""
  echo "### active-work per-track file"
  echo '```'
  echo "$AW_NOTES"
  echo '```'
} > "$REPORT"

cat "$REPORT"
echo ""
echo "report written to: $REPORT"

if [ "$VERDICT" = "❌" ]; then
  exit 1
fi
exit 0
