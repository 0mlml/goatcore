#!/bin/bash
# Block 65A.1 — tpm2 keyslot enrollment helper for systemd-cryptenroll 249.
#
# Reads the existing LUKS passphrase from stdin (1 line), forks
# systemd-cryptenroll, waits for the systemd ask-password request file
# to appear, sends the passphrase to the answer socket, waits for
# cryptenroll to complete, prints final slot table.
#
# Why this dance: systemd-cryptenroll 249 (Ubuntu 22.04 / JetPack 6.1)
# uses the systemd ask-password mechanism, NOT stdin. Piping the
# passphrase into ssh stdin doesn't reach cryptenroll. The
# documented `--unlock-key-file=` flag was added in systemd 251.
#
# See F-054 + ADR 0612 + Block 65A.1 in implementation-plan.md.
set -euo pipefail

LUKS_DEV="${LUKS_DEV:-/dev/nvme0n1}"
PCRS="${PCRS:-0+2+4+7+11}"
TIMEOUT_S="${TIMEOUT_S:-30}"

read -r PASSPHRASE  # one line from stdin
[ -n "$PASSPHRASE" ] || { echo "ERROR: empty passphrase on stdin"; exit 2; }

echo "=== Pre-state slot table ($LUKS_DEV) ==="
sudo systemd-cryptenroll "$LUKS_DEV"

echo "=== Launching cryptenroll (tpm2, PCRs $PCRS) ==="
sudo systemd-cryptenroll --tpm2-device=auto --tpm2-pcrs="$PCRS" "$LUKS_DEV" 2>&1 &
CRYPTENROLL_PID=$!

# Wait for ask-password request to appear (up to TIMEOUT_S)
for _i in $(seq 1 "$TIMEOUT_S"); do
    ASK=$(sudo find /run/systemd/ask-password/ -name 'ask.*' -mmin -1 2>/dev/null | head -1 || true)
    if [ -n "$ASK" ]; then
        SOCK=$(sudo grep '^Socket=' "$ASK" | cut -d= -f2)
        if [ -n "$SOCK" ]; then
            echo "=== Found ask-password request (sock=$SOCK) — sending passphrase ==="
            printf '+%s' "$PASSPHRASE" | sudo python3 -c "
import sys, socket
s = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
s.sendto(sys.stdin.buffer.read(), '$SOCK')
"
            break
        fi
    fi
    sleep 1
done

echo "=== Waiting for cryptenroll to complete ==="
if wait $CRYPTENROLL_PID; then
    echo "=== Enrollment SUCCESS ==="
else
    echo "=== Enrollment FAILED (exit=$?) ==="
    exit 1
fi

echo "=== Post-state slot table ==="
sudo systemd-cryptenroll "$LUKS_DEV"
echo "=== luksDump tokens (should show systemd-tpm2) ==="
sudo cryptsetup luksDump "$LUKS_DEV" | sed -n '/^Tokens:/,/^Digests:/p'
