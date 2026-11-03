#!/usr/bin/env bash
# parity-rollback.sh — the parity gate for `goatbuilder rollback` against
# ops/scripts/rollback.sh (goatbuilder-conformance.md §The parity gate).
#
# The two read-only modes are compared line for line for every component that
# carries a deploy-history.yml: `--list` (the on-host ledger where a host is
# reachable, else the in-repo history) and `--status` (the running ref per host,
# `<unreachable>` where it is not). Where no host is reachable — CI — both sides
# take the in-repo path, which is the fallback a real rollback would take there
# too. Where a host is reachable — the deployment e2e harness's container hosts
# (ops/e2e/hermetic.sh) or an operator's laptop — the host path is compared, and
# the summary counts how many invocations took it. The script reads the in-repo
# ledger through jq whichever yq is installed (mikefarah's or the Python one);
# CI runs this gate under both. The flip itself is the script's own remote
# shell, carried verbatim; the unit tests pin its three shapes.
#
# `--site <s>` narrows a component to the hosts of one goatnet. For every
# component whose hosts span more than one site, `--status --site` is compared
# for its first site, and an unknown site must be refused in the same words.
#
# usage: parity-rollback.sh <goatbuilder binary> [component ...]
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; REPO="$(cd "$HERE/../.." && pwd)"
BIN="${1:?usage: parity-rollback.sh <goatbuilder binary> [component ...]}"; shift || true
COMPONENTS=("$@")
if [ ${#COMPONENTS[@]} -eq 0 ]; then
  for f in "$REPO"/ops/ansible/roles/*/deploy-history.yml; do COMPONENTS+=("$(basename "$(dirname "$f")")"); done
fi
command -v yq >/dev/null || { echo "parity-rollback: yq required by the script side (brew install yq / apt-get install yq)"; exit 2; }

script_side() { "$REPO/ops/scripts/rollback.sh" "$@" 2>&1 | sed -E 's/^rollback\.sh: //' | sed -E 's/[[:space:]]+$//'; }
binary_side() { "$BIN" -C "$REPO" rollback "$@" 2>&1 | sed -E 's/^goatbuilder: //; s/^(ok  |FAIL)  rollback  //; /^rollback: (ok|refused|not converged)/d' | sed -E 's/[[:space:]]+$//'; }

# The distinct sites of a component's hosts, whichever yq is installed.
sites_of() {
  if yq --version 2>&1 | grep -qi mikefarah; then yq -o=json '.' "$1"; else yq '.' "$1"; fi \
    | jq -r '[.hosts[]?.site // empty] | unique | .[]'
}

fail=0; n=0; host=0
compare() {
  n=$((n+1))
  a="$(script_side "$@" || true)"; b="$(binary_side "$@" || true)"
  case "$a" in *"(on-host ledger @"*|*"(from host-ledger("*) host=$((host+1)) ;; esac
  if [ "$a" != "$b" ]; then
    fail=$((fail+1)); echo "PARITY FAIL: rollback $*"
    { diff <(printf '%s\n' "$a") <(printf '%s\n' "$b") || true; } | sed 's/^/    /' | head -12
  fi
}
for c in "${COMPONENTS[@]}"; do
  for mode in --list --status; do compare "$c" "$mode"; done
  sites="$(sites_of "$REPO/ops/ansible/roles/$c/deploy-history.yml" 2>/dev/null || true)"
  if [ "$(printf '%s\n' "$sites" | grep -c .)" -gt 1 ]; then
    compare "$c" --status --site "$(printf '%s\n' "$sites" | head -1)"
    compare "$c" --list --site no-such-site
  fi
done
[ "${n:-0}" -gt 0 ] || { echo "parity FAIL: nothing was compared (no inputs read — is PyYAML importable by python3?)"; exit 1; }
if [ "$fail" = 0 ]; then echo "parity ok: $n rollback invocations identical, $host on the host path (script vs goatbuilder rollback --list/--status over ${#COMPONENTS[@]} components)"; else echo "parity FAIL: $fail of $n differ"; exit 1; fi
