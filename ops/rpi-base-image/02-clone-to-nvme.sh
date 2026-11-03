#!/usr/bin/env bash
set -euo pipefail

# =============================================================
# 02-clone-to-nvme.sh
# Run ON THE PI after first boot from SD card.
# Clones the SD card to NVMe, fixes UUIDs, sets boot order,
# and prepares the Pi to boot from NVMe.
#
# Prerequisites:
#   - Pi 5 with NVMe HAT/base board and NVMe installed
#   - Booted from SD card (01-flash.sh completed)
#   - Cloud-init finished (wait 3-5 min after first boot)
#
# Usage: sudo bash 02-clone-to-nvme.sh
# =============================================================

# --- Colors & helpers ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
BOLD='\033[1m'
NC='\033[0m'

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

# --- Cleanup on failure ---
cleanup() {
    if [ $? -ne 0 ]; then
        mountpoint -q /mnt 2>/dev/null && umount /mnt
        error "Script failed. Check the output above for details."
    fi
}
trap cleanup EXIT

# --- Root check ---
if [ "$(id -u)" -ne 0 ]; then
    error "Run as root (sudo bash 02-clone-to-nvme.sh)"
    exit 1
fi

banner "SD → NVMe Clone"

# --- Verify we're booted from SD ---
ROOT_DEV=$(findmnt -n -o SOURCE /)
if [[ "$ROOT_DEV" == *"nvme"* ]]; then
    error "Already booted from NVMe. This script clones SD → NVMe."
    exit 1
fi
ok "Booted from SD card (${ROOT_DEV})"

# --- Verify NVMe is detected ---
step "Step 1/7: Detecting NVMe"

if [ ! -b /dev/nvme0n1 ]; then
    error "NVMe drive not detected (/dev/nvme0n1 not found)."
    echo ""
    echo "  Troubleshooting:"
    echo "    - Shut down, reseat NVMe in HAT, boot again"
    echo "    - NVMe is only detected at boot (no hot-plug)"
    echo "    - Check: lsblk"
    exit 1
fi

NVME_SIZE=$(lsblk -rno SIZE /dev/nvme0n1 | head -1)
ok "NVMe detected: /dev/nvme0n1 (${NVME_SIZE})"

# --- Fix NVMe power saving ---
step "Step 2/7: Fixing NVMe power saving"

CMDLINE="/boot/firmware/cmdline.txt"
if [ ! -f "$CMDLINE" ]; then
    CMDLINE="/boot/cmdline.txt"
fi

if [ ! -f "$CMDLINE" ]; then
    error "cmdline.txt not found"
    exit 1
fi

NVME_FIX="nvme_core.default_ps_max_latency_us=0 pcie_aspm=off pcie_port_pm=off nvme_core.io_timeout=255"
if grep -q "nvme_core.default_ps_max_latency_us" "$CMDLINE"; then
    ok "NVMe power saving fix already applied"
else
    # Append to existing line (cmdline.txt must be a single line)
    sed -i "s|$| ${NVME_FIX}|" "$CMDLINE"
    ok "Added NVMe power saving fix to cmdline.txt"
    warn "Rebooting to apply kernel parameters..."
    echo ""
    echo "  After reboot, run this script again:"
    printf "    ${CYAN}sudo bash 02-clone-to-nvme.sh${NC}\n"
    reboot
    exit 0
fi

# --- Check for existing clone or wipe ---
step "Step 3/7: Preparing NVMe"

NVME_PARTS=$(lsblk -rno NAME /dev/nvme0n1 | grep -c 'nvme0n1p' || true)
SKIP_CLONE=false

if [ "$NVME_PARTS" -gt 0 ]; then
    printf "${YELLOW}NVMe has ${NVME_PARTS} existing partition(s).${NC}\n"
    echo ""
    echo "  1) Wipe and re-clone from SD (fresh start)"
    echo "  2) Skip clone, just expand + fix UUID + set boot order"
    echo ""
    read -rp "  Choose [1/2]: " CLONE_CHOICE
    case "$CLONE_CHOICE" in
        2)
            SKIP_CLONE=true
            ok "Skipping clone — will expand and configure existing NVMe"
            ;;
        1)
            wipefs -a /dev/nvme0n1
            ok "NVMe wiped"
            ;;
        *)
            error "Invalid choice. Aborted."
            exit 1
            ;;
    esac
else
    ok "NVMe is empty"
fi

# --- Clone SD to NVMe ---
if [ "$SKIP_CLONE" = false ]; then
    step "Step 4/7: Cloning SD to NVMe"

    SD_SIZE=$(lsblk -rno SIZE /dev/mmcblk0 | head -1)
    info "Cloning /dev/mmcblk0 (${SD_SIZE}) → /dev/nvme0n1 (${NVME_SIZE})"
    echo "  This takes a few minutes..."
    echo ""

    dd bs=4M if=/dev/mmcblk0 of=/dev/nvme0n1 status=progress

    ok "Clone complete"
