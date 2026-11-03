#!/usr/bin/env bash
# cold-boot-drill.sh — turnkey re-runnable Block 65A.1 verification.
#
# Verifies that TPM2 auto-unlock of the docker-data LUKS volume worked
# on the Orin's most-recent boot (passive mode) OR forces a fresh
# cold-boot and re-verifies (active mode).
#
# Per ADR 0612 (LUKS multi-modal continuity priority), TPM2 auto-unlock
# is the load-bearing path for unattended power cycles. This drill is
# the gate that turns "we believe the box auto-unlocks" into "we can
# show the box auto-unlocks."
#
# Modes:
#   passive (default) — extract systemd-cryptsetup journal evidence
#                       from the current boot. ~30 seconds. No service
#                       impact. Sufficient for routine verification.
#   active            — force a fresh reboot via `sudo systemctl reboot`,
#                       wait for SSH to come back, re-verify. ~5 min
#                       per host. Brief container-down window during
#                       reboot; acceptable for non-load-bearing hosts
#                       or maintenance windows on load-bearing ones.
#
# Usage:
#   ./cold-boot-drill.sh passive <ssh-target> [<ssh-target> ...]
#   ./cold-boot-drill.sh active  <ssh-target> [<ssh-target> ...]
#
# Examples:
#   # Routine vega fleet check
#   ./cold-boot-drill.sh passive goat@192.168.1.132 \
#                                  goat@192.168.1.128 \
#                                  goat@192.168.1.253
#
#   # Active drill on Orin-C only (lowest blast radius)
#   ./cold-boot-drill.sh active goat@192.168.1.253
#
# Acceptance:
#   - Each target reports VERDICT: PASS in passive mode
#   - In active mode, each target additionally completes the reboot +
#     comes back via SSH within 5 min + re-verifies PASS

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <passive|active> <ssh-target> [<ssh-target> ...]" >&2
  exit 2
fi

MODE="$1"; shift
case "$MODE" in
  passive|active) ;;
  *) echo "mode must be 'passive' or 'active', got: $MODE" >&2; exit 2 ;;
esac

# verify_one: passive verification on a single target. Returns PASS/FAIL.
verify_one() {
  local target="$1"
  ssh -o ConnectTimeout=10 -o BatchMode=yes "$target" '
    set -e
    HOSTNAME=$(hostname)
    UPTIME=$(uptime -p)

    # Acceptance gate 1: docker-data is mounted via LUKS mapper
    if ! mount | grep -q "/dev/mapper/docker-data on /opt/docker-data"; then
      echo "FAIL: docker-data not mounted on $HOSTNAME"
      exit 1
    fi

    # Acceptance gate 2: systemd-cryptsetup unit is active+success
    UNIT_RESULT=$(sudo systemctl show systemd-cryptsetup@docker\\x2ddata.service \
      --property=Result --value)
    if [[ "$UNIT_RESULT" != "success" ]]; then
      echo "FAIL: systemd-cryptsetup@docker-data Result=$UNIT_RESULT on $HOSTNAME"
      exit 1
    fi

    # Acceptance gate 3: current-boot journal carries the TPM2-unlock line
    if ! sudo journalctl -b 0 --no-pager 2>/dev/null \
        | grep -q "Automatically discovered.*TPM2 token unlocks volume"; then
      echo "FAIL: no TPM2-auto-unlock evidence in current-boot journal on $HOSTNAME"
      exit 1
    fi

    # Acceptance gate 4: tpm2 device present + readable
    if ! sudo tpm2_pcrread sha256:7 >/dev/null 2>&1; then
      echo "FAIL: tpm2_pcrread failed on $HOSTNAME — TPM2 device unhealthy"
      exit 1
    fi

    BOOT_TIME=$(sudo systemctl show systemd-cryptsetup@docker\\x2ddata.service \
      --property=ActiveEnterTimestamp --value)
    echo "PASS: $HOSTNAME ($UPTIME, unlock at $BOOT_TIME)"
  '
}

reboot_one_and_wait() {
  local target="$1"
  echo "  Triggering reboot on $target ..."
  ssh -o ConnectTimeout=10 -o BatchMode=yes "$target" "sudo systemctl reboot" || true
  # Reboot drops the SSH session; that's expected.

  echo "  Waiting for SSH to come back (max 300s) ..."
  local elapsed=0
  while (( elapsed < 300 )); do
    sleep 10
    elapsed=$((elapsed + 10))
    if ssh -o ConnectTimeout=5 -o BatchMode=yes "$target" "true" 2>/dev/null; then
      echo "  SSH reachable again after ~${elapsed}s"
      return 0
    fi
  done
  echo "  TIMEOUT: $target did not come back within 300s — manual intervention required"
  return 1
}

OVERALL_VERDICT=PASS
RESULTS=()

for target in "$@"; do
  echo "=== $target (mode=$MODE) ==="

  if [[ "$MODE" == "active" ]]; then
    # Active mode: reboot first, then verify.
    if ! reboot_one_and_wait "$target"; then
      RESULTS+=("$target: FAIL (reboot/SSH-recovery)")
      OVERALL_VERDICT=FAIL
      echo
      continue
    fi
    # Give services a moment to settle past the cryptsetup unit
    sleep 5
  fi

  # Passive verification (always runs)
  if RESULT=$(verify_one "$target"); then
    echo "$RESULT"
    RESULTS+=("$target: $RESULT")
  else
    echo "$RESULT"
    RESULTS+=("$target: $RESULT")
    OVERALL_VERDICT=FAIL
  fi
  echo
done

echo "================================================================"
echo "OVERALL: $OVERALL_VERDICT"
for r in "${RESULTS[@]}"; do
  echo "  $r"
done

[[ "$OVERALL_VERDICT" == "PASS" ]]
