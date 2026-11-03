#!/usr/bin/env bash
set -euo pipefail

# =============================================================
# 00-prerequisites.sh
# Run ON YOUR MAC before starting the golden image build.
# Installs required tools and downloads the Ubuntu image.
#
# Usage: ./00-prerequisites.sh
# =============================================================

# --- Colors & helpers ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
BOLD='\033[1m'
DIM='\033[2m'
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

banner "Prerequisites Setup"

# --- Check macOS ---
if [[ "$(uname)" != "Darwin" ]]; then
    error "This script is for macOS only."
    exit 1
fi

# --- Check Homebrew ---
step "Step 1: Checking Homebrew"
if ! command -v brew &>/dev/null; then
    warn "Homebrew not found. Installing..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    ok "Homebrew installed"
else
    ok "Homebrew found"
fi

# --- Install tools ---
step "Step 2: Installing required tools"

TOOLS=(pv nmap ansible xz)
for tool in "${TOOLS[@]}"; do
    if command -v "$tool" &>/dev/null; then
        ok "$tool already installed"
    else
        info "Installing $tool..."
        brew install "$tool"
        ok "$tool installed"
    fi
done

# --- Ansible collections ---
step "Step 3: Ansible collections"
if ansible-galaxy collection list 2>/dev/null | grep -q "community.general"; then
    ok "community.general already installed"
else
    info "Installing community.general..."
    ansible-galaxy collection install community.general
    ok "community.general installed"
fi

# --- Download Ubuntu image ---
step "Step 4: Ubuntu Server image"

# Image cache resolution order:
#   1. $IMAGES_DIR env var if set (full override)
#   2. <repo-parent>/artifacts/images   (the standard SAUDI-ARABIA layout)
#   3. <repo-parent>/images             (legacy, pre-artifacts/ consolidation)
#   4. ~/Downloads                      (final fallback for fresh machines)
SCRIPT_PARENT="$(cd "$(dirname "$0")/.." && pwd)"
if   [ -n "${IMAGES_DIR:-}" ];                   then DOWNLOAD_DIR="$IMAGES_DIR"
elif [ -d "${SCRIPT_PARENT}/artifacts/images" ]; then DOWNLOAD_DIR="${SCRIPT_PARENT}/artifacts/images"
elif [ -d "${SCRIPT_PARENT}/images" ];           then DOWNLOAD_DIR="${SCRIPT_PARENT}/images"
else                                                  DOWNLOAD_DIR="$HOME/Downloads"
fi
ok "Image cache directory: $DOWNLOAD_DIR"

IMAGE_PATTERN="ubuntu-*-preinstalled-server-arm64+raspi.img.xz"
# Look in DOWNLOAD_DIR first, then in ~/Downloads as a fallback for legacy users
EXISTING=$(find "$DOWNLOAD_DIR" "$HOME/Downloads" -maxdepth 1 -name "$IMAGE_PATTERN" -type f 2>/dev/null | head -1)

if [ -n "$EXISTING" ]; then
    ok "Found image: $(basename "$EXISTING")"
    IMAGE_PATH="$EXISTING"
else
    info "Downloading Ubuntu Server 24.04 ARM64 preinstalled image..."
    IMAGE_URL="https://cdimage.ubuntu.com/releases/24.04/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
    IMAGE_PATH="$DOWNLOAD_DIR/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"

    if command -v curl &>/dev/null; then
        curl -L --progress-bar -o "$IMAGE_PATH" "$IMAGE_URL"
    else
        error "curl not found. Download manually from:"
        echo "  https://ubuntu.com/download/raspberry-pi"
        exit 1
    fi

    if [ -f "$IMAGE_PATH" ]; then
        ok "Downloaded: $(basename "$IMAGE_PATH")"
    else
        error "Download failed. Download manually from:"
        echo "  https://ubuntu.com/download/raspberry-pi"
        exit 1
    fi
fi

# --- Check SSH key ---
step "Step 5: SSH key"
if [ -f "$HOME/.ssh/id_ed25519.pub" ]; then
    ok "SSH key found: ~/.ssh/id_ed25519.pub"
    echo ""
    info "Your public key:"
    cat "$HOME/.ssh/id_ed25519.pub"
else
    warn "No ed25519 SSH key found."
    read -rp "Generate one now? (y/n): " GENERATE
    if [ "$GENERATE" = "y" ]; then
        ssh-keygen -t ed25519 -f "$HOME/.ssh/id_ed25519" -N ""
        ok "SSH key generated"
        echo ""
        info "Your public key:"
        cat "$HOME/.ssh/id_ed25519.pub"
    else
        warn "You'll need an SSH key before flashing. Generate one with:"
        echo "  ssh-keygen -t ed25519"
    fi
fi

# --- Remind to edit configs ---
step "Step 6: Configure cloud-init"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
USER_DATA="$SCRIPT_DIR/cloud-init-configs/user-data"

if [ -f "$USER_DATA" ] && grep -q "REPLACE_WITH_YOUR_PUBLIC_KEY" "$USER_DATA"; then
    warn "user-data still has placeholder SSH key."
    echo ""
    read -rp "Auto-configure user-data with your SSH key? (y/n): " AUTOCONFIG
    if [ "$AUTOCONFIG" = "y" ] && [ -f "$HOME/.ssh/id_ed25519.pub" ]; then
        PUB_KEY=$(cat "$HOME/.ssh/id_ed25519.pub")
        sed -i '' "s|REPLACE_WITH_YOUR_PUBLIC_KEY|${PUB_KEY}|g" "$USER_DATA"
        ok "user-data updated with your SSH key"
    else
        info "Edit manually before flashing:"
        printf "  ${CYAN}nano cloud-init-configs/user-data${NC}\n"
    fi
else
    ok "user-data already configured"
fi

# --- Summary ---
printf "\n${GREEN}${BOLD}=========================================${NC}\n"
printf "${GREEN}${BOLD}  Prerequisites complete!${NC}\n"
echo ""
echo "  Tools installed: pv, nmap, ansible, xz"
echo "  Image: $(basename "${IMAGE_PATH:-not found}")"
echo ""
echo "  Next step:"
printf "    ${CYAN}./01-flash.sh /dev/diskN ${IMAGE_PATH:-~/Downloads/ubuntu-*.img.xz}${NC}\n"
echo ""
echo "  Find your SD card with:"
printf "    ${CYAN}diskutil list${NC}\n"
printf "${GREEN}${BOLD}=========================================${NC}\n"
