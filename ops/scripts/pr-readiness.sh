#!/usr/bin/env bash
# pr-readiness — pre-push hard gate for the current branch.
#
# Hard-fails (exit 1) when any of:
#   - HEAD's merge-base with origin/main is more than MAX_BEHIND commits
#     behind origin/main (default 50). Orphan-resurrection trigger.
#   - Any ahead-commit's SHA matches a recently-merged-and-deleted branch
#     ref on origin (per gh PR list state=MERGED with deleted-branch).
#   - Diff vs origin/main exceeds MAX_FILES (default 50) OR MAX_LINES
#     (default 5000) — runaway-diff signal.
#   - Working tree is dirty (forces a clean push).
#   - Any ahead-commit's Signed-off-by trailer is missing OR present but
#     does not match that commit's own author (DCO; mirrors pr-hygiene.yml's
#     "Developer Certificate of Origin" step).
#   - Any ahead-commit is unsigned (%G? = N) — ssh signing fails silently.
#   - An open PR for this branch has an EMPTY status-check rollup, or a
#     completed check that did not pass.
#   - A track/* branch has any ahead-commit missing its [track: <name>]
#     trailer (ADR 0006; mirrors pr-hygiene.yml's "track discipline" step).
#   - Any ahead-commit message carries an agent-tooling / AI-attribution
#     reference (claude.ai, Claude-Session:, anthropic.com — mirrors
#     agent-references.yml's commit-message check).
#   - An open PR already exists for this branch and its title does not
#     follow Conventional Commits with a lowercase scope (mirrors
#     pr-hygiene.yml's "Conventional Commits PR title" step; main is
#     squash-merged, so the PR title is what lands on main).
#
# Run from any branch worktree before `git push` / `gh pr create`. Composes
# with — does not replace — pr-hygiene.yml + agent-references.yml CI gates
# (which run post-push).
#
# Block 48Q. See docs/operations/multi-agent-substrate-install.md.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

REMOTE="${REMOTE:-origin}"
TRUNK="${TRUNK:-main}"
TRUNK_REF="${REMOTE}/${TRUNK}"
MAX_BEHIND="${PR_READINESS_MAX_BEHIND:-50}"
MAX_FILES="${PR_READINESS_MAX_FILES:-50}"
MAX_LINES="${PR_READINESS_MAX_LINES:-5000}"

red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
yel()   { printf '\033[33m%s\033[0m\n' "$*"; }

FAIL=0
fail() { red "FAIL: $*"; FAIL=$((FAIL + 1)); }
pass() { green "  ok: $*"; }

# Refuse on master worktree (we should not be running pr-readiness on main itself).
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" == "$TRUNK" ]]; then
  red "FAIL: refusing to run pr-readiness on $TRUNK itself."
  red "      Switch to a feature branch or a per-session worktree first."
  exit 1
fi

echo "==> pr-readiness on $BRANCH (vs $TRUNK_REF)"

# Refresh trunk ref so behind/ahead counts are honest.
if ! git fetch --quiet "$REMOTE" "$TRUNK" 2>/dev/null; then
  yel "  warn: could not 'git fetch $REMOTE $TRUNK' — counts may be stale."
fi

# Check 1 — branch is descendant of recent origin/main.
HEAD_SHA=$(git rev-parse HEAD)
TRUNK_SHA=$(git rev-parse "$TRUNK_REF")
MB=$(git merge-base "$HEAD_SHA" "$TRUNK_SHA")
BEHIND=$(git rev-list --count "${MB}..${TRUNK_SHA}")
AHEAD=$(git rev-list --count "${MB}..${HEAD_SHA}")

if [[ "$BEHIND" -gt "$MAX_BEHIND" ]]; then
  fail "branch base is $BEHIND commits behind $TRUNK_REF (max $MAX_BEHIND). Rebase before pushing."
else
  pass "branch base is $BEHIND commits behind $TRUNK_REF (≤ $MAX_BEHIND)."
fi

# Check 1b — the branch still merges into the trunk.
#
# GitHub builds a merge commit to run a `pull_request` workflow. A conflict does
# not make CI fail: it stops CI from starting, so the PR page goes quiet rather
# than red and `gh pr checks` shows only the `pull_request_target` workflows.
# That cost a night's CI on #2341, which is why this check exists.
if MERGE_OUT=$(git merge-tree --write-tree "$HEAD_SHA" "$TRUNK_SHA" 2>&1); then
  pass "branch merges into $TRUNK_REF cleanly."
