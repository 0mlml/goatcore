#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# test-source-of-record.sh — fresh-source.sh on an ASSEMBLED tree (goatcore at
# the commit in goatcore.ref, an environment overlay copied over it, marked by
# `.goat-assembled`): ops/scripts/lib/source_of_record.py.
#
# Builds a scratch goatcore (this checkout's ops/scripts + a role file) and a
# scratch overlay with a bare origin, assembles them, and checks each verdict:
#   current -> 0, prints the goatcore commit; ledger describe names both commits
#   overlay main moved in a deploy path -> GB-064; in docs/ only -> 0
#   an overlay file edited in the tree -> GB-064; a goatcore file edited -> GB-064
#   HEAD not at goatcore.ref -> GB-064; -dirty overlay -> GB-064
#   no overlay checkout -> GB-065 (offline: 0); overlay origin gone -> GB-065
#   pinned GOAT_FRESH_SOURCE_TRUNK -> 0 with no fetch
#   goatstuff layered (goatstuff_ref=): current -> 0 and describe names it;
#   a goatstuff file edited -> GB-064; goatstuff.ref disagrees -> GB-064;
#   another goatstuff commit layered -> GB-064; no goatstuff checkout -> GB-065
#   (offline: 0); an overlay file replacing a goatstuff file -> 0
# and that a clone (no marker) is still judged by the trunk-remote path.
# No network, no host.
set -euo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
S="$(mktemp -d "${TMPDIR:-/tmp}/sor.XXXXXX")"
trap 'rm -rf "$S"' EXIT
G=(-c user.name=t -c user.email=t@localhost -c commit.gpgsign=false -c init.defaultBranch=main)
unset GOAT_FRESH_SOURCE_TRUNK GOAT_DEPLOY_OFFLINE GOAT_OVERLAY_DIR GOAT_OVERLAY_REMOTE GOAT_OVERLAY_TRUNK_REMOTE GOAT_GOATSTUFF_DIR
FAILS=0
ok()   { echo "ok   $1"; }
bad()  { echo "FAIL $1"; FAILS=$((FAILS + 1)); }

# --- goatcore: the tooling under test plus one role file -----------------------
mkdir -p "$S/core/ops/scripts/lib" "$S/core/ops/ansible/roles/x/tasks"
cp "$REPO/ops/scripts/fresh-source.sh" "$S/core/ops/scripts/"
cp "$REPO/ops/scripts/lib/source_of_record.py" "$S/core/ops/scripts/lib/"
echo "- debug: msg=core" > "$S/core/ops/ansible/roles/x/tasks/main.yml"
echo "core: example" > "$S/core/ops/ansible/group_vars_all.yml"
git "${G[@]}" -C "$S/core" init -q
git -C "$S/core" add -A
git "${G[@]}" -C "$S/core" commit -q -m "chore: export from the source tree at 0123456789ab"
CORE="$(git -C "$S/core" rev-parse HEAD)"

# --- overlay with a bare origin -------------------------------------------------
mkdir -p "$S/ov/ops/ansible/group_vars/x_hosts" "$S/ov/docs"
echo "x: 1" > "$S/ov/ops/ansible/group_vars/x_hosts/vars.yml"
echo "notes" > "$S/ov/docs/notes.md"
echo "$CORE" > "$S/ov/goatcore.ref"
echo "#!/bin/sh" > "$S/ov/assemble.sh"
git "${G[@]}" -C "$S/ov" init -q
git -C "$S/ov" add -A
git "${G[@]}" -C "$S/ov" commit -q -m "overlay"
git "${G[@]}" init -q --bare "$S/ov.git"
git -C "$S/ov" remote add origin "$S/ov.git"
git -C "$S/ov" push -q origin main

