#!/usr/bin/env bats

# Tests for ops/ci/check-bandwidth-categories.sh.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
SUBJECT="$REPO_ROOT/ops/ci/check-bandwidth-categories.sh"

setup() {
  if [ ! -f "$SUBJECT" ]; then
    echo "subject script not found at $SUBJECT" >&2
    return 1
  fi

  for tool in git python3 python3-yaml; do
    case "$tool" in
      git)
        if ! command -v git >/dev/null 2>&1; then
          if [ -n "${CI:-}" ]; then
            echo "missing required tool: git (required in CI)" >&2
            return 1
          else
            skip "git not available"
          fi
        fi
        ;;
      python3)
        if ! command -v python3 >/dev/null 2>&1; then
          if [ -n "${CI:-}" ]; then
            echo "missing required tool: python3 (required in CI)" >&2
            return 1
          else
            skip "python3 not available"
          fi
        fi
        ;;
      python3-yaml)
        if ! python3 -c 'import yaml' >/dev/null 2>&1; then
          if [ -n "${CI:-}" ]; then
            echo "missing required tool: python3 yaml module (required in CI)" >&2
            return 1
          else
            skip "python3 yaml module not available"
          fi
        fi
        ;;
    esac
  done
}

init_sandbox_repo() {
  sandbox="$BATS_TEST_TMPDIR/check-bandwidth-categories-$BATS_TEST_NUMBER"
  mkdir -p "$sandbox"

  (
    cd "$sandbox" &&
      git init --quiet &&
      git config user.email "test@example.com" &&
      git config user.name "Test User"
  )
}

@test "empty grammar arrays pass under /bin/bash -u" {
  init_sandbox_repo

  mkdir -p "$sandbox/ops/observability"
  cat >"$sandbox/ops/observability/bandwidth-categories.yaml" <<'YAML'
workload: []
transport: []
YAML

  cp "$SUBJECT" "$sandbox/check-bandwidth-categories.sh"

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-bandwidth-categories.sh

  [ "$status" -eq 0 ]
  [[ "$output" == *"grammar: 0 workload keys, 0 transport keys"* ]]
  [[ "$output" != *"unbound variable"* ]]
  [[ "$output" == *"bandwidth category check OK"* ]]
}

@test "grammar and producers preserve keys with spaces" {
  init_sandbox_repo

  mkdir -p "$sandbox/ops/observability"
  cat >"$sandbox/ops/observability/bandwidth-categories.yaml" <<'YAML'
workload:
  - key: "foo bar"
    why: "test workload key with space"
  - key: "baz qux"
    why: "second workload key with space"
transport: []
YAML

  mkdir -p "$sandbox/ops/ansible/roles/goat-logship/defaults"
  cat >"$sandbox/ops/ansible/roles/goat-logship/defaults/main.yml" <<'YML'
logship_http_category_rules:
  - category: "foo bar"
  - category: "baz qux"
logship_http_category_fallback: "foo bar"
YML

  cp "$SUBJECT" "$sandbox/check-bandwidth-categories.sh"

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-bandwidth-categories.sh

  [ "$status" -eq 0 ]
  [[ "$output" == *"bandwidth category check OK"* ]]
}
