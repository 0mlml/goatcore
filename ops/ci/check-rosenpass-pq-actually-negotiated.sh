#!/usr/bin/env bash
# check-rosenpass-pq-actually-negotiated.sh — rosenpass declaration coherence
# (coupled-fact guard #3, redteam remediation).
#
# THE COUPLED FACTS: `rosenpass_enabled: true` in a goatnet's group_vars says
# intra-mesh PQC is ON; the effective behavior is whatever the computed flags
# fragment (group_vars/all.yml ~line 44) hands `netbird up`:
#
#   rosenpass_up_flags: "{{ '--enable-rosenpass --rosenpass-permissive'
#                           if (rosenpass_enabled | bool) else '' }}"
#
# WHAT THIS GUARD CAN AND CANNOT PROVE (P6 — read before trusting a green):
# `--rosenpass-permissive` is the deliberate staged-rollout posture: a peer
# that fails (or never attempts) the Rosenpass handshake silently continues
# on classical WireGuard, so NO static check can prove PQ is actually
# negotiated. A green here means exactly three things —
#   1. the flag→flags coupling in all.yml is intact (enabled really appends
#      --enable-rosenpass),
#   2. no scope severs that coupling by overriding rosenpass_up_flags,
#   3. a RUNTIME probe exists in the acceptance suite, because the runtime
#      half is the only place "PQ is on" can be verified: from an enrolled
#      peer,  netbird status | grep 'Quantum resistance: true'.
# The static verdict on "is PQ negotiated" is UNKNOWN, never PASS. This guard
# FAILS while any goatnet declares rosenpass_enabled: true and the acceptance
# suite carries no such probe — a flag that nothing anywhere can verify is
# exactly the defect class this campaign closes. Do not green it by weakening
# check 3; green it by adding the probe.
#
# --self-test: proves the guard RED against broken fixtures and GREEN against
# a coherent one before its verdict on the real tree is trusted (P6). CI runs
# the self-test before every real run.
set -euo pipefail

if [ "${1:-}" = "--self-test" ]; then
  self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  good_all() {  # a coherent all.yml compute
    mkdir -p "$1/group_vars"
    cat > "$1/group_vars/all.yml" <<'EOF'
rosenpass_enabled: false
rosenpass_up_flags: "{{ '--enable-rosenpass --rosenpass-permissive' if (rosenpass_enabled | bool) else '' }}"
EOF
  }
  enabled_scope() {
    mkdir -p "$1/group_vars/pq_hosts"
    echo "rosenpass_enabled: true" > "$1/group_vars/pq_hosts/vars.yml"
  }
  probe_yaml() {
    mkdir -p "$(dirname "$1")"
    cat > "$1" <<'EOF'
  - id: rosenpass-pq-negotiated
    probe: cmd-assert
    params: {cmd: "netbird status | grep -c 'Quantum resistance: true'", expect: "1"}
EOF
  }
  # RED A: all.yml compute decoupled — enabled no longer appends --enable-rosenpass.
  mkdir -p "$tmp/redA/group_vars"
  cat > "$tmp/redA/group_vars/all.yml" <<'EOF'
rosenpass_enabled: false
rosenpass_up_flags: ""
EOF
  enabled_scope "$tmp/redA"; probe_yaml "$tmp/redA/acceptance.yaml"
  if "$self" "$tmp/redA" "$tmp/redA/acceptance.yaml" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed a decoupled rosenpass_up_flags compute"
    exit 1
  fi
  echo "self-test: RED proven — decoupled flag compute fails"
  # RED B: a scope overrides rosenpass_up_flags, severing the flag→flags coupling.
  good_all "$tmp/redB"; enabled_scope "$tmp/redB"; probe_yaml "$tmp/redB/acceptance.yaml"
  echo 'rosenpass_up_flags: ""' >> "$tmp/redB/group_vars/pq_hosts/vars.yml"
  if "$self" "$tmp/redB" "$tmp/redB/acceptance.yaml" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed a scope-level rosenpass_up_flags override"
    exit 1
  fi
  echo "self-test: RED proven — scope override of rosenpass_up_flags fails"
  # RED C: enabled somewhere, but the acceptance suite has NO runtime PQ probe.
  good_all "$tmp/redC"; enabled_scope "$tmp/redC"
  echo "  - id: unrelated-row" > "$tmp/redC/acceptance.yaml"
  if "$self" "$tmp/redC" "$tmp/redC/acceptance.yaml" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed rosenpass_enabled:true with no runtime probe anywhere"
    exit 1
  fi
  echo "self-test: RED proven — enabled-with-no-runtime-probe fails"
  # GREEN: coherent compute + enabled scope + runtime probe present.
  good_all "$tmp/green"; enabled_scope "$tmp/green"; probe_yaml "$tmp/green/acceptance.yaml"
  "$self" "$tmp/green" "$tmp/green/acceptance.yaml" >/dev/null
  echo "self-test: GREEN proven — coherent + probed fixture passes"
  echo "OK: rosenpass-pq self-test — red-first proof holds"
  exit 0