assemble() {   # -> $S/t
  rm -rf "$S/t"
  git clone -q "$S/core" "$S/t"
  git -C "$S/t" -c advice.detachedHead=false checkout -q "$CORE"
  (cd "$S/ov" && git ls-files) | while IFS= read -r f; do
    case "$f" in assemble.sh|README.md) continue ;; esac
    mkdir -p "$S/t/$(dirname "$f")"; cp -p "$S/ov/$f" "$S/t/$f"
  done
  printf 'goatcore_ref=%s\noverlay_commit=%s\noverlay_path=%s\n' "$CORE" "$(git -C "$S/ov" rev-parse HEAD)" "$S/ov" > "$S/t/.goat-assembled"
}
fs() { "$S/t/ops/scripts/fresh-source.sh" --repo "$S/t" "$@"; }
expect() {   # <name> <rc> <pattern|-> <cmd...>
  local name="$1" want="$2" pat="$3"; shift 3
  local out rc=0
  out="$("$@" 2>&1)" || rc=$?
  if [ "$rc" != "$want" ]; then bad "$name: exit $rc, wanted $want"; printf '%s\n' "$out" | head -4 | sed 's/^/     /'; return; fi
  if [ "$pat" != - ] && ! printf '%s' "$out" | grep -q -- "$pat"; then bad "$name: no '$pat' in output"; printf '%s\n' "$out" | head -4 | sed 's/^/     /'; return; fi
  ok "$name"
}

assemble
expect "current tree runs" 0 "$CORE" fs
D="$(python3 "$S/t/ops/scripts/lib/source_of_record.py" describe --repo "$S/t")"
if printf '%s' "$D" | grep -q "\"goatcore\":\"$CORE\"" && printf '%s' "$D" | grep -q "\"overlay\":\"$(git -C "$S/ov" rev-parse HEAD)\"" \
   && printf '%s' "$D" | grep -q '"goatcore_export_of":"0123456789ab"'; then ok "describe names both commits and the export source"; else bad "describe: $D"; fi
expect "pinned run, no fetch" 0 "$CORE" env GOAT_FRESH_SOURCE_TRUNK="$CORE" "$S/t/ops/scripts/fresh-source.sh" --repo "$S/t" --quiet

# overlay main moves in docs only -> still current; in a deploy path -> behind
git clone -q "$S/ov.git" "$S/ov2"
echo more >> "$S/ov2/docs/notes.md"; git -C "$S/ov2" add -A; git "${G[@]}" -C "$S/ov2" commit -q -m "docs only"; git -C "$S/ov2" push -q origin main
expect "overlay main moved in docs/ only" 0 "$CORE" fs
echo "x: 2" > "$S/ov2/ops/ansible/group_vars/x_hosts/vars.yml"; git -C "$S/ov2" add -A; git "${G[@]}" -C "$S/ov2" commit -q -m "x to 2"; git -C "$S/ov2" push -q origin main
expect "overlay behind its main in a deploy path" 1 "GB-064 REFUSE.*behind" fs
git -C "$S/ov" pull -q --ff-only origin main
assemble
expect "re-assembled tree runs" 0 "$CORE" fs

echo "x: hand" > "$S/t/ops/ansible/group_vars/x_hosts/vars.yml"
expect "overlay file edited in the tree" 1 "edited after assembly" fs
assemble
echo "- debug: msg=hand" > "$S/t/ops/ansible/roles/x/tasks/main.yml"
expect "goatcore file edited in the tree" 1 "neither goatcore" fs
assemble
echo "stray" > "$S/t/ops/ansible/new.yml"
expect "file added in the tree" 1 "neither goatcore" fs

assemble
git "${G[@]}" -C "$S/t" commit -q --allow-empty -m moved
expect "HEAD not at goatcore.ref" 1 "not the goatcore.ref commit" fs

assemble
sed -i.bak -E 's/^(overlay_commit=.*)$/\1-dirty/' "$S/t/.goat-assembled"; rm -f "$S/t/.goat-assembled.bak"
expect "overlay assembled dirty" 1 "uncommitted" fs