else
  MERGE_RC=$?
  CONFLICTED=$(printf '%s\n' "$MERGE_OUT" | sed -n 's/^CONFLICT ([^)]*): Merge conflict in //p' | paste -sd' ' -)
  if [[ -n "$CONFLICTED" ]]; then
    fail "branch conflicts with $TRUNK_REF in: $CONFLICTED. Merge or rebase first, or no pull_request workflow can run."
  elif [[ "$MERGE_RC" -gt 1 ]]; then
    yel "  warn: could not test the merge (git merge-tree exited $MERGE_RC; needs git 2.38 or newer)."
  else
    fail "branch does not merge into $TRUNK_REF cleanly. Merge or rebase first, or no pull_request workflow can run."
  fi
fi

if [[ "$AHEAD" -eq 0 ]]; then
  fail "branch carries 0 commits ahead of merge-base. Nothing to ship."
else
  pass "branch carries $AHEAD commits ahead of merge-base."
fi

# Check 2 — orphan-resurrection: any ahead-commit SHA seen as a recently-
# merged-and-deleted branch HEAD on origin? Requires gh CLI; skip with warn
# if absent.
if command -v gh >/dev/null 2>&1; then
  ORPHAN_HITS=0
  AHEAD_SHAS=$(git rev-list "${MB}..${HEAD_SHA}")
  # Pull last 30 merged PRs' headRefOid + headRefName + state
  MERGED_HEADS=$(gh pr list --state merged --limit 30 --json headRefOid,headRefName 2>/dev/null || echo '[]')
  if [[ "$MERGED_HEADS" != "[]" ]] && [[ -n "$MERGED_HEADS" ]]; then
    for sha in $AHEAD_SHAS; do
      if echo "$MERGED_HEADS" | grep -q "$sha"; then
        match_branch=$(echo "$MERGED_HEADS" | python3 -c "import json,sys; d=json.load(sys.stdin); [print(p['headRefName']) for p in d if p['headRefOid']==sys.argv[1]]" "$sha" 2>/dev/null || echo '<unknown>')
        fail "commit $sha matches HEAD of merged-and-deleted branch '$match_branch'. Orphan-resurrection — re-author on a fresh branch off $TRUNK_REF."
        ORPHAN_HITS=$((ORPHAN_HITS + 1))
      fi
    done
  fi
  [[ "$ORPHAN_HITS" -eq 0 ]] && pass "no ahead-commit matches a recently-merged-and-deleted branch (orphan-resurrection check)."
else
  yel "  warn: gh not on PATH; orphan-resurrection check skipped."
fi

# Check 3 — diff vs trunk exceeds sanity threshold.
DIFF_STAT=$(git diff --shortstat "${TRUNK_SHA}...${HEAD_SHA}" 2>/dev/null)
DIFF_FILES=$(echo "$DIFF_STAT" | grep -oE '[0-9]+ files? changed' | grep -oE '^[0-9]+' || echo 0)
DIFF_INS=$(echo "$DIFF_STAT" | grep -oE '[0-9]+ insertions?' | grep -oE '^[0-9]+' || echo 0)
DIFF_DEL=$(echo "$DIFF_STAT" | grep -oE '[0-9]+ deletions?' | grep -oE '^[0-9]+' || echo 0)
DIFF_LINES=$((DIFF_INS + DIFF_DEL))

if [[ "$DIFF_FILES" -gt "$MAX_FILES" ]]; then
  fail "diff touches $DIFF_FILES files (max $MAX_FILES). Set PR_READINESS_MAX_FILES=N to override for a bulk change."
else
  pass "diff touches $DIFF_FILES files (≤ $MAX_FILES)."
fi

if [[ "$DIFF_LINES" -gt "$MAX_LINES" ]]; then
  fail "diff totals $DIFF_LINES lines (+$DIFF_INS/-$DIFF_DEL; max $MAX_LINES). Set PR_READINESS_MAX_LINES=N to override for a bulk change."
else
  pass "diff totals $DIFF_LINES lines (+$DIFF_INS/-$DIFF_DEL; ≤ $MAX_LINES)."
fi

# Check 4 — working tree clean.
DIRTY=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')
if [[ "$DIRTY" -gt 0 ]]; then
  fail "working tree dirty ($DIRTY uncommitted/untracked entries). Commit or stash before push."
