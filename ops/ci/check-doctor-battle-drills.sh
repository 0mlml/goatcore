#!/usr/bin/env bash
# Keep the seven offline doctor drills complete and linked from their index.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
drills="$root/docs/operations/battle-drills"

for n in 01 02 03 04 05 06 07; do
  files=("$drills/$n"-*.md)
  if [[ ${#files[@]} -ne 1 || ! -f ${files[0]} ]]; then
    echo "FAIL: expected one drill numbered $n" >&2
    exit 1
  fi
  file="${files[0]}"
  for heading in Symptom Conditions Steps Pass Escalate Evidence; do
    if ! grep -qx "## $heading" "$file"; then
      echo "FAIL: $file lacks section $heading" >&2
      exit 1
    fi
  done
  if ! grep -Fq "($(basename "$file"))" "$drills/README.md"; then
    echo "FAIL: $(basename "$file") is absent from the index" >&2
    exit 1
  fi
done

echo "OK: seven indexed doctor battle drills have the required sections."