assemble
grep -v '^overlay_path=' "$S/t/.goat-assembled" > "$S/m"; mv "$S/m" "$S/t/.goat-assembled"
expect "no overlay checkout known" 1 "GB-065 REFUSE" fs
expect "no overlay checkout, offline" 0 "$CORE" fs --offline
expect "overlay checkout from GOAT_OVERLAY_DIR" 0 "$CORE" env GOAT_OVERLAY_DIR="$S/ov" "$S/t/ops/scripts/fresh-source.sh" --repo "$S/t"

assemble
git -C "$S/ov" remote set-url origin "$S/nonexistent.git"
expect "overlay origin unreachable" 1 "GB-065 REFUSE.*could not fetch" fs
expect "overlay origin unreachable, offline" 0 "OFFLINE" fs --offline
git -C "$S/ov" remote set-url origin "$S/ov.git"

# --- goatstuff layered between goatcore and the overlay (goatstuff_ref=) --------
mkdir -p "$S/stuff/ops/ansible/roles/y/tasks" "$S/stuff/ops/ansible/roles/z/defaults"
echo "- debug: msg=stuff" > "$S/stuff/ops/ansible/roles/y/tasks/main.yml"
echo "z: stuff" > "$S/stuff/ops/ansible/roles/z/defaults/main.yml"
echo "goatstuff" > "$S/stuff/README.md"
git "${G[@]}" -C "$S/stuff" init -q
git -C "$S/stuff" add -A
git "${G[@]}" -C "$S/stuff" commit -q -m "goatstuff"
STUFF="$(git -C "$S/stuff" rev-parse HEAD)"
echo "z: stuff2" > "$S/stuff/ops/ansible/roles/z/defaults/main.yml"
git -C "$S/stuff" add -A
git "${G[@]}" -C "$S/stuff" commit -q -m "goatstuff moves"
STUFF2="$(git -C "$S/stuff" rev-parse HEAD)"
echo "$STUFF" > "$S/ov/goatstuff.ref"
git -C "$S/ov" add -A
git "${G[@]}" -C "$S/ov" commit -q -m "pin goatstuff"
git -C "$S/ov" push -q origin main

assemble_stuff() {   # <goatstuff commit> -> $S/t: goatcore, then goatstuff's ops/, then the overlay
  rm -rf "$S/t" "$S/st"
  git clone -q "$S/core" "$S/t"
  git -C "$S/t" -c advice.detachedHead=false checkout -q "$CORE"
  git clone -q "$S/stuff" "$S/st"
  git -C "$S/st" -c advice.detachedHead=false checkout -q "$1"
  (cd "$S/st" && git ls-files ops) | while IFS= read -r f; do
    mkdir -p "$S/t/$(dirname "$f")"; cp -p "$S/st/$f" "$S/t/$f"
  done
  (cd "$S/ov" && git ls-files) | while IFS= read -r f; do
    case "$f" in assemble.sh|README.md) continue ;; esac
    mkdir -p "$S/t/$(dirname "$f")"; cp -p "$S/ov/$f" "$S/t/$f"
  done
  printf 'goatcore_ref=%s\noverlay_commit=%s\ngoatstuff_ref=%s\noverlay_path=%s\ngoatstuff_path=%s\n' \
    "$CORE" "$(git -C "$S/ov" rev-parse HEAD)" "$STUFF" "$S/ov" "$S/stuff" > "$S/t/.goat-assembled"
}

assemble_stuff "$STUFF"
expect "goatstuff layered, current tree runs" 0 "$CORE" fs
D="$(python3 "$S/t/ops/scripts/lib/source_of_record.py" describe --repo "$S/t")"
if printf '%s' "$D" | grep -q "\"goatstuff\":\"$STUFF\"" && printf '%s' "$D" | grep -q "\"goatstuff_ref\":\"$STUFF\"" \
   && printf '%s' "$D" | grep -q "\"goatcore\":\"$CORE\""; then ok "describe names the goatstuff commit"; else bad "describe with goatstuff: $D"; fi
