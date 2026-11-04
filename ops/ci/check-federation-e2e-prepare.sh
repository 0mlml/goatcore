#!/usr/bin/env bash
# check-federation-e2e-prepare.sh — the two-sovereign runner prepares from a checkout
# shaped like the public one (docs/tutorials/04-two-sovereigns.md).
#
# A public checkout carries no deployment state: no ops/ansible/group_vars/*/pins.yml,
# no ops/goatbuilder/goatbuilder.yaml, no committed intents, and a fleet-pins.yaml that
# declares no goatnet. This copies the working tree, removes all of that, and runs
# `ops/federation-e2e/run.sh --prepare-only` on the copy: the image pins resolve, both
# sovereigns are generated, each clone's config validates and its federation plans.
# The negative control drops the `spire` component and expects the runner to refuse.
#
# Needs python3 with PyYAML, git, openssl, ssh-keygen and a built goatbuilder. No Docker.
#
# usage: ops/ci/check-federation-e2e-prepare.sh <goatbuilder-binary>
# Rollback: rm this file and its step in .github/workflows/federation-e2e.yml.
set -euo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
GB="${1:-}"
[ -x "$GB" ] || { echo "usage: $0 <goatbuilder-binary>" >&2; exit 2; }
GB="$(cd "$(dirname "$GB")" && pwd)/$(basename "$GB")"
T="$(mktemp -d "${TMPDIR:-/tmp}/fed-prepare.XXXXXX")"
trap 'rm -rf "$T"' EXIT

public_copy() { # $1 destination: the working tree as a public checkout would hold it
  local dst="$1"
  python3 - "$REPO" "$dst" <<'PY'
import os, shutil, subprocess, sys
src, dst = sys.argv[1], sys.argv[2]
names = subprocess.run(["git", "-C", src, "ls-files", "-co", "--exclude-standard", "-z"],
                       check=True, capture_output=True).stdout.split(b"\0")
for n in names:
    if not n:
        continue
    rel = n.decode()
    s = os.path.join(src, rel)
    if not os.path.isfile(s) or os.path.islink(s):
        continue
    d = os.path.join(dst, rel)
    os.makedirs(os.path.dirname(d), exist_ok=True)
    shutil.copy2(s, d)
PY
  rm -f "$dst"/ops/ansible/group_vars/*/pins.yml "$dst/ops/goatbuilder/goatbuilder.yaml"
  rm -rf "$dst/ops/consistency/intent"
  python3 - "$dst/ops/pins/fleet-pins.yaml" <<'PY'
import sys, yaml
p = sys.argv[1]
t = yaml.safe_load(open(p))
t["goatnets"] = {}
open(p, "w").write(yaml.safe_dump(t, sort_keys=False))
PY
  git -C "$dst" init -q -b main
  git -C "$dst" add -A
  git -C "$dst" -c user.name=check -c user.email=check@localhost -c commit.gpgsign=false commit -q -m "public-shaped checkout"
}

fail=0
echo "==> a public-shaped checkout prepares both sovereigns"
public_copy "$T/public"
for f in "$T"/public/ops/ansible/group_vars/*/pins.yml "$T/public/ops/goatbuilder/goatbuilder.yaml"; do
  [ ! -e "$f" ] || { echo "FAIL: the public-shaped copy still carries $f" >&2; exit 1; }
done
if "$T/public/ops/federation-e2e/run.sh" --prepare-only --goatbuilder "$GB" --work "$T/work"; then
  echo "ok: run.sh --prepare-only passed on a public-shaped checkout"
else
  echo "FAIL: run.sh --prepare-only failed on a public-shaped checkout" >&2; fail=1
fi
for s in pasture meadow; do
  grep -q "with: $( [ $s = pasture ] && echo meadow || echo pasture )" "$T/work/$s/clone/ops/goatbuilder/goatbuilder.yaml" \
    || { echo "FAIL: $s's clone declares no peering" >&2; fail=1; }
done

echo "==> negative control: no spire component, the runner refuses"
python3 - "$T/public/ops/pins/fleet-pins.yaml" <<'PY'
import sys, yaml
p = sys.argv[1]
t = yaml.safe_load(open(p))
del t["components"]["spire"]
open(p, "w").write(yaml.safe_dump(t, sort_keys=False))
PY
set +e
out="$("$T/public/ops/federation-e2e/run.sh" --prepare-only --goatbuilder "$GB" --work "$T/work-neg" 2>&1)"; rc=$?
set -e
if [ "$rc" = 2 ] && printf '%s' "$out" | grep -q "declares no digest-pinned SPIRE image"; then
  echo "ok: refused with exit 2 and named the missing pin"
else
  echo "FAIL: expected exit 2 naming the SPIRE pin, got $rc: $out" >&2; fail=1
fi
exit "$fail"
