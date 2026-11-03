#!/usr/bin/env bash
# sync-goatnet-from-scratch.sh — keep the Hello Galaxy repository's copy of the worked
# example equal to this repository's (docs/design/goatnet-tutorials.md).
#
#   ops/tutorials/sync-goatnet-from-scratch.sh <clone of hello-galaxy-goatnet-from-scratch> [--apply]
#
# ops/goatbuilder/fixtures/goatnet-from-scratch/ is the canonical copy: goatbuilder's
# tutor, the goatnet_from_scratch_* tests and ops/tutorials/check.sh read it on every
# pull request here. USAREUR-AF/hello-galaxy-goatenv carries the
# same step directories and checkpoints at its root for a reader who clones the
# example alone. Without --apply this compares the two and exits 1 on any
# difference; with --apply it copies this repository's version over the clone's
# and sets the clone's GOATBUILDER_REF to this checkout's HEAD, leaving the
# commit to you. Run it in the same change that edits the fixture or re-records
# a checkpoint.
#
# Bash 3.2 portable.
set -euo pipefail

dest="${1:?usage: sync-goatnet-from-scratch.sh <clone of hello-galaxy-goatnet-from-scratch> [--apply]}"
apply="${2:-}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
src="$ROOT/ops/goatbuilder/fixtures/goatnet-from-scratch"
[ -x "$dest/run" ] || { echo "sync-goatnet-from-scratch.sh: $dest is not a clone of hello-galaxy-goatnet-from-scratch (no ./run)" >&2; exit 2; }

parts="01-founder 02-first-federated-node 03-boundary-node checkpoints"
fail=0
for p in $parts; do
  if [ "$apply" = "--apply" ]; then
    rm -rf "${dest:?}/$p"; cp -R "$src/$p" "$dest/$p"; echo "copied $p"
  elif diff -r "$src/$p" "$dest/$p" > /dev/null; then
    echo "ok    $p"
  else
    echo "DIFF  $p"; diff -r "$src/$p" "$dest/$p" | sed 's/^/      /' | head -20; fail=1
  fi
done
if [ "$apply" = "--apply" ]; then
  git -C "$ROOT" rev-parse HEAD > "$dest/GOATBUILDER_REF"
  echo "GOATBUILDER_REF -> $(cat "$dest/GOATBUILDER_REF")"
fi
exit "$fail"
