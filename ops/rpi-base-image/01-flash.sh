#!/usr/bin/env bash
set -euo pipefail

# =============================================================
# Flash Ubuntu + cloud-init to SD card for the Pi 5 base image
# =============================================================
# Usage:
#   ./01-flash.sh /dev/diskN ~/Downloads/ubuntu-24.04-preinstalled-server-arm64+raspi.img.xz
#
# Prerequisites:
#   - Ubuntu Server 24.04 ARM64 preinstalled image (for Raspberry Pi)
#     Download: https://ubuntu.com/download/raspberry-pi
#   - SD card inserted
#   - Your SSH public key in user-data (edit before running)
#   - pv installed (brew install pv) — for progress bar
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
        error "Script failed. SD card may be in an inconsistent state."
    fi
}
trap cleanup EXIT

# --- Args ---
DISK="${1:-}"
IMAGE="${2:-}"

if [ -z "$DISK" ] || [ -z "$IMAGE" ]; then
    echo "Usage: $0 /dev/diskN /path/to/ubuntu-image.img.xz"
    echo ""
    echo "Find your SD card with: diskutil list"
    exit 1
fi

banner "Base Image SD Card Flash"

# Validate disk path
if [[ ! "$DISK" =~ ^/dev/disk[0-9]+$ ]]; then
    error "Invalid disk path: $DISK (expected /dev/diskN)"
    exit 1
fi

# Validate image exists
if [ ! -f "$IMAGE" ]; then
    error "Image not found: $IMAGE"
    exit 1
fi

# Safety check
printf "${YELLOW}${BOLD}WARNING:${NC} This will erase ${BOLD}%s${NC}\n" "$DISK"
echo "Press Ctrl+C to cancel, Enter to continue..."
read -r

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
USER_DATA="$SCRIPT_DIR/cloud-init-configs/user-data"

if [ ! -f "$USER_DATA" ]; then
    error "user-data not found at $USER_DATA"
    exit 1
fi

# Check SSH key is configured
if grep -q "REPLACE_WITH_YOUR_PUBLIC_KEY" "$USER_DATA"; then
    error "Edit user-data and replace the SSH key placeholder with your actual public key"
    info "Your key: $(cat ~/.ssh/id_ed25519.pub 2>/dev/null || echo 'not found')"
    exit 1
fi

step "Step 1: Unmounting $DISK"
diskutil unmountDisk "$DISK"
ok "Unmounted"

step "Step 2: Flashing image to $DISK"
RAW_DISK="/dev/r${DISK#/dev/}"

# Cache sudo credentials before starting the pipe so the password
# prompt doesn't get buried under pv output
sudo -v

if ! command -v pv &>/dev/null; then
    error "pv not found. Install it: brew install pv"
    exit 1
fi

if [[ "$IMAGE" == *.xz ]]; then
    # Get decompressed size for progress, decompress and write
    IMG_SIZE=$(xz --robot --list "$IMAGE" 2>/dev/null | awk '/^totals/{print $5}')
    if [ -n "$IMG_SIZE" ] && [ "$IMG_SIZE" -gt 0 ] 2>/dev/null; then
        info "Decompressing and writing..."
        xz -dc "$IMAGE" | pv -s "$IMG_SIZE" -petra | sudo dd of="$RAW_DISK" bs=4m
    else
        info "Decompressing and writing..."
        xz -dc "$IMAGE" | pv -petra | sudo dd of="$RAW_DISK" bs=4m
    fi
elif [[ "$IMAGE" == *.img ]]; then
    IMG_SIZE=$(stat -f%z "$IMAGE")
    info "Writing image..."
    pv -s "$IMG_SIZE" -petra "$IMAGE" | sudo dd of="$RAW_DISK" bs=4m
else
    error "Image must be .img or .img.xz"
    exit 1
fi
ok "Flash complete"

step "Step 3: Waiting for partitions to mount"
sleep 3
diskutil mountDisk "$DISK"

# Find the boot partition (system-boot)
BOOT_MOUNT=""
for mount_point in /Volumes/system-boot /Volumes/BOOT /Volumes/boot-firmware; do
    if [ -d "$mount_point" ]; then
        BOOT_MOUNT="$mount_point"
        break
    fi
done

if [ -z "$BOOT_MOUNT" ]; then
    error "Could not find boot partition mount point"
    info "Check: ls /Volumes/"
    exit 1
fi
ok "Boot partition: $BOOT_MOUNT"

step "Step 4: Copying configs to $BOOT_MOUNT"
cp "$USER_DATA" "$BOOT_MOUNT/user-data"
ok "Copied user-data"

NETWORK_CONFIG="$SCRIPT_DIR/cloud-init-configs/network-config"
if [ -f "$NETWORK_CONFIG" ]; then
    cp "$NETWORK_CONFIG" "$BOOT_MOUNT/network-config"
    ok "Copied network-config"
fi

# Copy encryption helper scripts to boot partition (for initramfs use)
HELPER_DIR="$SCRIPT_DIR/helper-scripts"
for script in encrypt-rootfs.sh encrypt-rootfs-unlock.sh; do
    if [ -f "$HELPER_DIR/$script" ]; then
        cp "$HELPER_DIR/$script" "$BOOT_MOUNT/$script"
        ok "Copied $script"
    fi
done

# Copy deployment scripts to boot partition (avoids SCP later)
for script in 02-clone-to-nvme.sh 03-prepare-encryption.sh 03-prepare-encryption.sh 04-post-encryption-setup.sh 04-enroll-yubikey.sh 05-enable-fips.sh; do
    if [ -f "$SCRIPT_DIR/$script" ]; then
        cp "$SCRIPT_DIR/$script" "$BOOT_MOUNT/$script"
        ok "Copied $script"
    fi
done

step "Step 5: Ejecting $DISK"
diskutil eject "$DISK"
ok "Ejected"

printf "\n${GREEN}${BOLD}=== Done ===${NC}\n"
echo "1. Insert SD card into Pi and boot"
echo "2. Wait 3-5 minutes for cloud-init to complete"
echo "3. Find Pi IP: nmap -sn 192.168.x.0/24 (or check router)"
echo "4. SSH in: ssh goatnet@<PI_IP>  (or ubuntu@<PI_IP> as fallback)"
echo ""
echo "If deploying to NVMe (recommended for Pi 5):"
printf "5. ${CYAN}sudo bash 02-clone-to-nvme.sh${NC}\n"
echo "   Then remove SD card and boot from NVMe."
echo ""
echo "Then continue with encryption:"
printf "6. ${CYAN}sudo bash 03-prepare-encryption.sh${NC}\n"
echo ""
echo "See README.md for full steps."