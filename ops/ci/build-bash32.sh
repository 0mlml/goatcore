#!/usr/bin/env bash
# GNU's immutable source archive, built serially for its old dependency graph.
set -euo pipefail
if [ "$#" -ne 1 ]; then
  echo "usage: build-bash32.sh <install-prefix>" >&2
  exit 2
fi
prefix=$1
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
curl -fsSL --retry 3 --connect-timeout 20 --max-time 120 \
  https://ftpmirror.gnu.org/bash/bash-3.2.57.tar.gz -o "$scratch/bash.tar.gz" \
  || curl -fsSL --retry 3 --connect-timeout 20 --max-time 120 \
    https://mirrors.kernel.org/gnu/bash/bash-3.2.57.tar.gz -o "$scratch/bash.tar.gz"
digest=3fa9daf85ebf35068f090ce51283ddeeb3c75eb5bc70b1a4a7cb05868bfe06a4
actual=$(shasum -a 256 "$scratch/bash.tar.gz")
if [ "${actual%% *}" != "$digest" ]; then
  echo "FAIL: GNU Bash 3.2.57 archive checksum mismatch" >&2
  exit 1
fi
tar -xzf "$scratch/bash.tar.gz" -C "$scratch"
cd "$scratch/bash-3.2.57"
# Both flags are needed: build tools do not inherit CFLAGS in Bash 3.2.
CFLAGS='-O2 -std=gnu89 -Wno-implicit-function-declaration' \
CFLAGS_FOR_BUILD='-O2 -std=gnu89 -Wno-implicit-function-declaration' \
  ./configure --prefix="$prefix"
make
make install
# Expand version fields in the newly built interpreter, not this shell.
# shellcheck disable=SC2016
"$prefix/bin/bash" -c 'test "${BASH_VERSINFO[0]}:${BASH_VERSINFO[1]}:${BASH_VERSINFO[2]}" = "3:2:57"'
