#!/usr/bin/env bash
# Pinned test runner shared by both CI interpreter jobs.
set -euo pipefail
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: install-bats.sh <install-prefix> [harness-bash]" >&2
  exit 2
fi
prefix=$1
harness_bash=${2:-}
if [ -n "$harness_bash" ]; then
  case "$harness_bash" in
    /*) ;;
    *) echo "FAIL: harness Bash must be an absolute executable path" >&2; exit 2 ;;
  esac
  case "$harness_bash" in
    *[[:space:]]*) echo "FAIL: harness Bash path cannot contain whitespace" >&2; exit 2 ;;
  esac
  if [ ! -x "$harness_bash" ] || ! "$harness_bash" -c 'test "${BASH_VERSINFO[0]}" -ge 5'; then
    echo "FAIL: the Bats harness requires Bash 5 or newer" >&2
    exit 2
  fi
fi
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
curl -fsSL --retry 3 --connect-timeout 20 --max-time 120 \
  https://codeload.github.com/bats-core/bats-core/tar.gz/refs/tags/v1.14.0 \
  -o "$scratch/bats.tar.gz"
digest=bb537b70b15b732f6d8827dd6578e3d8ce166636ce1f18ea9a074184fcce9177
actual=$(shasum -a 256 "$scratch/bats.tar.gz")
if [ "${actual%% *}" != "$digest" ]; then
  echo "FAIL: bats-core v1.14.0 archive checksum mismatch" >&2
  exit 1
fi
tar -xzf "$scratch/bats.tar.gz" -C "$scratch"
bash "$scratch/bats-core-1.14.0/install.sh" "$prefix"
# Bats 1.14.0 under Bash 3.2 can ignore failed [[ assertions. Pin only the
# installed harness entry points to the host shell; PATH still selects 3.2
# for operator scripts and env-bash children in the compatibility job.
if [ -n "$harness_bash" ]; then
  for script in "$prefix/bin/bats" "$prefix"/libexec/bats-core/*; do
    [ -f "$script" ] || continue
    IFS= read -r first_line < "$script"
    [ "$first_line" = '#!/usr/bin/env bash' ] || continue
    { printf '#!%s\n' "$harness_bash"; tail -n +2 "$script"; } > "$scratch/harness-script"
    cat "$scratch/harness-script" > "$script"
  done
fi
"$prefix/bin/bats" --version