else
    step "Step 4/7: Cloning SD to NVMe"
    ok "Skipped (using existing clone)"
fi

# --- Expand NVMe partition ---
step "Step 5/7: Expanding NVMe partition"

info "Running filesystem check..."
# e2fsck returns 1 if it fixed errors — that's OK
e2fsck -fy /dev/nvme0n1p2 || {
    RC=$?
    if [ "$RC" -le 1 ]; then
        ok "Filesystem check complete (errors fixed)"
    else
        error "Filesystem check failed with code ${RC}"
        exit 1
    fi
}

info "Expanding partition to use full NVMe..."
parted /dev/nvme0n1 resizepart 2 100% || true

info "Expanding filesystem..."
resize2fs /dev/nvme0n1p2

ok "NVMe partition expanded"

# --- Fix UUID and fstab ---
step "Step 6/7: Fixing UUID and fstab"

info "Generating new UUID for NVMe root partition..."
echo "y" | tune2fs -U random /dev/nvme0n1p2

NEW_UUID=$(blkid -s UUID -o value /dev/nvme0n1p2)
ok "New UUID: ${NEW_UUID}"

info "Updating fstab on NVMe..."
mount /dev/nvme0n1p2 /mnt

# Replace LABEL=writable or old UUID with new UUID
if grep -q 'LABEL=writable' /mnt/etc/fstab; then
    sed -i "s|LABEL=writable.*|UUID=${NEW_UUID} / ext4 defaults 0 1|" /mnt/etc/fstab
elif grep -q "UUID=.*/" /mnt/etc/fstab; then
    OLD_UUID=$(blkid -s UUID -o value /dev/mmcblk0p2 2>/dev/null || echo "none")
    sed -i "s|UUID=${OLD_UUID}|UUID=${NEW_UUID}|" /mnt/etc/fstab
fi

# Show result
info "fstab contents:"
cat /mnt/etc/fstab

# Also copy the NVMe power saving fix to the NVMe's cmdline.txt
NVME_CMDLINE="/mnt/boot/firmware/cmdline.txt"
if [ -f "$NVME_CMDLINE" ]; then
    if ! grep -q "nvme_core.default_ps_max_latency_us" "$NVME_CMDLINE"; then
        sed -i "s|$| ${NVME_FIX}|" "$NVME_CMDLINE"
        ok "NVMe power saving fix copied to NVMe boot partition"
    fi
fi

ok "fstab updated on NVMe"
umount /mnt

# --- Set boot order ---
step "Step 7/7: Setting NVMe boot order"

# Install rpi-eeprom if not present
if ! command -v rpi-eeprom-config &>/dev/null; then
    info "Installing rpi-eeprom..."
    apt-get update -qq
    apt-get install -y -qq rpi-eeprom
fi

# Read current boot order
CURRENT_BOOT_ORDER=$(rpi-eeprom-config 2>/dev/null | grep '^BOOT_ORDER=' | cut -d= -f2 || echo "unknown")
info "Current boot order: ${CURRENT_BOOT_ORDER}"

if [ "$CURRENT_BOOT_ORDER" = "0xf416" ]; then
    ok "Boot order already set to NVMe first"
else
    # Set NVMe first (6=NVMe, 4=USB, 1=SD)
    TMPCONF=$(mktemp)
    rpi-eeprom-config > "$TMPCONF"
    sed -i 's/^BOOT_ORDER=.*/BOOT_ORDER=0xf416/' "$TMPCONF"
    if ! grep -q '^BOOT_ORDER=' "$TMPCONF"; then
        echo "BOOT_ORDER=0xf416" >> "$TMPCONF"
    fi
    rpi-eeprom-config --apply "$TMPCONF"
    rm -f "$TMPCONF"
    ok "Boot order set to NVMe first (0xf416)"
fi

# --- Done ---
printf "\n${GREEN}${BOLD}=========================================${NC}\n"
printf "${GREEN}${BOLD}  NVMe clone complete.${NC}\n"
echo ""
echo "  NVMe: /dev/nvme0n1 (${NVME_SIZE})"
echo "  UUID: ${NEW_UUID}"
echo "  Boot: NVMe first (0xf416)"
echo ""
echo "  Next steps:"
printf "    1. ${CYAN}sudo shutdown -h now${NC}\n"
echo "    2. Remove the SD card"
echo "    3. Power on — Pi boots from NVMe"
echo "    4. SSH in and verify:"
printf "       ${CYAN}lsblk${NC}  (root on /dev/nvme0n1p2)\n"
printf "       ${CYAN}df -h /${NC}  (full NVMe capacity)\n"
echo ""
echo "    5. Continue with encryption:"
printf "       ${CYAN}sudo bash 03-prepare-encryption.sh${NC}\n"
printf "${GREEN}${BOLD}=========================================${NC}\n"