fi

[ $# -eq 0 ] && cd "$(git rev-parse --show-toplevel)"
ANSIBLE_DIR="${1:-ops/ansible}"
ACCEPTANCE="${2:-ops/validation/acceptance-tests.yaml}"
ALL_YML="$ANSIBLE_DIR/group_vars/all.yml"
fail=0
note() { echo "FAIL: $*"; fail=1; }

# 1. The flag→flags coupling in all.yml must be intact: the ONE compute that
#    turns the declaration into behavior appends --enable-rosenpass gated on
#    rosenpass_enabled. If it drifts, every rosenpass_enabled: true fleet-wide
#    becomes a no-op that still reads "on".
compute="$(grep -E '^rosenpass_up_flags\s*:' "$ALL_YML" || true)"
if [ -z "$compute" ]; then
  note "$ALL_YML no longer defines rosenpass_up_flags — the flag→flags compute is gone; every rosenpass_enabled: true is a dead declaration"
else
  echo "$compute" | grep -Fq -- '--enable-rosenpass' \
    || note "$ALL_YML rosenpass_up_flags no longer appends --enable-rosenpass — rosenpass_enabled: true would enable NOTHING while still reading on"
  echo "$compute" | grep -Fq 'rosenpass_enabled' \
    || note "$ALL_YML rosenpass_up_flags is no longer computed FROM rosenpass_enabled — the flag and the behavior are decoupled facts again"
fi

# 2. No scope may sever the coupling by overriding the computed fragment.
overrides="$(grep -rn -E '^rosenpass_up_flags\s*:' \
  "$ANSIBLE_DIR/group_vars" "$ANSIBLE_DIR/inventory/group_vars" "$ANSIBLE_DIR/roles" 2>/dev/null \
  | grep -v "group_vars/all.yml:" || true)"
if [ -n "$overrides" ]; then
  note "rosenpass_up_flags overridden outside group_vars/all.yml — the scope's rosenpass_enabled flag no longer controls its behavior:"
  printf '  %s\n' "$overrides"
fi

# 3. Declarations exist ⇒ the runtime half must exist. Permissive mode makes
#    "PQ is on" statically unverifiable, so an acceptance-suite probe (the
#    `netbird status` Quantum-resistance check from an enrolled peer) is the
#    only enforcement point; without it the flag is verified by NOTHING.
enabled="$(grep -rln -E '^rosenpass_enabled\s*:\s*[Tt]rue' "$ANSIBLE_DIR/group_vars" 2>/dev/null || true)"
if [ -n "$enabled" ]; then
  if ! grep -qi 'Quantum resistance' "$ACCEPTANCE" 2>/dev/null; then
    note "rosenpass_enabled: true is declared but $ACCEPTANCE carries NO runtime PQ probe (no 'Quantum resistance' assertion). --rosenpass-permissive means a peer silently falls back to classical WireGuard, so with no probe the PQ-on claim is verified by nothing, anywhere. Add an acceptance row asserting \`netbird status | grep 'Quantum resistance: true'\` from an enrolled peer. Declaring scopes:"
    printf '  %s\n' $enabled
  fi
fi

if [ "$fail" != 0 ]; then
  echo
  echo "rosenpass declaration is incoherent or unverifiable (see above). Static"
  echo "analysis can only prove the declaration WOULD actuate and that a runtime"
  echo "probe exists — never that PQ is negotiated (P6: UNKNOWN, not PASS)."
  exit 1
fi
echo "OK: rosenpass flag→flags coupling intact, no scope overrides, runtime probe present."
echo "NOTE (P6): static analysis alone cannot prove PQ negotiation under --rosenpass-permissive;"
echo "'PQ is on' remains UNKNOWN until the acceptance probe passes on a live peer."
