#!/usr/bin/env bash
set -euo pipefail

# =============================================================
# 03b-headless-dropbear-unlock.sh
# Companion to 03-prepare-encryption.sh — bakes a headless
# remote-unlock path (dropbear-in-initramfs) so an already-
# installed Pi with NO SD / clone / HDMI / keyboard, reachable
# only over SSH, can be encrypted in place and unlocked over
# the network.
#
# 03-prepare-encryption.sh rewrites cmdline.txt to
# cryptdevice=<part>:rootfs. From then on the box halts in the
# initramfs at every boot waiting for the LUKS key. On a
# headless node with no console that is a BRICK unless the
# initramfs can bring the NIC up and run an SSH daemon. This
# script adds that daemon and — critically — REFUSES to leave
# you in a bootable-but-unreachable state: it verifies, with
# lsinitramfs, that the initramfs the bootloader actually loads
# contains dropbear + your authorized_keys + the NIC driver +
# cryptsetup, and exits non-zero if any piece is missing.
#
# Run ORDER (adopt an already-installed Pi in place):
#   1. sudo bash 03-prepare-encryption.sh          # cryptsetup + fstab + cmdline
#   2. sudo ... bash 03b-headless-dropbear-unlock.sh   # THIS — headless unlock layer
#   3. sudo reboot                                 # lands in initramfs; dropbear up
#   4. ssh root@<CIP>                              # from your workstation
#        mount -t vfat /dev/nvme0n1p1 /mnt/boot
#        sh /mnt/boot/helper-scripts/encrypt-rootfs.sh   # passphrase mode
#   5. reencrypt is resumable — a dropped SSH session PAUSES it; reconnect and
#        cryptsetup reencrypt --resume-only /dev/nvme0n1p2
#      Only a power-off mid-reencrypt is dangerous.
#
# Parameters (env vars, all required unless noted):
#   CIP     client IP the Pi should claim in the initramfs   e.g. 192.168.1.51
#   GW      default gateway                                  e.g. 192.168.1.1
#   MASK    dotted netmask                                   e.g. 255.255.255.0
#   PUBKEY  operator SSH public key (the full line, or a path to a .pub file)
#   NIC     initramfs network interface   (default: eth0 — Pi 5 onboard NIC)
#   NICDRV  its kernel driver module      (default: macb — Pi 5 onboard NIC)
#
# Usage:
#   sudo CIP=192.168.1.51 GW=192.168.1.1 MASK=255.255.255.0 \
#        PUBKEY="$(cat ~/.ssh/id_ed25519.pub)" \
#        bash 03b-headless-dropbear-unlock.sh
# =============================================================

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; WHITE='\033[1;37m'
BOLD='\033[1m'; NC='\033[0m'

info()  { printf "${CYAN}>${NC} %s\n" "$*"; }
step()  { printf "\n${BOLD}${BLUE}=== %s ===${NC}\n" "$*"; }
ok()    { printf "${GREEN}[ok]${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}[warn]${NC} %s\n" "$*" >&2; }
error() { printf "${RED}[error]${NC} %s\n" "$*" >&2; }

banner() {
    echo ""
    printf "${BLUE}${BOLD}  ┌──────────────────────────────────┐${NC}\n"
    printf "${BLUE}${BOLD}  │  ${WHITE}ODT ${NC}${BLUE}${BOLD}│${NC} ${GREEN}%-24s${BLUE}${BOLD}  │${NC}\n" "$1"
    printf "${BLUE}${BOLD}  └──────────────────────────────────┘${NC}\n\n"
}

if [ "$(id -u)" -ne 0 ]; then
    error "Run as root (sudo ... bash 03b-headless-dropbear-unlock.sh)"
    exit 1
fi

banner "Headless Remote-Unlock Prep"

# ---- Parameters ------------------------------------------------------------
NIC="${NIC:-eth0}"
NICDRV="${NICDRV:-macb}"
CIP="${CIP:-}"
GW="${GW:-}"
MASK="${MASK:-}"
PUBKEY="${PUBKEY:-}"

missing=0
for v in CIP GW MASK PUBKEY; do
    if [ -z "${!v}" ]; then
        error "Required parameter ${v} is not set."
        missing=1
    fi
