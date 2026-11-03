#!/usr/bin/env bash
# serve.sh — run the consistency console in CF-7 AGGREGATOR MODE on the
# operator laptop (docs/design/consistency-foundation.md §7.8; the
# ops/relay/health/serve.sh precedent). This is the aggregator's FIRST
# deployment: laptop-run, loopback-only, reaching each goatnet's zenoh router
# through its env-door tunnel (docs/design/env-doors.md, ADR 1117). No hosted
# deployment exists; the rear-hosted/cloud variant is an operator-gated
# decision recorded in §7.8.
#
# Read-only by construction: the binary subscribes and renders; it publishes
# nothing, writes nothing, and no goatnet notices whether it runs (§7.9 C1).
#
# ONE-TIME per goatnet: mint a subscribe-only client leaf with that goatnet's
# zenoh CA (subject CN=goat-consistency-console-trial-peer — the same subject
# each goatnet's deployed console already has an ACL rule for) and place
# leaf-cert.pem / leaf-key.pem / ca-root-cert.pem under certs/<goatnet>/
# (gitignored). Then copy aggregator.example.yaml -> aggregator.local.yaml.
#
# EACH SESSION, open the doors (one terminal per command; ADR 1117 Tier 1 —
# SSM idle-timeout ~20min, re-run env-door.sh if a port goes quiet):
#
#   <goatnet-a>   ops/scripts/env-door.sh <goatnet-a>
#                 ssh -p <door-port-a> -N -L 17447:<mesh-ip-a>:7447 <ssh-user>@127.0.0.1
#   <goatnet-b>   ops/scripts/env-door.sh <goatnet-b>
#                 ssh -p <door-port-b> -N -L 27447:<mesh-ip-b>:7447 <ssh-user>@127.0.0.1
#
# (The -L target is the mgmt host's own mesh IP — the router publishes 7447
# there. The zenoh session then rides tls/127.0.0.1:<port> end-to-end mTLS;
# the tunnel moves ciphertext only.)
#
# Usage: ./serve.sh [--config aggregator.local.yaml] [--listen 127.0.0.1:9761]
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG="$HERE/aggregator.local.yaml"
LISTEN="127.0.0.1:9761" # loopback ONLY — the laptop page has no auth fence
while [ $# -gt 0 ]; do
  case "$1" in
    --config) CONFIG="$2"; shift ;;
    --listen) LISTEN="$2"; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

if [ ! -f "$CONFIG" ]; then
  echo "aggregator: no config at $CONFIG" >&2
  echo "  copy $HERE/aggregator.example.yaml -> $CONFIG and fill in the cert paths" >&2
  exit 2
fi

# Best-effort door preflight: warn (never fail) on a closed local endpoint —
# an unreachable goatnet renders indeterminate on the page, which is the
# honest answer, not an error.
grep -Eo '(tls|tcp)/[0-9.]+:[0-9]+' "$CONFIG" | sed 's#^[a-z]*/##' | sort -u | while IFS=: read -r host port; do
  if ! nc -z "$host" "$port" >/dev/null 2>&1; then
    echo "aggregator: warn — $host:$port not answering; is that goatnet's env-door forward up? (its row will render indeterminate, not green)" >&2
  fi
done

echo "aggregator: building goat-consistency-console (release, --locked)…"
cargo build --release --locked --manifest-path "$HERE/../rust/Cargo.toml" -p goat-consistency-console

echo "aggregator: serving http://$LISTEN/ from $CONFIG"
exec "$HERE/../rust/target/release/goat-consistency-console" --aggregate "$CONFIG" --listen "$LISTEN"
