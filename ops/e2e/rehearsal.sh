#!/usr/bin/env bash
# rehearsal.sh — every open release candidate's NEXT step, dry-run, before a person runs it.
#
# docs/design/deployment-e2e.md §Release rehearsal. For each candidate record in
# state `candidate` (ops/release/records/<component>/*.yaml, class ladder) the
# next step is `--from prod` (graduate after the soak); for each component whose
# dev rung carries a candidate deviation with no open record, the next step is
# `--from dev` (preprod). Both drivers rehearse it: rc.sh --dry-run and
# goatbuilder release --dry-run. A refusal here is found today, not on the day.
#
# usage: ops/e2e/rehearsal.sh [--goatbuilder BIN] [--record DIR]
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; REPO="$(cd "$HERE/../.." && pwd)"
GB=""; RECORD=""
while [ $# -gt 0 ]; do
  case "$1" in
    --goatbuilder) GB="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"; shift 2 ;;
    --record) RECORD="$2"; shift 2 ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "rehearsal: unknown argument $1" >&2; exit 2 ;;
  esac
done
[ -n "$RECORD" ] || RECORD="$(mktemp -d "${TMPDIR:-/tmp}/goat-rehearsal.XXXXXX")"
mkdir -p "$RECORD"; export E2E_RECORD_DIR="$RECORD"
export GOAT_RC_ROLE_CHECK="${GOAT_RC_ROLE_CHECK:-0}" GOAT_MESH_PROBE="${GOAT_MESH_PROBE:-0}"
FAILS=0
rec() { python3 "$HERE/rec.py" "$@" || FAILS=$((FAILS + 1)); }
cd "$REPO"

# The candidates and their next steps, from the records and the pins.
python3 - "$REPO" > "$RECORD/candidates.tsv" <<'PY'
import glob, os, sys, yaml
repo = sys.argv[1]
pins = yaml.safe_load(open(os.path.join(repo, "ops/pins/fleet-pins.yaml")))
ladder = yaml.safe_load(open(os.path.join(repo, "ops/release/ladder.yaml")))
rungs = {r["name"]: r["goatnet"] for r in ladder["rungs"] if r.get("status") == "live"}
seen = set()
for f in sorted(glob.glob(os.path.join(repo, "ops/release/records/*/*.yaml"))):
    r = yaml.safe_load(open(f)) or {}
    if r.get("state") != "candidate" or r.get("class") == "hotfix":
        continue
    comp = r.get("component"); ref = str(r.get("candidate_ref", ""))
    tag = ref.split("@")[0].rsplit(":", 1)[-1]
    wf = (pins["components"].get(comp) or {}).get("image_workflow") or ""
    image = os.path.basename(wf).replace("-image.yml", "") if wf else comp
    if tag.startswith("v") and "-rc." in tag:
        print(f"{comp}\t{image}/{tag}\tprod\t{os.path.relpath(f, repo)}")
        seen.add(comp)
for comp, spec in (pins.get("components") or {}).items():
    dev = (spec.get("deviations") or {}).get(rungs.get("dev", ""), {})
    val = str((dev or {}).get("value", ""))
    tag = val.split("@")[0].rsplit(":", 1)[-1]
    if comp not in seen and tag.startswith("v") and "-rc." in tag:
        wf = spec.get("image_workflow") or ""
        image = os.path.basename(wf).replace("-image.yml", "") if wf else comp
        print(f"{comp}\t{image}/{tag}\tdev\t-")
PY
n=0
while IFS=$'\t' read -r comp tag from rec_path; do
  [ -n "$comp" ] || continue
  n=$((n + 1))
  printf '\n\033[1m==> %s %s: next step --from %s%s\033[0m\n' "$comp" "$tag" "$from" "${rec_path:+ (record $rec_path)}"
  rec "rc-$comp-from-$from" --expect-in "candidate tag $tag" \
    -- "$REPO/ops/scripts/rc.sh" "$comp" --tag "$tag" --from "$from" --ack "rehearsal" --dry-run
  if [ -n "$GB" ]; then
    rec "goatbuilder-$comp-from-$from" --expect-in "$tag" \
      -- "$GB" release "$comp" --tag "$tag" --from "$from" --ack "rehearsal" --dry-run
  fi
done < "$RECORD/candidates.tsv"
[ "$n" = 0 ] && echo "rehearsal: no open release candidate — nothing to rehearse"
# The runner's own toolchain for a browser row, so the daily run says whether the
# machine that rehearses could also run the rows.
rec browser-toolchain-present --expect-exit 0 -- bash -c 'command -v node && command -v npm && echo "node+npm present; the console e2e node_modules is per checkout: (cd ops/goat-id-console/web/e2e && npm ci && npx playwright install chromium)"'
python3 "$HERE/report.py" "$RECORD" --check --title "release rehearsal" || true
echo "record: $RECORD"
[ "$FAILS" = 0 ] && exit 0 || exit 1
