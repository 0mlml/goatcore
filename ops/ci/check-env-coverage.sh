#!/usr/bin/env bash
# ops/ci/check-env-coverage.sh — the meta-guard: "we added an environment" and
# "every guard covers it" must be the same event.
#
# WHY: the cross-account fuse existed on four terraform trees and a
# fifth was added without it, so an apply silently followed the ambient
# aws-vault profile into the wrong AWS account. Nothing failed — the fuse was a
# convention someone had to remember. Same shape one layer up: guards that
# iterate environments from a hand-typed list (the old two-entry SITE_LITERALS)
# leave every new environment uncovered by default.
#
# This check asserts two coverage properties, both DERIVED from the tree
# (docs/design/environment-parity.md §4, principle 3 — "guards derive their
# coverage; they never enumerate it"):
#
#   (a) Cross-account fuse: every `provider "aws"` block in every terraform tree
#       — discovered by scanning tracked *.tf, not by listing trees — sets
#       `allowed_account_ids`. A new tree is covered the moment it is committed.
#
#   (b) Guard reach: every guard that claims full-tree environment coverage
#       declares its discovery glob in an `# env-discovery: <glob>` marker, and
#       that glob actually expands to every `group_vars/<env>/` dir. A guard
#       that hand-enumerates envs, or whose glob drifts, fails here. Each such
#       guard must also be wired into a workflow — a guard documented as
#       CI-enforced but wired nowhere covers nothing.
#
# Usage: ops/ci/check-env-coverage.sh          (full tree; no arguments)
# Exit:  0 covered · 1 coverage gap (each gap printed with its fix) · 2 dep error
set -euo pipefail

GV_ROOT="${GV_ROOT:-ops/ansible/group_vars}"
WORKFLOW_DIR="${WORKFLOW_DIR:-.github/workflows}"

# Guards that claim full-tree environment coverage. Each MUST carry an
# `# env-discovery: <glob>` marker naming how it finds environments.
COVERAGE_GUARDS=(
  'ops/ci/check-portal-cert-identity-override.sh'
  'ops/ci/check-site-config-bleed.sh'
)

# Terraform trees exempt from the allowed_account_ids fuse. EMPTY today — every
# tree complies, including the inert GovCloud tree (ADR 1078), which satisfies
# the fuse with `[var.govcloud_account_id]`, a variable with no default: it
# refuses to apply until a real gov account is supplied. That is the pattern to
# copy for a tree that does not yet know its account; exemption is not.
# Format: one repo-relative directory per entry, each with a reason on the line.
EXEMPT_TF_TREES=()

repo_root() { git rev-parse --show-toplevel 2>/dev/null || pwd; }
cd "$(repo_root)"

fail=0

