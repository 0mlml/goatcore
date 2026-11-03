#!/usr/bin/env bash
# pr-readiness_test.sh — behavioral test harness for ops/scripts/pr-readiness.sh.
#
# Constructs throwaway git repos in $TMPDIR, drives the script through each of
# its five failure modes + the green path, and asserts on exit code + key
# output strings. GitHub responses use an offline fixture with no open PR.
#
# Run:  ./ops/scripts/pr-readiness_test.sh
# Exit: 0 if every case passes; 1 on first failure.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$SCRIPT_DIR/pr-readiness.sh"
[ -x "$SCRIPT" ] || { echo "ERROR: $SCRIPT not executable" >&2; exit 1; }

PASS_COUNT=0
FAIL_COUNT=0

red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }

assert_pass() {
  local name="$1" want_code="$2" got_code="$3" want_substr="$4" got_output="$5"
  if [ "$got_code" -ne "$want_code" ]; then
    red "FAIL: $name — exit code $got_code, wanted $want_code"
    red "  output: $got_output"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  if [ -n "$want_substr" ] && ! echo "$got_output" | grep -qF -- "$want_substr"; then
    red "FAIL: $name — output missing substring: $want_substr"
    red "  output: $got_output"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  green "PASS: $name"
  PASS_COUNT=$((PASS_COUNT + 1))
}

# Build a "fake origin" + a clone with `origin/main` set up so pr-readiness's
# `git rev-list HEAD...origin/main` works. Caller cd's into the clone.
make_fixture() {
  local fixture_dir="$1"
  local origin="$fixture_dir/origin.git"
  local clone="$fixture_dir/clone"

  # Bare origin
  git init --bare -b main "$origin" >/dev/null

  # Seed clone (one commit on main, pushed to origin)
  git clone -q "$origin" "$clone"
  ( cd "$clone" || exit 1
    git config user.email "test@example.com"
    git config user.name "Test"
    mkdir -p .git/test-bin
    cat > .git/test-bin/gh <<'GH'
#!/usr/bin/env bash
if [ "$1 $2" = "pr list" ]; then printf '[]\n'; else exit 1; fi
GH
    chmod +x .git/test-bin/gh
    echo seed > README.md
    git add README.md
    git commit -sq -m "seed"
    git push -q origin main
  ) || return 1
  echo "$clone"
}