done
if [ "$missing" -ne 0 ]; then
    echo ""
    echo "  Example:"
    printf "    ${CYAN}sudo CIP=192.168.1.51 GW=192.168.1.1 MASK=255.255.255.0 \\\\${NC}\n"
    printf "    ${CYAN}     PUBKEY=\"\$(cat ~/.ssh/id_ed25519.pub)\" \\\\${NC}\n"
    printf "    ${CYAN}     bash 03b-headless-dropbear-unlock.sh${NC}\n"
    exit 1
fi

# PUBKEY may be a path to a .pub file or the key line itself.
if [ -f "$PUBKEY" ]; then
    PUBKEY="$(cat "$PUBKEY")"
fi
case "$PUBKEY" in
    ssh-ed25519\ *|ssh-rsa\ *|ecdsa-sha2-*\ *|sk-ssh-*\ *)
        : ;;
    *)
        error "PUBKEY does not look like an SSH public key line: '${PUBKEY:0:40}...'"
        exit 1 ;;
esac

# ---- Preflight: 03 must have run first -------------------------------------
step "Preflight: confirm 03-prepare-encryption.sh has run"
CMDLINE="/boot/firmware/cmdline.txt"
[ -f "$CMDLINE" ] || CMDLINE="/boot/cmdline.txt"
if ! grep -q 'cryptdevice=' "$CMDLINE"; then
    error "No 'cryptdevice=' in ${CMDLINE}. Run 03-prepare-encryption.sh FIRST."
    exit 1
fi
ok "cmdline.txt carries cryptdevice= ($(grep -o 'cryptdevice=[^ ]*' "$CMDLINE"))"

# ---- Phase 1: dropbear-initramfs + operator key ----------------------------
step "Phase 1: Install dropbear-initramfs + seed operator key"
info "Installing dropbear-initramfs..."
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq dropbear-initramfs

DROPBEAR_DIR="/etc/dropbear/initramfs"
mkdir -p "$DROPBEAR_DIR"
AKEYS="${DROPBEAR_DIR}/authorized_keys"
# The initramfs dropbear logs in as root; the key must be present here, not in
# the operator's ~/.ssh. Write it fresh (single-purpose, unlock-only key set).
printf '%s\n' "$PUBKEY" > "$AKEYS"
chmod 600 "$AKEYS"
ok "operator key written to ${AKEYS}"

# Restrict dropbear to key-only, no port-forwarding (optional hardening).
DBCONF="${DROPBEAR_DIR}/dropbear.conf"
if [ ! -f "$DBCONF" ] || ! grep -q -- '-j -k' "$DBCONF" 2>/dev/null; then
    echo 'DROPBEAR_OPTIONS="-j -k -p 22 -s"' > "$DBCONF"
fi
ok "dropbear.conf set (no port-forwarding, key-only)"

# ---- Phase 2: static initramfs networking ----------------------------------
step "Phase 2: Static initramfs networking (IP= + NIC driver)"
ICONF="/etc/initramfs-tools/initramfs.conf"
# Kernel ip= form: client::gateway:netmask::device:autoconf
IPLINE="IP=${CIP}::${GW}:${MASK}::${NIC}:off"
sed -i '/^IP=/d' "$ICONF"
echo "$IPLINE" >> "$ICONF"
ok "initramfs.conf IP line: ${IPLINE}"

MODFILE="/etc/initramfs-tools/modules"
for mod in "$NICDRV" dm-crypt; do
    grep -q "^${mod}$" "$MODFILE" 2>/dev/null || echo "$mod" >> "$MODFILE"
done
ok "initramfs modules ensured: ${NICDRV} (NIC driver) + dm-crypt"

# ---- Phase 3: rebuild initramfs --------------------------------------------
step "Phase 3: Rebuild initramfs"
update-initramfs -u
ok "initramfs rebuilt"

# ---- Phase 4: FAIL-CLOSED verify (the safety gate) -------------------------
# 03 already rewrote cmdline to cryptdevice=. A reboot now with a broken
# dropbear strands the box in an unreachable initramfs = a brick on a headless
# node. Verify the initramfs the BOOTLOADER loads (not just /boot/initrd.img)
# contains every piece, and REFUSE to declare success if any is missing.
step "Phase 4: Fail-closed verify of the LOADED initramfs"

