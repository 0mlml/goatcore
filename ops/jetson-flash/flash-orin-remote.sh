#!/usr/bin/env bash
#
# flash-orin-remote.sh — drive a Jetson AGX Orin flash from this Mac
# via an x86 Ubuntu host on the LAN.
#
# Why two scripts:
#   - flash-jetson-ubuntu.sh runs on the x86 host (it needs to talk
#     to the Jetson over USB and use Linux-only tools like
#     qemu-aarch64-static, chroot, lsusb).
#   - flash-orin-remote.sh runs on the Mac. It pushes the host-side
#     script over SSH, makes sure host prerequisites are installed,
#     and tails the flash output back to the Mac terminal.
#
# Required env vars (set in .envrc.local — gitignored):
#   ORIN3_FLASH_HOST          LAN IP / hostname of the x86 flash host.
#   ORIN3_FLASH_USER          SSH user on the x86 flash host.
#   JETSON_FLASH_USER_PASS    First-boot password for the Orin's
#                             default user. One-time bootstrap secret.
#
# Optional env vars (passed through to flash-jetson-ubuntu.sh):
#   JETSON_FLASH_USER         Default Orin user (notional default
#                             baked into the host script — operator
#                             SHOULD override; see jetson-flash/README.md
#                             "Compliance note — notional defaults").
#   JETSON_FLASH_HOSTNAME     Orin hostname (default: orin-c).
#   JETSON_FLASH_BOARD        L4T board target.
#   JETSON_FLASH_SSH_KEY      SSH pubkey to bake. If unset and the
#                             agent has a key, we use the first agent
#                             key.
#
# Usage:
#   ./ops/jetson-flash/flash-orin-remote.sh
#
# The host-side flash takes 30-45 min (download L4T, apply_binaries,
# pre-bake rootfs, flash.sh). This script blocks until it returns
# and prints all output to your terminal.

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
LOCAL_FLASH_SCRIPT="${SCRIPT_DIR}/flash-jetson-ubuntu.sh"

# ----- Env --------------------------------------------------------------------

require_env() {
    local var="$1"
    if [[ -z "${!var:-}" ]]; then
        echo "error: $var not set. Add it to .envrc.local and 'direnv allow'." >&2
        exit 64
    fi
}

require_env ORIN3_FLASH_HOST
require_env ORIN3_FLASH_USER
# JETSON_FLASH_USER_PASS is optional — host script ships a notional
# default ('goatnet') for unattended demos; production deployments
# MUST override via .envrc.local. See jetson-flash/README.md.

target="${ORIN3_FLASH_USER}@${ORIN3_FLASH_HOST}"

# ----- Optional: pick first agent key if no explicit pubkey -------------------

if [[ -z "${JETSON_FLASH_SSH_KEY:-}" ]] && command -v ssh-add >/dev/null 2>&1; then
    JETSON_FLASH_SSH_KEY="$(ssh-add -L 2>/dev/null | head -1 || true)"
    if [[ -n "$JETSON_FLASH_SSH_KEY" ]]; then
        echo "[remote-flash] using first ssh-agent key: ${JETSON_FLASH_SSH_KEY:0:40}..."
    fi
fi

# ----- Pre-flight on host -----------------------------------------------------

echo "[remote-flash] probing key auth to $target..."
if ! ssh -o BatchMode=yes -o ConnectTimeout=5 "$target" true 2>/dev/null; then
    echo "error: key auth to $target is not set up." >&2
    echo "  run: ops/scripts/seed-ssh-key.sh '$target' <PASSWORD_ENV_VAR>" >&2
    exit 1
fi

echo "[remote-flash] verifying host prereqs (apt install if missing)..."
ssh -o BatchMode=yes "$target" '
    set -e
    missing=()
    for c in wget tar lsusb qemu-aarch64-static curl unzip; do
        command -v "$c" >/dev/null 2>&1 || missing+=("$c")
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "missing on host: ${missing[*]}; installing..."
        sudo apt-get update -qq
        sudo apt-get install -y -qq wget tar usbutils qemu-user-static curl unzip ca-certificates
    fi
    avail=$(df --output=avail -BG "$HOME" | tail -1 | tr -dc 0-9)
    if (( avail < 30 )); then
        echo "error: only ${avail} GB free in \$HOME on host (need 30 GB)." >&2
        exit 1
    fi
    if ! lsusb | grep -qi "NVIDIA Corp. APX"; then
        echo "warning: Jetson not currently in recovery mode (no NVIDIA APX on USB)." >&2
        echo "  you can connect + recovery-mode the Jetson now; the host script will block on a prompt if it is missing at run time." >&2
    fi
'

# ----- Push the host-side script ---------------------------------------------

echo "[remote-flash] copying flash-jetson-ubuntu.sh to host..."
scp -q "$LOCAL_FLASH_SCRIPT" "${target}:flash-jetson-ubuntu.sh"
ssh -o BatchMode=yes "$target" "chmod +x flash-jetson-ubuntu.sh"

# ----- Run flash --------------------------------------------------------------

echo "[remote-flash] starting flash on host (30-45 min, do not interrupt)..."
echo "[remote-flash] output streams below."
echo

# We pass env vars through with `sudo -E`. Pass JETSON_FLASH_* vars
# only if they are set locally — empty/unset means "use script
# default."
remote_env=()
# `:-` is required, not stylistic. The comment at the top of this file says
# JETSON_FLASH_USER_PASS is optional because the host script ships a notional
# default -- but this runs under `set -u`, so a bare ${JETSON_FLASH_USER_PASS}
# aborts with "unbound variable" when it is unset, and the documented optional
# path never worked. Passing it EMPTY is correct: the host script reads it as
# ${JETSON_FLASH_USER_PASS:-goatnet}, and `:-` treats empty the same as unset.
remote_env+=("JETSON_FLASH_USER_PASS=${JETSON_FLASH_USER_PASS:-}")
[[ -n "${JETSON_FLASH_USER:-}" ]]     && remote_env+=("JETSON_FLASH_USER=${JETSON_FLASH_USER}")
[[ -n "${JETSON_FLASH_HOSTNAME:-}" ]] && remote_env+=("JETSON_FLASH_HOSTNAME=${JETSON_FLASH_HOSTNAME}")
[[ -n "${JETSON_FLASH_BOARD:-}" ]]    && remote_env+=("JETSON_FLASH_BOARD=${JETSON_FLASH_BOARD}")
[[ -n "${JETSON_FLASH_SSH_KEY:-}" ]]  && remote_env+=("JETSON_FLASH_SSH_KEY=${JETSON_FLASH_SSH_KEY}")

ssh -o BatchMode=yes "$target" \
    "sudo -E env ${remote_env[*]@Q} bash flash-jetson-ubuntu.sh"

echo
echo "[remote-flash] flash returned. Check Jetson — it should be rebooting into Ubuntu-for-Tegra."
echo "[remote-flash] next: find the Orin's LAN IP, then ops/scripts/seed-ssh-key.sh."