else
  pass "working tree clean."
fi

# Check 5 — DCO: every ahead-commit's Signed-off-by trailer is present AND
# matches that commit's own author (mirrors pr-hygiene.yml's "Developer
# Certificate of Origin" step). A trailer can be present yet wrong — pasted
# from a different commit, left over from an --amend, or just mistyped — and
# CI rejects on the mismatch exactly as it would on an absent trailer; a
# presence-only check here waved commits through that pr-hygiene.yml then
# failed post-push. Scoped to non-merge commits, same as CI: a merge commit
# introduces no authored content and cannot carry a meaningful sign-off.
#
# This is the local pre-push context, so the two server-side exemptions
# pr-hygiene.yml also carries do not apply here: a Dependabot-authored commit
# on a Dependabot-opened PR (no local branch is ever that), and GitHub's
# squash-merge noreply-alias rewrite (only exists after a squash merge on the
# server, never on an unpushed local branch).
MISSING_DCO=0
MISMATCHED_DCO=0
for sha in $(git rev-list --no-merges "${MB}..${HEAD_SHA}"); do
  author_name=$(git log -1 --format='%an' "$sha")
  author_email=$(git log -1 --format='%ae' "$sha")
  body=$(git log -1 --format='%B' "$sha")
  if ! printf '%s' "$body" | grep -qiE "^Signed-off-by:[[:space:]]+.+<.+@.+>"; then
    fail "commit $sha missing Signed-off-by trailer (DCO). git commit --amend -s --no-edit (or rebase -i + amend)."
    MISSING_DCO=$((MISSING_DCO + 1))
    continue
  fi
  expected="Signed-off-by: ${author_name} <${author_email}>"
  if ! printf '%s' "$body" | grep -qiF "$expected"; then
    fail "commit $sha: Signed-off-by trailer does not match the commit author (${author_name} <${author_email}>). git commit --amend -s --no-edit to re-sign as the correct author."
    MISMATCHED_DCO=$((MISMATCHED_DCO + 1))
  fi
done
[[ "$MISSING_DCO" -eq 0 && "$MISMATCHED_DCO" -eq 0 ]] && pass "every ahead-commit has a Signed-off-by trailer matching its author (DCO)."

# Check 6 — every ahead-commit is actually signed (F: ssh signing fails silently).
# `git commit -S` with a misconfigured ssh signer can produce an UNSIGNED commit
# and exit 0. %G? is the only place that shows: N = no signature at all.
UNSIGNED=0
for sha in $(git rev-list "${MB}..${HEAD_SHA}"); do
  sig=$(git log -1 --format='%G?' "$sha")
  if [[ "$sig" == "N" ]]; then
    fail "commit $sha carries NO signature (%G? = N). git rebase --exec 'git commit --amend --no-edit -S' ${TRUNK_REF}..HEAD"
    UNSIGNED=$((UNSIGNED + 1))
  fi
done
[[ "$UNSIGNED" -eq 0 ]] && pass "every ahead-commit carries a signature (%G? != N)."

# Check 7 — if a PR already exists for this branch, its checks must have RUN.
#
# F-348: GitHub Actions was disabled repository-wide for ~40 minutes on
# . Every merge in that window took ZERO workflow runs — not failed
# runs, NO runs — while the PRs showed green and the merges succeeded. An EMPTY
# statusCheckRollup is not a pass; it is the absence of evidence. Assert it is
# non-empty, and that nothing in it has already failed. An unpushed repair
# may follow a failed ancestor; those checks do not describe the repair.
# Verify ancestry here, then require new-head CI after publication.
#
# A RUNNING check reports conclusion="" (an empty string, not null), and a
# `timeout-minutes` kill reports CANCELLED rather than FAILURE — so completion
# is judged on .status, and failure on "not one of the passing conclusions".
# Check 10 reads PR_DATA too; it stays empty when gh is missing or the read fails.
PR_DATA=""
if command -v gh >/dev/null 2>&1; then
  if PR_DATA=$(gh pr list --state open --head "$BRANCH" --json headRefOid,statusCheckRollup,title); then
    if CHECK_RESULT=$(printf '%s' "$PR_DATA" | python3 "$SCRIPT_DIR/../ci/check_pr_head_checks.py" "$HEAD_SHA" 2>&1); then
      pass "$CHECK_RESULT"
    else
      fail "$CHECK_RESULT"
    fi
  else
    fail "could not read open PR state; readiness is unverified"
  fi
