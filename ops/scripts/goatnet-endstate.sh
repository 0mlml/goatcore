#!/usr/bin/env bash
# goatnet-endstate.sh — is a goatnet in its declared end state? (ADR 1206)
#
# The script avenue of `goatbuilder endstate` (ADR 1201): the same derivation and
# grades, from the same file (ops/goatbuilder/endstate/endstate.py). Reads only.
#
#   ops/scripts/goatnet-endstate.sh <site> [--no-assay] [--strict] [--facts FILE] [--assay-rows FILE] [--root DIR]
#
# --root reads the declarations from another tree (a fixture); the parity gate
# ops/goatbuilder/parity-endstate.sh runs both avenues that way.
#
# By default the acceptance assay runs first (the standup's phase-13 commands) and
# its rows decide each service's functional grade; --no-assay leaves that grade
# unknown. Exit 0 in the declared end state, 3 when anything is off (GB-068),
# 1 when it cannot run (GB-066).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
site="${1:-}"
[ -n "$site" ] || { echo "usage: goatnet-endstate.sh <site> [--no-assay] [--strict] [--facts FILE] [--assay-rows FILE]" >&2; exit 2; }
shift
run_assay=1; facts=""; rows=""; strict=""; root="$REPO"
while [ $# -gt 0 ]; do
  case "$1" in
    --no-assay) run_assay=0 ;;
    --strict) strict="--strict" ;;
    --facts) facts="$2"; shift ;;
    --assay-rows) rows="$2"; shift ;;
    --root) root="$(cd "$2" && pwd)"; shift ;;
    *) echo "goatnet-endstate.sh: unknown argument $1" >&2; exit 2 ;;
  esac
  shift
done

# The Python with PyYAML: python3 when it has it, else Ansible's own (#2120).
py="${GOATBUILDER_PYTHON:-python3}"
if ! "$py" -c 'import yaml' 2>/dev/null; then
  ap="$(head -1 "$(command -v ansible-playbook 2>/dev/null || echo /dev/null)" 2>/dev/null | sed 's/^#!//' | awk '{print $NF}' || true)"
  if [ -n "$ap" ] && "$ap" -c 'import yaml' 2>/dev/null; then py="$ap"; fi
fi

if [ ! -d "$root/ops/ansible/group_vars/${site//-/_}_hosts" ]; then
  echo "GB-066: no goatnet site named \`$site\`: ops/ansible/group_vars/${site//-/_}_hosts/ does not exist" >&2
  exit 1
fi

scratch="$(mktemp -d "${TMPDIR:-/tmp}/goatnet-endstate.XXXXXX")"
trap 'rm -rf "$scratch"' EXIT
if [ -z "$rows" ] && [ -z "$facts" ] && [ "$run_assay" = 1 ]; then
  # The standup's phase-13 commands; the assay's verdict is its own, its rows feed ours.
  "$py" "$REPO/ops/validation/generate-values.py" "$site" >/dev/null
  "$py" "$REPO/ops/validation/run-acceptance.py" --env "$site" \
    --values "$REPO/ops/validation/values-$site.yaml" --all-eligible \
    --json "$scratch/assay.jsonl" >/dev/null || true
  [ -s "$scratch/assay.jsonl" ] && rows="$scratch/assay.jsonl"
fi

args=(grade "$site" --root "$root")
[ -n "$facts" ] && args+=(--facts "$facts")
[ -n "$rows" ] && args+=(--assay "$rows")
[ -n "$strict" ] && args+=("$strict")
exec "$py" "$REPO/ops/goatbuilder/endstate/endstate.py" "${args[@]}"