# ---------------------------------------------------------------------------
# (a) every `provider "aws"` block carries the cross-account fuse
# ---------------------------------------------------------------------------
is_exempt_tree() {
  local e
  for e in ${EXEMPT_TF_TREES+"${EXEMPT_TF_TREES[@]}"}; do
    [[ "$1" == "$e" || "$1" == "$e"/* ]] && return 0
  done
  return 1
}

TF_FILES=()
while IFS= read -r f; do
  TF_FILES+=("$f")
done < <(git ls-files '*.tf')
tf_blocks=0
for f in ${TF_FILES+"${TF_FILES[@]}"}; do
  [[ -f "$f" ]] || continue
  if is_exempt_tree "$(dirname "$f")"; then continue; fi

  # Walk the file tracking brace depth so nested blocks (`default_tags { … }`)
  # do not end the provider block early, and so `allowed_account_ids` is only
  # credited when it sits inside the provider block itself.
  depth=0; in_provider=0; start_line=0; has_fuse=0; lineno=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    lineno=$((lineno+1))
    code="${line%%#*}"                       # strip trailing comment
    if [[ $in_provider -eq 0 && "$code" =~ ^[[:space:]]*provider[[:space:]]+\"aws\"[[:space:]]*\{ ]]; then
      in_provider=1; depth=0; has_fuse=0; start_line=$lineno
    fi
    if [[ $in_provider -eq 1 ]]; then
      [[ "$code" == *allowed_account_ids* ]] && has_fuse=1
      opens="${code//[^\{]/}"; closes="${code//[^\}]/}"
      depth=$(( depth + ${#opens} - ${#closes} ))
      if [[ $depth -le 0 ]]; then
        in_provider=0
        tf_blocks=$((tf_blocks+1))
        if [[ $has_fuse -eq 0 ]]; then
          echo "FAIL: $f:$start_line — provider \"aws\" block has no allowed_account_ids."
          echo "      An apply follows whatever account the ambient credentials point at."
          echo "      Add inside the block:  allowed_account_ids = [\"<account-id>\"]"
          echo "      (or [var.<x>_account_id] with no default, to refuse until supplied)."
          fail=1
        fi
      fi
    fi
  done < "$f"
done

if [[ $tf_blocks -eq 0 ]]; then
  echo "FAIL: found zero provider \"aws\" blocks — discovery is broken, so this" >&2
  echo "      check would pass vacuously." >&2
  exit 2
fi

# ---------------------------------------------------------------------------
# (b) every group_vars/<env>/ dir is reachable by every full-coverage guard
# ---------------------------------------------------------------------------
ENV_DIRS=()
while IFS= read -r envdir; do
  ENV_DIRS+=("$envdir")
done < <(
  for d in "$GV_ROOT"/*/; do
    [[ -d "$d" ]] || continue
    printf '%s\n' "${d%/}"
  done | sort
)

if [[ ${#ENV_DIRS[@]} -eq 0 ]]; then
  echo "FAIL: found zero environment dirs under $GV_ROOT — discovery is broken." >&2
  exit 2
fi

for guard in "${COVERAGE_GUARDS[@]}"; do
  if [[ ! -f "$guard" ]]; then
    echo "FAIL: coverage guard '$guard' does not exist (renamed or deleted?)."
    echo "      Update COVERAGE_GUARDS in $0, or restore the guard."
    fail=1
    continue
  fi

  marker="$(command grep -m1 -E '^#[[:space:]]*env-discovery:' "$guard" 2>/dev/null \
             | sed -E 's/^#[[:space:]]*env-discovery:[[:space:]]*//; s/[[:space:]]*$//' || true)"
  if [[ -z "$marker" ]]; then
    echo "FAIL: '$guard' claims full-tree environment coverage but declares no"
    echo "      discovery glob. Add a line near its env loop:"
    echo "        # env-discovery: $GV_ROOT/*/"
    fail=1
    continue
  fi

  # Expand the declared glob and compare against the real environment set.
  REACHED=()
  while IFS= read -r reached; do
    REACHED+=("$reached")
  done < <(
    shopt -s nullglob
    # shellcheck disable=SC2086  # the marker is a glob and must stay unquoted
    for d in $marker; do [[ -d "$d" ]] && printf '%s\n' "${d%/}"; done | sort -u
  )

  gaps=()
  for env in "${ENV_DIRS[@]}"; do
    hit=0
    for r in ${REACHED+"${REACHED[@]}"}; do [[ "$r" == "$env" ]] && { hit=1; break; }; done
    [[ $hit -eq 1 ]] || gaps+=("$env")
  done

  if [[ ${#gaps[@]} -gt 0 ]]; then
    echo "FAIL: '$guard' does not reach ${#gaps[@]} environment(s):"
    printf '        %s\n' "${gaps[@]}"
    echo "      Its declared discovery is '$marker'. A guard must find environments"
    echo "      by glob, never by a hand-typed list — otherwise the next env is"
    echo "      silently uncovered."
    fail=1
  fi

  # A guard nobody runs covers nothing.
  if ! command grep -rqF "$(basename "$guard")" "$WORKFLOW_DIR" 2>/dev/null; then
    echo "FAIL: '$guard' is referenced by no workflow under $WORKFLOW_DIR."
    echo "      Full-tree coverage that never executes is coverage on paper only."
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  echo ""
  echo "Environment coverage is a derived property, not a checklist:"
  echo "docs/design/environment-parity.md §4 (self-extending guards)."
  exit 1
fi

echo "OK: $tf_blocks provider \"aws\" block(s) fused; ${#ENV_DIRS[@]} environment dir(s) reached by ${#COVERAGE_GUARDS[@]} full-coverage guard(s)."