expect "goatstuff layered, pinned run" 0 "$CORE" env GOAT_FRESH_SOURCE_TRUNK="$CORE" "$S/t/ops/scripts/fresh-source.sh" --repo "$S/t" --quiet

echo "- debug: msg=hand" > "$S/t/ops/ansible/roles/y/tasks/main.yml"
expect "goatstuff file edited in the tree" 1 "differ from goatstuff" fs

assemble_stuff "$STUFF2"
expect "another goatstuff commit layered than the marker names" 1 "differ from goatstuff" fs

assemble_stuff "$STUFF"
echo "$STUFF2" > "$S/t/goatstuff.ref"
expect "goatstuff.ref disagrees with the marker" 1 "goatstuff.ref in this tree names" fs

assemble_stuff "$STUFF"
sed -i.bak -E 's/^goatstuff_ref=.*$/goatstuff_ref=abc/' "$S/t/.goat-assembled"; rm -f "$S/t/.goat-assembled.bak"
expect "goatstuff_ref not a commit" 1 "not a 40-hex commit" fs

assemble_stuff "$STUFF"
grep -v '^goatstuff_path=' "$S/t/.goat-assembled" > "$S/m"; mv "$S/m" "$S/t/.goat-assembled"
expect "no goatstuff checkout known" 1 "GB-065 REFUSE.*goatstuff" fs
expect "no goatstuff checkout, offline" 0 "$CORE" fs --offline
expect "goatstuff checkout from GOAT_GOATSTUFF_DIR" 0 "$CORE" env GOAT_GOATSTUFF_DIR="$S/stuff" "$S/t/ops/scripts/fresh-source.sh" --repo "$S/t"

# the overlay replaces a goatstuff file: the overlay's bytes are judged, not goatstuff's
mkdir -p "$S/ov/ops/ansible/roles/z/defaults"
echo "z: ours" > "$S/ov/ops/ansible/roles/z/defaults/main.yml"
git -C "$S/ov" add -A
git "${G[@]}" -C "$S/ov" commit -q -m "overlay replaces z"
git -C "$S/ov" push -q origin main
assemble_stuff "$STUFF"
expect "an overlay file replacing a goatstuff file runs" 0 "$CORE" fs

# a marker without goatstuff_ref records no goatstuff
assemble
D="$(python3 "$S/t/ops/scripts/lib/source_of_record.py" describe --repo "$S/t")"
if printf '%s' "$D" | grep -q goatstuff; then bad "describe without goatstuff_ref names goatstuff: $D"; else ok "describe without goatstuff_ref names no goatstuff"; fi

# a clone (no marker) still takes the trunk-remote path: goatcore's main moves
# in ops/ansible and a plain clone at the older commit is refused as behind it
rm -rf "$S/t"
git clone -q "$S/core" "$S/t"
git -C "$S/t" -c advice.detachedHead=false checkout -q "$CORE"
echo "- debug: msg=core2" > "$S/core/ops/ansible/roles/x/tasks/main.yml"
git -C "$S/core" add -A
git "${G[@]}" -C "$S/core" commit -q -m "core moves"
expect "clone without a marker is judged against its trunk remote" 1 "behind origin/main" env GOAT_ANSIBLE_TRUNK_REMOTE=origin "$S/t/ops/scripts/fresh-source.sh" --repo "$S/t"
K="$(python3 "$S/t/ops/scripts/lib/source_of_record.py" kind --repo "$S/t")"
[ "$K" = clone ] && ok "kind: clone" || bad "kind: $K"

if [ "$FAILS" = 0 ]; then echo "test-source-of-record: PASS"; else echo "test-source-of-record: $FAILS FAILED"; exit 1; fi
