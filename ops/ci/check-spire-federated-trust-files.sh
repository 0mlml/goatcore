#!/usr/bin/env bash
# A federated trust file must never be a CORE router's trust file.
#
# WHY. A federated bundle is a FOREIGN certificate authority. The Zenoh
# ACL matches a peer's Common Name and cannot see which CA issued it, so any CA
# in a router's trust store can mint ANY name that router's ACL recognises --
# including CN=goat-zenoh-router. A partner bundle on a CORE router is therefore
# not "trusting a partner", it is handing them every identity the core knows.
#
# spire-trust-sync writes federated content ONLY to
# spire_trust_sync_federated_trust_files, and the own bundle only to
# spire_trust_sync_router_trust_file. That separation is the safety property.
# This guard refuses the one configuration that would defeat it: naming the same
# file in both.
#
# It also refuses a federated file that looks like a core stack's trust file even
# when the core variable is unset in that group -- a group can inherit the core
# path from a role default, so absence here is not proof of safety.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 1
fail=0
note() { printf "  %s\n" "$1"; }

echo "[spire-federated-trust] scanning group_vars"

# Collect, per file, the core trust path and the federated list.
for f in $(git ls-files 'ops/ansible/group_vars/**/*.yml' 'ops/ansible/host_vars/**/*.yml' 2>/dev/null); do
  grep -q 'spire_trust_sync_federated_trust_files' "$f" 2>/dev/null || continue

  core=$(grep -E '^\s*spire_trust_sync_router_trust_file:' "$f" 2>/dev/null \
         | head -1 | sed -E 's/.*:\s*"?([^"]*)"?\s*$/\1/')

  # the federated list items that follow the key
  n=$(grep -nE '^\s*spire_trust_sync_federated_trust_files:' "$f" | head -1 | cut -d: -f1)
  [ -n "$n" ] || continue
  feds=$(awk -v s="$n" 'NR>s && /^[[:space:]]*-[[:space:]]*"?/{gsub(/^[[:space:]]*-[[:space:]]*"?|"?[[:space:]]*$/,""); print; next} NR>s && NF{exit}' "$f")

  [ -n "$feds" ] || continue
  while IFS= read -r ftf; do
    [ -n "$ftf" ] || continue
    if [ -n "$core" ] && [ "$ftf" = "$core" ]; then
      note "FAIL  $f"
      note "        federated trust file is the CORE trust file: $ftf"
      note "        a partner CA here can mint any name the core ACL knows"
      fail=1
      continue
    fi
    # A federated file living under a core stack's tls dir is the same mistake
    # wearing a different path. Edge stacks are named *-edge-* by convention.
    case "$ftf" in
      *-edge-*|*/edge/*|*edge*) note "ok    $f -> $ftf" ;;
      *) note "FAIL  $f"
         note "        federated trust file does not look like an EDGE stack: $ftf"
         note "        federated bundles belong ONLY on a sovereign edge router."
         note "        If this really is an edge, name it so the path says so."
         fail=1 ;;
    esac
  done <<< "$feds"
done

if [ "$fail" -ne 0 ]; then
  echo "[spire-federated-trust] FAIL — see F-424 and roles/spire/defaults/main.yml"
  exit 1
fi
echo "[spire-federated-trust] PASS"
