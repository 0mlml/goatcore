#!/usr/bin/env bash
# parity-release.sh — the parity gate for `goatbuilder release` against
# ops/scripts/rc.sh (goatbuilder-conformance.md §The parity gate).
#
# Both run with --dry-run for the same candidate — `--from dev` (tag → dev pin →
# dev deploy → rows → preprod → soak), `--from preprod-rows` (preprod already
# pins the tag: its deploy check, rows, evidence, landing, no re-pin) and
# `--from prod` (graduate → fold → prod deploy → every rung) — and every step and every DRY command they would run is
# compared as a multiset after normalisation: scratch paths, worktree and branch
# stamps, log and body-file names. One shape is reduced before comparing,
# one a documented defect of the script's own dry run, not a difference in what
# a real run does: it delegates the deploy to deploy-component.sh
# (whose parity is parity-deploy.sh's job) where the binary runs its own
# converger and prints that converger's plan. The pin, record and intent edits
# are the script's `python3 -` snippets on one side and the binary's typed
# edits (`pins`, `promote`) on the other: both are reduced to EDIT / PINS /
# TOOL with their arguments, so what is compared is which edit, on what.
#
# usage: parity-release.sh <goatbuilder binary> <component> <tag>
#        env: GOAT_RC_ROLE_CHECK=0 where no person is signed in (CI)
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; REPO="$(cd "$HERE/../.." && pwd)"
BIN="${1:?}"; COMP="${2:?}"; TAG="${3:?}"
export GOAT_MESH_PROBE="${GOAT_MESH_PROBE:-0}"

norm() {
  sed -E 's#\x1b\[[0-9;]*m##g; s#/private/tmp/[^ ]*#TMP#g; s#/var/folders/[^ ]*#TMP#g; s#'"$REPO"'[^ ]*#TMP#g; s#wt-rc-[0-9]+#wt-rc-T#g; s#rc-'"$COMP"'-[0-9]+-[0-9]+#rc-T#g; s#goat-deploy-[^ ]*\.log#LOG#g; s#tmp\.[A-Za-z0-9]+#TMPF#g; s#goatbuilder-pr-body-[0-9]+#TMPF#g; s#\(20[0-9]{2}-[0-9]{2}-[0-9]{2}, ADR#(DATE, ADR#g' \
  | sed -E 's/^rc: /    REFUSED: /' \
  | sed -E 's#DRY: python3 - #DRY: EDIT #; s#DRY: edit [a-z-]+ #DRY: EDIT #; s#DRY: goatbuilder pins --check TMP#DRY: PINS --check#; s#DRY: goatbuilder pins TMP#DRY: PINS#; s#DRY: python3 TMP --check$#DRY: PINS --check#; s#DRY: python3 TMP$#DRY: PINS#; s#DRY: python3 TMP (open|check|abandon|graduate)#DRY: TOOL \1#; s#DRY: python3 ops/release/graduate.py (graduate)#DRY: TOOL \1#; s#DRY: goatbuilder promote (open|check|abandon|graduate)#DRY: TOOL \1#' \
  | grep -E '^(==> |    DRY: |    [a-z]+ on [a-z-]+: |    candidate |    .* is the preprod candidate|    REFUSED: )' \
  | grep -vE 'DRY: TMP '"$COMP"' --goatnet [a-z_]+( --verify-only)? --dry-run$' \
  | sed -E 's/^    //' | sort
}
script_side() { "$REPO/ops/scripts/rc.sh" "$COMP" --tag "$TAG" --from "$1" --ack "Parity Gate" --dry-run 2>&1 | norm; }
binary_side() {
  "$BIN" -C "$REPO" release "$COMP" --tag "$TAG" --from "$1" --ack "Parity Gate" --dry-run 2>&1 \
  | sed -E 's/^(ok|FAIL) +step +//; s/^(ok|FAIL) +note +/    /; s/^FAIL +refused +/    REFUSED: /' \
  | grep -vE '^(ok|FAIL) +[a-z0-9_-]+/' \
  | norm
}

# When main already pins the preprod rung at this tag with its record open, a
# run from dev must reuse that record, never open a second one for the same ref
# (#2086): both sides are held to it, not only to agreeing with each other.
REUSE="$(python3 - "$REPO" "$COMP" "$TAG" <<'PY'
import pathlib, sys, yaml
repo, comp, tag = sys.argv[1:4]
pins = yaml.safe_load(open(f"{repo}/ops/pins/fleet-pins.yaml"))["components"][comp]
ladder = yaml.safe_load(open(f"{repo}/ops/release/ladder.yaml"))
pre = next((r.get("goatnet", "") for r in ladder.get("rungs") or [] if r.get("name") == "preprod"), "")
val = ((pins.get("deviations") or {}).get(pre) or {}).get("value", "")
if pre and val.split("@")[0].endswith(":" + tag.split("/", 1)[1]):
    d12 = val.rsplit(":", 1)[1][:12]
    hits = [p for p in pathlib.Path(repo, "ops/release/records", comp).glob(f"*-{d12}.yaml") if (yaml.safe_load(p.read_text()) or {}).get("state") == "candidate"]
    print("yes" if len(hits) == 1 else "")
PY
)" || REUSE=""

fail=0
for from in dev preprod-rows prod; do
  a="$(script_side "$from" || true)"; b="$(binary_side "$from" || true)"
  if [ -z "$a" ]; then
    fail=1; echo "PARITY FAIL: release $COMP $TAG --from $from — the script produced no steps (refused before step 1? run it by hand to see why)"; continue
  fi
  if [ "$a" != "$b" ]; then
    fail=1; echo "PARITY FAIL: release $COMP $TAG --from $from"
    { diff <(printf '%s\n' "$a") <(printf '%s\n' "$b") || true; } | sed 's/^/    /' | head -30
    echo "    --- goatbuilder, raw (first 12 lines):"; "$BIN" -C "$REPO" release "$COMP" --tag "$TAG" --from "$from" --ack "Parity Gate" --dry-run 2>&1 | head -12 | sed 's/^/    /' | cut -c1-200
  else
    echo "parity ok: release $COMP $TAG --from $from ($(printf '%s\n' "$a" | grep -c .) lines identical)"
  fi
  if [ "$from" = dev ] && [ "$REUSE" = yes ]; then
    if printf '%s\n' "$a" | grep -q 'already runs' && ! printf '%s\n' "$a" | grep -q 'TOOL open'; then
      echo "parity ok: release $COMP $TAG --from dev reuses preprod's open record (#2086)"
    else
      fail=1; echo "PARITY FAIL: release $COMP $TAG --from dev — preprod already runs the tag with its record open, and the run opens another (#2086)"
    fi
  fi
done
exit $fail