else
  fail "gh not on PATH; PR check evidence is unavailable"
fi

# Check 8 — every commit on a track/* branch carries its [track:] trailer
# (ADR 0006; mirrors pr-hygiene.yml's "track discipline" step). Only a
# track/* branch is held to this; a hand-rolled fix/* or chore/* branch is
# not. Merge commits are exempt, same as CI.
if [[ "$BRANCH" =~ ^track/ ]]; then
  WANT_TRAILER="[track: ${BRANCH#track/}]"
  TRACK_MISSING=0
  for sha in $(git rev-list "${MB}..${HEAD_SHA}"); do
    parents=$(git log -1 --format='%P' "$sha")
    if [[ "$parents" == *" "* ]]; then
      continue # merge commit — exempt, same as pr-hygiene.yml
    fi
    body=$(git log -1 --format='%B' "$sha")
    if ! printf '%s' "$body" | grep -qE '\[track:[[:space:]]*[a-zA-Z0-9._-]+\]'; then
      fail "commit $sha lacks the ${WANT_TRAILER} trailer (track/* branch, ADR 0006). Add it as the last line: git commit --amend, or rebase -i and reword."
      TRACK_MISSING=$((TRACK_MISSING + 1))
    fi
  done
  [[ "$TRACK_MISSING" -eq 0 ]] && pass "every ahead-commit on this track/* branch carries a [track:] trailer."
else
  pass "branch '$BRANCH' is not track/*; [track:] trailer check does not apply."
fi

# Check 9 — no agent-tooling / AI-attribution reference in any ahead-commit
# message (mirrors agent-references.yml's "Check the commit messages too"
# step). A commit message is permanent once it reaches a squash-merge on
# main; this is the only place a contributor can still fix it for free.
AI_REF_HITS=$(git log --format='%B' "${MB}..${HEAD_SHA}" | grep -nEi 'claude\.ai|Claude-Session:|anthropic\.com' || true)
if [[ -n "$AI_REF_HITS" ]]; then
  fail "ahead-commit message(s) carry an agent-tooling / AI-attribution reference (claude.ai, Claude-Session:, or anthropic.com). Amend or rebase before pushing — history cannot be scrubbed after merge."
else
  pass "no agent-tooling / AI-attribution references in ahead-commit messages."
fi

# Check 10 — if an open PR already exists for this branch, its title must
# follow Conventional Commits with a lowercase scope (mirrors pr-hygiene.yml's
# "Conventional Commits PR title" step). main is squash-merged, so the PR
# title — not any individual commit subject — becomes the permanent commit
# message (CONTRIBUTING.md "Conventional Commits for PR titles"). No PR yet
# is not a failure: there is no title to check until `gh pr create` runs,
# same treatment as Check 7's "No open PR" case. If gh is missing or the PR
# list read failed, Check 7 already reported that; this adds nothing more.
if command -v gh >/dev/null 2>&1 && [[ -n "$PR_DATA" ]]; then
  PR_TITLE=$(printf '%s' "$PR_DATA" | python3 -c "
import json, sys
d = json.load(sys.stdin)
print(d[0]['title'] if d else '')
" 2>/dev/null || echo '')
  if [[ -n "$PR_TITLE" ]]; then
    TITLE_PATTERN='^(feat|fix|chore|docs|refactor|test|perf|ci|build|revert|adr)(\([a-z0-9._/-]+\))?!?: [^[:space:]].{0,98}$'
    if [[ "$PR_TITLE" =~ $TITLE_PATTERN ]]; then
      pass "open PR title follows Conventional Commits: $PR_TITLE"
    else
      fail "open PR title '$PR_TITLE' does not follow Conventional Commits (expected '<type>(<scope>): <subject>' or '<type>: <subject>', lowercase scope only). CONTRIBUTING.md 'Conventional Commits for PR titles'."
    fi
  else
    pass "no open PR yet; Conventional Commits title check runs once one exists."
  fi
fi


echo
if [[ "$FAIL" -eq 0 ]]; then
  green "==> READY: $BRANCH cleared all gates. Safe to 'git push' / 'gh pr create'."
  exit 0
else
  red "==> NOT READY: $BRANCH has $FAIL gate failure(s). Fix before pushing."
  exit 1
fi