# Helper: append N tracked commits to a feature branch and check out that branch.
checkout_feature_with_commits() {
  local clone="$1" branch="$2" count="$3"
  ( cd "$clone" || exit 1
    git checkout -q -b "$branch" main
    for i in $(seq 1 "$count"); do
      echo "feat-$i" >> "feat-$i.txt"
      git add "feat-$i.txt"
      # Real track/* commits always carry the [track:] trailer (ADR 0006,
      # normally appended by `work commit`); match that here so this fixture
      # exercises the OTHER checks, not an incidental track-trailer failure.
      if [[ "$branch" == track/* ]]; then
        git commit -sq -m "feat: change $i" -m "[track: ${branch#track/}]"
      else
        git commit -sq -m "feat: change $i"
      fi
    done
  ) || return 1
}

# ── CASE 1 — green path: 1 ahead, clean, signed-off ──────────────────────────
case_green_path() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")" || { red "fixture failed"; return; }
  checkout_feature_with_commits "$clone" "track/green" 1
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "green path exits 0" 0 "$code" "READY" "$out"
  rm -rf "$tmp"
}

# ── CASE 2 — refuse on main itself ───────────────────────────────────────────
case_refuse_on_main() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "refuses on main" 1 "$code" "refusing to run pr-readiness on main itself" "$out"
  rm -rf "$tmp"
}

# ── CASE 3 — zero ahead (branch == main, nothing to ship) ────────────────────
case_zero_ahead() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  ( cd "$clone" && git checkout -qb track/empty main )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "zero-ahead fails" 1 "$code" "0 commits ahead" "$out"
  rm -rf "$tmp"
}

# ── CASE 4 — dirty working tree ──────────────────────────────────────────────
case_dirty_tree() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  checkout_feature_with_commits "$clone" "track/dirty" 1
  ( cd "$clone" && echo dirty > scratch.txt )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "dirty tree fails" 1 "$code" "working tree dirty" "$out"
  rm -rf "$tmp"
}

# ── CASE 5 — missing DCO sign-off ────────────────────────────────────────────
case_missing_dco() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  ( cd "$clone" || exit 1
    git checkout -qb track/no-dco main
    echo nodco > nodco.txt
    git add nodco.txt
    # NB: no -s flag — deliberate
    git commit -q -m "feat: missing dco"
  )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "missing DCO fails" 1 "$code" "missing Signed-off-by" "$out"
  rm -rf "$tmp"
}

# ── CASE 6 — runaway diff (>50 files) ────────────────────────────────────────
case_runaway_files() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  ( cd "$clone" || exit 1
    git checkout -qb track/wide main
    for i in $(seq 1 60); do
      echo "$i" > "wide-$i.txt"
    done
    git add .
    git commit -sq -m "feat: wide change" -m "[track: wide]"
  )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "runaway-files fails" 1 "$code" "diff touches" "$out"
  # Override threshold via env var → green path.
  out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" PR_READINESS_MAX_FILES=200 "$SCRIPT" 2>&1 )
  code=$?
  assert_pass "runaway-files override via env" 0 "$code" "READY" "$out"
  rm -rf "$tmp"
}

# ── CASE 7 — runaway lines (>5000 lines) ─────────────────────────────────────
case_runaway_lines() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  ( cd "$clone" || exit 1
    git checkout -qb track/big main
    # Write a single 6000-line file.
    seq 1 6000 > big.txt
    git add big.txt
    git commit -sq -m "feat: big file"
  )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "runaway-lines fails" 1 "$code" "diff totals" "$out"
  rm -rf "$tmp"
}

# ── CASE 8 — stale base (>50 commits behind) ─────────────────────────────────
case_stale_base() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  ( cd "$clone" || exit 1
    # Branch off NOW (clean), then advance main 60 commits and push.
    git checkout -qb track/stale main
    echo branch > branch.txt
    git add branch.txt
    git commit -sq -m "feat: branch commit"

    git checkout -q main
    for i in $(seq 1 60); do
      echo "main-$i" >> main-churn.txt
      git add main-churn.txt
      git commit -sq -m "main churn $i"
    done
    git push -q origin main

    git checkout -q track/stale
  )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "stale base fails (>50 behind)" 1 "$code" "behind origin/main" "$out"
  rm -rf "$tmp"
}

# A branch that no longer merges into main is refused, and the file is named.
#
# This is the one failure that does not show up as a red check: GitHub builds a
# merge commit to run a `pull_request` workflow, so a conflict stops CI from
# starting at all and the PR page goes quiet rather than red.
case_conflicts_with_trunk() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  ( cd "$clone" || exit 1
    # Both sides change the same line of the same file.
    git checkout -qb track/conflicted main
    echo "ours" > shared.txt
    git add shared.txt
    git commit -sq -m "feat: our line"

    git checkout -q main
    echo "theirs" > shared.txt
    git add shared.txt
    git commit -sq -m "main: their line"
    git push -q origin main

    git checkout -q track/conflicted
  )
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "a branch that conflicts with the trunk fails, naming the file" 1 "$code" "conflicts with origin/main in: shared.txt" "$out"
  rm -rf "$tmp"
}

# GitHub read failures must not be mistaken for an absent PR.
case_github_unavailable() {
  local tmp; tmp="$(mktemp -d)"
  local clone; clone="$(make_fixture "$tmp")"
  checkout_feature_with_commits "$clone" "track/api-error" 1
  printf '#!/usr/bin/env bash\nexit 1\n' > "$clone/.git/test-bin/gh"
  local out; out=$( cd "$clone" && PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1 )
  local code=$?
  assert_pass "GitHub API failure refuses publication" 1 "$code" "could not read open PR state" "$out"
  out=$( cd "$clone" || exit 1
    # Exported for the readiness subprocess to model a missing gh command.
    # shellcheck disable=SC2329
    command() {
      if [ "$*" = "-v gh" ]; then return 1; fi
      builtin command "$@"
    }
    export -f command
    PATH="$clone/.git/test-bin:/usr/bin:/bin" "$SCRIPT" 2>&1
  )
  code=$?
  assert_pass "missing gh refuses publication" 1 "$code" "gh not on PATH" "$out"
  rm -rf "$tmp"
}

# Run every case
case_green_path
case_refuse_on_main
case_zero_ahead
case_dirty_tree
case_missing_dco
case_runaway_files
case_runaway_lines
case_stale_base
case_conflicts_with_trunk
case_github_unavailable

echo
if [ "$FAIL_COUNT" -eq 0 ]; then
  green "==> $PASS_COUNT passed, 0 failed"
  exit 0
else
  red "==> $PASS_COUNT passed, $FAIL_COUNT failed"
  exit 1
fi