# config.txt names the initramfs the firmware loads (initramfs <name> followkernel).
CONFIGTXT="/boot/firmware/config.txt"
[ -f "$CONFIGTXT" ] || CONFIGTXT="/boot/config.txt"
INITRD_NAME="$(grep -oE '^[[:space:]]*initramfs[[:space:]]+[^[:space:]]+' "$CONFIGTXT" 2>/dev/null | awk '{print $2}' | tail -1)"
INITRD_NAME="${INITRD_NAME:-initrd.img}"
BOOTDIR="$(dirname "$CONFIGTXT")"
LOADED_INITRD="${BOOTDIR}/${INITRD_NAME}"

info "Bootloader loads: ${LOADED_INITRD} (per '${CONFIGTXT}')"
if [ ! -f "$LOADED_INITRD" ]; then
    error "The initramfs the bootloader loads (${LOADED_INITRD}) does not exist."
    error "DO NOT REBOOT — the box would strand in an unreachable initramfs."
    exit 1
fi

# If update-initramfs wrote /boot/initrd.img but the firmware copy is older, the
# loaded file will lack these bits. That is exactly the case this catches.
LIST="$(lsinitramfs "$LOADED_INITRD" 2>/dev/null || true)"

check_component() {
    # $1 = human name, $2.. = grep -E patterns (any match passes)
    local name="$1"; shift
    local pat
    for pat in "$@"; do
        if printf '%s\n' "$LIST" | grep -qE "$pat"; then
            ok "present: ${name}"
            return 0
        fi
    done
    error "MISSING from ${LOADED_INITRD}: ${name}"
    return 1
}

fail=0
check_component "dropbear daemon"        'sbin/dropbear$'                      || fail=1
check_component "authorized_keys"        'dropbear/initramfs/authorized_keys$' 'root/.ssh/authorized_keys$' 'authorized_keys$' || fail=1
check_component "NIC driver (${NICDRV})" "/${NICDRV}\.ko"                      || fail=1
check_component "cryptsetup"             'sbin/cryptsetup$'                    || fail=1

if [ "$fail" -ne 0 ]; then
    echo ""
    error "Fail-closed verify FAILED. The loaded initramfs is not safe to boot."
    error "Refusing to proceed. cmdline.txt already carries cryptdevice=, so a"
    error "reboot now could brick this headless box. Investigate before rebooting:"
    echo ""
    echo "    lsinitramfs ${LOADED_INITRD} | grep -E 'dropbear|${NICDRV}|cryptsetup|authorized_keys'"
    echo ""
    echo "  Common cause: update-initramfs wrote /boot/initrd.img but the firmware"
    echo "  copy at ${LOADED_INITRD} is stale. Re-run update-initramfs -u and confirm"
    echo "  the firmware copy's mtime advanced, then re-run this script."
    exit 1
fi

printf "\n${GREEN}${BOLD}=========================================${NC}\n"
printf "${GREEN}${BOLD}  Headless unlock verified. Safe to reboot.${NC}\n"
echo ""
echo "  On this box:"
printf "    ${CYAN}sudo reboot${NC}\n"
echo ""
echo "  It will halt in the initramfs with dropbear listening on ${CIP}:22."
echo "  From your workstation:"
printf "    ${CYAN}ssh root@${CIP}${NC}\n"
printf "    ${CYAN}mount -t vfat /dev/nvme0n1p1 /mnt/boot${NC}\n"
printf "    ${CYAN}sh /mnt/boot/helper-scripts/encrypt-rootfs.sh${NC}   # passphrase mode\n"
echo ""
echo "  The reencrypt is RESUMABLE. A dropped SSH session only PAUSES it —"
echo "  reconnect and run:"
printf "    ${CYAN}cryptsetup reencrypt --resume-only /dev/nvme0n1p2${NC}\n"
echo "  Only a power-off mid-reencrypt is dangerous."
echo ""
echo "  Time: cryptsetup reencrypt XTS-encrypts the WHOLE partition. Measured"
echo "  ~150 MiB/s at PCIe Gen 2 on a 1.8 TB NVMe ~ 3.4 h — time scales with"
echo "  partition size, NOT the ~40 min a small drive implies. See the README"
echo "  'Adopt an already-installed Pi headlessly' section for the Gen-3 lever."
printf "${GREEN}${BOLD}=========================================${NC}\n"
