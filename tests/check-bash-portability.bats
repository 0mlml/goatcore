#!/usr/bin/env bats
# Behavior tests for ops/ci/check-bash-portability.sh.
#
# The gate is a static scan, so these tests exercise it on synthetic
# fixture files passed as explicit paths. They pin:
#   - detection of each forbidden construct class (builtins, assoc
#     arrays / namerefs in combined-flag form, case conversion, |&),
#   - the comment rules: whole-line and trailing comments are ignored,
#     but a `#` inside ${#arr[@]} must NOT hide a token after it
#     (the truncation blind spot found in PR #205 review),
#   - exit codes: 0 clean, 1 violation, 2 explicitly-named missing file.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
GATE_SH="$REPO_ROOT/ops/ci/check-bash-portability.sh"

setup() {
  if [ ! -x "$GATE_SH" ]; then
    skip "portability gate not present at $GATE_SH"
  fi
}

# Helper: write a fixture script and echo its path.
fixture() {
  local path
  path="$BATS_TEST_TMPDIR/fixture-$$-$BATS_TEST_NUMBER.sh"
  cat > "$path"
  echo "$path"
}

@test "bash-portability: clean 3.2-safe file passes" {
  local f
  f="$(fixture <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
arr=()
while IFS= read -r x; do arr+=("$x"); done < <(ls)
declare -a plain=()
n=${#arr[@]}
echo "count=$n" 2>&1 | cat
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 0 ]
}

@test "bash-portability: mapfile is flagged" {
  local f
  f="$(fixture <<'EOF'
mapfile -t arr < <(ls)
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 1 ]
  [[ "$output" == *"$f:1:"* ]]
}

@test "bash-portability: declare -A and combined-flag forms are flagged" {
  # Includes non-terminal positions of the A/n flag (-Ax, -Ag, -nr): the
  # ERE is unanchored, so backtracking matches these too (empirically
  # re-confirmed against the codex review round's contrary claim).
  local f
  f="$(fixture <<'EOF'
declare -A map=()
typeset -A tmap=()
declare -rA frozen=()
declare -Ax exported=()
declare -Ag global=()
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 1 ]
  [[ "$output" == *"$f:1:"* ]]
  [[ "$output" == *"$f:2:"* ]]
  [[ "$output" == *"$f:3:"* ]]
  [[ "$output" == *"$f:4:"* ]]
  [[ "$output" == *"$f:5:"* ]]
}

@test "bash-portability: namerefs via local -n and declare -n are flagged" {
  local f
  f="$(fixture <<'EOF'
x() {
  local -n ref=$1
}
declare -n gref=THING
y() {
  local -nr ro_ref=$1
}
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 1 ]
  [[ "$output" == *"$f:2:"* ]]
  [[ "$output" == *"$f:4:"* ]]
  [[ "$output" == *"$f:6:"* ]]
}

@test "bash-portability: case-conversion expansions are flagged" {
  local f
  f="$(fixture <<'EOF'
echo "${v,,}"
echo "${NAME^^}"
echo "${arr[0],,}"
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 1 ]
  [[ "$output" == *"$f:1:"* ]]
  [[ "$output" == *"$f:2:"* ]]
  [[ "$output" == *"$f:3:"* ]]
}

@test "bash-portability: |& pipe shorthand is flagged" {
  local f
  f="$(fixture <<'EOF'
some_command |& tee log.txt
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 1 ]
}

@test "bash-portability: token after \${#arr[@]} on the same line is NOT hidden" {
  # Regression for the truncation blind spot: a first-#-wins comment
  # stripper cut the line at the # in ${#a[@]} and missed the declare -A.
  local f
  f="$(fixture <<'EOF'
n=${#a[@]}; declare -A m
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 1 ]
  [[ "$output" == *"$f:1:"* ]]
}

@test "bash-portability: whole-line and trailing comments are ignored" {
  local f
  f="$(fixture <<'EOF'
# mapfile is documented here but never used
echo real-code  # chosen over mapfile deliberately
    # indented comment about declare -A too
EOF
)"
  run bash "$GATE_SH" "$f"
  [ "$status" -eq 0 ]
}

@test "bash-portability: explicitly named missing file exits 2" {
  run bash "$GATE_SH" "$BATS_TEST_TMPDIR/does-not-exist.sh"
  [ "$status" -eq 2 ]
}

@test "bash-portability: declared linux-host-only script is skipped, not scanned" {
  # ops/scripts/vxlan-rca.sh runs only on goat hosts (bash 5.x) and keeps
  # its bash-4 idioms; the gate must skip it visibly instead of flagging it.
  run bash "$GATE_SH" ops/scripts/vxlan-rca.sh
  [ "$status" -eq 0 ]
  [[ "$output" == *"skip (linux-host-only"* ]]
}

@test "bash-portability: discovers newly tracked extensionless bash scripts" {
  local repo="$BATS_TEST_TMPDIR/discovery"
  mkdir -p "$repo/ops/ci" "$repo/ops/scripts"
  cp "$GATE_SH" "$repo/ops/ci/check-bash-portability.sh"
  printf '#!/usr/bin/env bash\nmapfile -t values\n' > "$repo/ops/scripts/new-entry"
  git -C "$repo" init -q
  git -C "$repo" add ops
  run "${BASH_UNDER_TEST:-/bin/bash}" "$repo/ops/ci/check-bash-portability.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"ops/scripts/new-entry:2:"* ]]
}

@test "bash-portability: a missing tracked script fails discovery" {
  local repo="$BATS_TEST_TMPDIR/missing"
  mkdir -p "$repo/ops/ci" "$repo/ops/scripts"
  cp "$GATE_SH" "$repo/ops/ci/check-bash-portability.sh"
  printf '#!/bin/bash\necho ok\n' > "$repo/ops/scripts/missing.sh"
  git -C "$repo" init -q
  git -C "$repo" add ops
  rm "$repo/ops/scripts/missing.sh"
  run "${BASH_UNDER_TEST:-/bin/bash}" "$repo/ops/ci/check-bash-portability.sh"
  [ "$status" -eq 2 ]
  [[ "$output" == *"missing.sh"* ]]
}
