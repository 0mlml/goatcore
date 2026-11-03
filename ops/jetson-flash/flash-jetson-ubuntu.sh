#!/usr/bin/env bash
#
# flash-jetson-ubuntu.sh
#
# Flash stock Ubuntu-for-Tegra 22.04 (NVIDIA L4T R36.4.x) onto an
# NVIDIA Jetson AGX Orin, with a pre-baked rootfs (Docker, netbird,
# rathole, system + network tools). Run on an x86 Ubuntu host with
# the Orin connected via USB-C in recovery mode.
#
# Lands the Orin at the same baseline described in
# docs/operations/pilot-bringup.md §1, plus pre-baked tools so the
# post-flash Ansible chain has less to install. Bootstrap continues
# at pilot-bringup.md §2 (ssh-copy-id) -> mgmt-host-baseline.yml ->
# mgmt-host-luks.yml -> per-site bootstrap.
#
# Project posture (vs upstream NVIDIA L4T flow):
#   - No Kubernetes tooling. Project rejects k8s/k3s/swarm/nomad in
#     favour of Docker Compose (CLAUDE.md "Container orchestration"),
#     so kubectl/k9s/helm/kubectx/kubens are NOT baked.
#   - No cloudflared. Project does not use Cloudflare Tunnel/WARP
#     (CLAUDE.md "Cloudflare Tunnel / WARP not used").
#   - Default user password is REQUIRED via env var, never a
#     plaintext default in this file (CLAUDE.md "No plaintext secrets
#     in committed files").
#
# Notional defaults (compliance note):
#   The default user and password baked in below are NOTIONAL — they
#   are placeholders that let an unattended flash succeed end-to-end
#   if the operator does not override them. Real deployments MUST
#   override both via env var (set in .envrc.local, not committed)
#   and rotate the password immediately post-flash via the
#   ops/scripts/seed-ssh-key.sh chain. Treat the defaults below the
#   same way you'd treat raspbian's 'pi/raspberry' — known-publicly,
#   never to be used in production.
#
# Optional env vars:
#   JETSON_FLASH_USER_PASS   First-boot password for the default user.
#                            Default: 'goatnet' (notional — see above).
#   JETSON_FLASH_USER        Default user. Default: 'goatnet' (notional).
#   JETSON_FLASH_HOSTNAME    Image hostname (default: orin-c).
#   JETSON_FLASH_BOARD       L4T board target (default:
#                            jetson-agx-orin-devkit).
#   JETSON_FLASH_SSH_KEY     SSH public key to bake into
#                            ~${user}/.ssh/authorized_keys. If set,
#                            key auth works on first boot and we can
#                            skip the password-based seed step.
#   WORK_DIR                 Workspace for L4T BSP + rootfs
#                            (default: $HOME/jetson-ubuntu-flash).
#
# Usage:
#   sudo -E ./flash-jetson-ubuntu.sh
#
# Host prerequisites (apt install on the x86 host):
#   wget tar usbutils qemu-user-static curl unzip ca-certificates
#
# After flash, follow docs/operations/pilot-bringup.md §2 onward.

set -euo pipefail

# ----- Config ----------------------------------------------------------------
# L4T R36.4.3 = JetPack 6.1.x. Pinned URLs because the L4T directory
# layout changes between releases. Bump in lockstep when moving to a
# newer JetPack.
L4T_URL="https://developer.nvidia.com/downloads/embedded/l4t/r36_release_v4.3/release/Jetson_Linux_r36.4.3_aarch64.tbz2"
ROOTFS_URL="https://developer.nvidia.com/downloads/embedded/l4t/r36_release_v4.3/release/Tegra_Linux_Sample-Root-Filesystem_R36.4.3_aarch64.tbz2"

# SHA-256 of what those two URLs served recorded so the flash
# host verifies the bytes it fetched rather than only that the bzip2 stream is
# well-formed. `bzip2 -t` catches a truncated or corrupt download; it says
# nothing about whether the archive is the one this procedure was validated
# against.
#
# Be precise about what this is worth. NVIDIA publishes no checksum for these
# files -- no .sha256/.sha512/.md5 sibling exists (all 404) and the release
# page carries only asset cache-busting hashes. So these are trust-on-first-use
# digests of an authenticated TLS download, not vendor-attested values. They
# detect corruption in transit and any LATER change to the bytes behind a URL
# that is supposed to be immutable; they cannot vouch for the original.
#
# NVIDIA does occasionally re-spin a release in place under the same filename.
# When that happens this script FAILS rather than silently flashing different
# bits -- which is the point. Re-verify deliberately, then bump these constants
# in the same PR as the L4T version table in README.md.
L4T_SHA256="949a44049c4ce6a8efdf572ea0820c874f6ee5d41ca3e4935b9f0e38d11873d2"
ROOTFS_SHA256="67b85bae18a532dd2fd75399359ba10829c30c0e60d77231b614df39ab52156d"

WORK_DIR="${WORK_DIR:-$HOME/jetson-ubuntu-flash}"
# 'goatnet' / 'goatnet' are NOTIONAL defaults. Operators override
# both via env var (.envrc.local) and rotate the password
# immediately post-flash. See header comment.
DEFAULT_USER="${JETSON_FLASH_USER:-goatnet}"
DEFAULT_PASS="${JETSON_FLASH_USER_PASS:-goatnet}"
DEFAULT_HOSTNAME="${JETSON_FLASH_HOSTNAME:-orin-c}"
BOARD="${JETSON_FLASH_BOARD:-jetson-agx-orin-devkit}"
SSH_PUBKEY="${JETSON_FLASH_SSH_KEY:-}"

NETBIRD_VERSION="0.68.3"
RATHOLE_VERSION="0.5.0"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()   { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ----- Pre-flight ------------------------------------------------------------

check_prerequisites() {
    log "Checking prerequisites..."

    if [[ $EUID -ne 0 ]]; then
        error "Run as root: sudo -E $0"
    fi

    for cmd in wget qemu-aarch64-static lsusb tar curl unzip; do
        if ! command -v "$cmd" &>/dev/null; then
            error "$cmd not installed. apt install -y wget tar usbutils qemu-user-static curl unzip ca-certificates"
        fi
    done

    local avail
    avail=$(df --output=avail -BG "$HOME" | tail -1 | tr -dc '0-9')
    if (( avail < 30 )); then
        error "Need at least 30 GB free in $HOME (have ${avail} GB)."
    fi

    log "Prerequisites OK (${avail} GB free)."
}

check_jetson_connected() {
    log "Checking Jetson recovery mode..."
    if lsusb | grep -qi 'NVIDIA Corp. APX'; then
        log "Jetson detected in recovery mode (APX)."
        return
    fi

    warn "Jetson not detected via USB."
    cat <<EOM

Recovery mode steps:
  1. Unplug power from the Jetson.
  2. Connect USB-C from the Jetson flashing port (above the power
     port) to this host.
  3. Hold the RECOVERY button.
  4. Plug in power while holding RECOVERY.
  5. Wait 2 seconds, release RECOVERY.

EOM
    read -rp "Press Enter when ready, or Ctrl+C to abort..."

    if ! lsusb | grep -qi 'NVIDIA Corp. APX'; then
        error "Jetson still not detected in recovery mode."
    fi
    log "Jetson detected."
}

# ----- L4T workspace ---------------------------------------------------------

# A cached but corrupt .bz2 (from a prior killed wget) blocks the
# whole flow because we'd skip re-download and explode at extract
# time. Verify integrity; if bad, delete + force re-download.
verify_sha256() {
    local file="$1" want="$2" label="$3"
    local got
    got="$(sha256sum "$file" | awk '{print $1}')"
    if [[ "$got" != "$want" ]]; then
        error "${label} SHA-256 mismatch.
  expected: ${want}
  actual:   ${got}
This is either a corrupt download or NVIDIA re-spun the release in place under
the same filename. Do NOT flash. Re-verify the source deliberately, then bump
the pinned constant at the top of this script in the same change as the L4T
version table in README.md."
    fi
    log "${label} SHA-256 matches the pin."
}

fetch_bzip2_with_integrity() {
    local file="$1" url="$2" label="$3" want_sha="$4"
    if [[ -f "$file" ]]; then
        # Both checks, in this order. bzip2 -t is the cheap "is this a whole
        # archive" test; the digest is the "is it the RIGHT archive" test. A
        # cached file that passes bzip2 -t but fails the digest is exactly the
        # case the digest was added for, so a cached file is not trusted until
        # both agree.
        if bzip2 -t "$file" 2>/dev/null && \
           [[ "$(sha256sum "$file" | awk '{print $1}')" == "$want_sha" ]]; then
            log "${label} already downloaded, intact, and matches the pin."
            return
        fi
        warn "${label} cached but corrupt or off-pin — re-downloading."
        rm -f "$file"
    fi
    log "Downloading ${label}..."
    wget -q --show-progress "$url" -O "$file"
    log "Verifying ${label} integrity..."
    if ! bzip2 -t "$file"; then
        error "${label} failed integrity check after download."
    fi
    verify_sha256 "$file" "$want_sha" "$label"
}

download_files() {
    mkdir -p "$WORK_DIR"
    cd "$WORK_DIR"

    # Sizes are the real content-lengths, measured . The previous
    # "~3 GB / ~1 GB" labels had them backwards and inflated the total by a
    # third, which matters when the number is what an operator uses to decide
    # whether the flash host's 30 GB headroom is enough.
    fetch_bzip2_with_integrity tegra.bz2  "$L4T_URL"    "L4T BSP (683 MiB)"       "$L4T_SHA256"
    fetch_bzip2_with_integrity rootfs.bz2 "$ROOTFS_URL" "sample rootfs (1.7 GiB)" "$ROOTFS_SHA256"

    if [[ ! -d Linux_for_Tegra ]]; then
        log "Extracting L4T BSP..."
        tar xf tegra.bz2
    fi

    if [[ ! -f Linux_for_Tegra/rootfs/etc/hostname ]]; then
        log "Extracting rootfs into Linux_for_Tegra/rootfs/..."
        cd Linux_for_Tegra/rootfs
        sudo tar xpf ../../rootfs.bz2
        cd ../..
    fi

    cd Linux_for_Tegra
    log "L4T workspace ready at $WORK_DIR/Linux_for_Tegra"
}

apply_binaries() {
    log "Applying NVIDIA binaries to rootfs (apply_binaries.sh)..."
    ./apply_binaries.sh
    log "NVIDIA binaries applied."
}

# ----- Pre-bake rootfs (ARM64 chroot via qemu-aarch64-static) ----------------

mount_rootfs() {
    cp /usr/bin/qemu-aarch64-static rootfs/usr/bin/
    mount -t proc proc rootfs/proc 2>/dev/null || true
    mount -t sysfs sysfs rootfs/sys 2>/dev/null || true
    mount --bind /dev rootfs/dev 2>/dev/null || true
    mount --bind /dev/pts rootfs/dev/pts 2>/dev/null || true
    cp /etc/resolv.conf rootfs/etc/resolv.conf 2>/dev/null || true
}

unmount_rootfs() {
    umount rootfs/dev/pts 2>/dev/null || true
    umount rootfs/dev 2>/dev/null || true
    umount rootfs/sys 2>/dev/null || true
    umount rootfs/proc 2>/dev/null || true
    rm -f rootfs/usr/bin/qemu-aarch64-static
}

prebake_rootfs() {
    log "Pre-baking rootfs via ARM64 chroot..."
    mount_rootfs
    # Always unmount, even on chroot failure.
    trap unmount_rootfs EXIT

    chroot rootfs /bin/bash -c "
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq

        # Docker (one-step install matches Ansible docker-engine role's runtime).
        curl -fsSL https://get.docker.com | sh

        apt-get install -y -qq \
            htop iotop tmux vim jq \
            lsof strace git unzip \
            gettext-base bash-completion \
            openssh-server ufw fail2ban \
            curl wget ca-certificates

        apt-get install -y -qq \
            nmap iperf3 nload iftop mtr-tiny traceroute \
            tcpdump ethtool net-tools dnsutils \
            wireguard-tools

        systemctl enable docker
        systemctl enable ssh

        # Allow ssh-copy-id from the admin Mac on first boot.
        sed -i 's/^#\\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config

        # Some apt deps (flash-kernel via tcpdump/iperf3 etc.) drag in
        # 'linux-image-*-realtime' as a transitive dependency. The Tegra
        # L4T flash boots only the Tegra kernel via extlinux; the
        # realtime kernel never runs but eats ~200 MB and confuses
        # future apt-upgrade runs. Purge it.
        apt-get remove -y --purge 'linux-image-*-realtime' 'linux-modules-*-realtime' 'linux-modules-extra-*-realtime' 2>/dev/null || true
        apt-get autoremove -y 2>/dev/null || true
    " 2>&1 | grep -E '>>|Setting up|ERROR' || true

    if [[ -n "$SSH_PUBKEY" ]]; then
        log "  Baking SSH public key for first-boot user (oneshot)..."
        # The user is created post-flash by l4t_create_default_user.sh
        # NOT during chroot, so we stage the key in /etc/goat-flash/
        # and a first-boot oneshot moves it into the user's home.
        mkdir -p "rootfs/etc/goat-flash"
        echo "$SSH_PUBKEY" > "rootfs/etc/goat-flash/authorized_keys"
        chmod 644 "rootfs/etc/goat-flash/authorized_keys"

        cat > "rootfs/etc/systemd/system/goat-flash-ssh-seed.service" <<'UNIT'
[Unit]
Description=Seed authorized_keys for the first-boot user (one-shot)
ConditionPathExists=/etc/goat-flash/authorized_keys
After=multi-user.target

[Service]
Type=oneshot
ExecStart=/bin/bash -c 'install -d -m 0700 -o ${USER} -g ${USER} /home/${USER}/.ssh && install -m 0600 -o ${USER} -g ${USER} /etc/goat-flash/authorized_keys /home/${USER}/.ssh/authorized_keys && rm -f /etc/goat-flash/authorized_keys'
Environment=USER=PLACEHOLDER_USER
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT
        sed -i "s/PLACEHOLDER_USER/${DEFAULT_USER}/g" "rootfs/etc/systemd/system/goat-flash-ssh-seed.service"
        chroot rootfs systemctl enable goat-flash-ssh-seed.service
    fi

    local bindir="rootfs/usr/local/bin"

    log "  Installing netbird ${NETBIRD_VERSION}..."
    curl -fsSL "https://github.com/netbirdio/netbird/releases/download/v${NETBIRD_VERSION}/netbird_${NETBIRD_VERSION}_linux_arm64.tar.gz" \
        | tar xzf - -C "$bindir" netbird
    chmod +x "$bindir/netbird"

    log "  Installing rathole ${RATHOLE_VERSION}..."
    curl -fsSL "https://github.com/rathole-org/rathole/releases/download/v${RATHOLE_VERSION}/rathole-aarch64-unknown-linux-musl.zip" \
        -o /tmp/rathole.zip
    (cd /tmp && unzip -o rathole.zip rathole)
    cp /tmp/rathole "$bindir/rathole"
    chmod +x "$bindir/rathole"
    rm -f /tmp/rathole.zip /tmp/rathole

    chroot rootfs /bin/bash -c "apt-get clean; rm -rf /var/lib/apt/lists/*" 2>/dev/null || true

    unmount_rootfs
    trap - EXIT

    log "Rootfs pre-baked."
}

create_default_user() {
    # Skips the first-boot OOBE wizard. Without this the Orin would
    # boot to a graphical setup wizard that requires a monitor +
    # keyboard. With this, first boot lands directly at a usable
    # login over SSH (assuming network is up via DHCP).
    log "Pre-creating default user '${DEFAULT_USER}' with hostname '${DEFAULT_HOSTNAME}'..."
    ./tools/l4t_create_default_user.sh \
        -u "$DEFAULT_USER" \
        -p "$DEFAULT_PASS" \
        -n "$DEFAULT_HOSTNAME" \
        --accept-license
}

# ----- Flash -----------------------------------------------------------------

flash_jetson() {
    log "Installing flash-side prerequisites (one-time)..."
    ./tools/l4t_flash_prerequisites.sh

    log "========================================="
    log "  FLASHING ${BOARD} (Ubuntu-for-Tegra 22.04)"
    log "  DO NOT DISCONNECT USB OR POWER"
    log "  Expect 10-20 minutes."
    log "========================================="

    ./flash.sh "$BOARD" internal

    log "========================================="
    log "  FLASH COMPLETE"
    log "========================================="
}

# ----- Main ------------------------------------------------------------------

main() {
    cat <<EOM

===========================================
  Ubuntu-for-Tegra 22.04 -> Jetson AGX Orin
  ($(date -u +%Y-%m-%dT%H:%M:%SZ))
  user:     ${DEFAULT_USER}
  hostname: ${DEFAULT_HOSTNAME}
  board:    ${BOARD}
  workdir:  ${WORK_DIR}
===========================================

EOM

    check_prerequisites
    check_jetson_connected
    download_files
    apply_binaries
    prebake_rootfs
    create_default_user
    flash_jetson

    cat <<EOM

Jetson will reboot into Ubuntu-for-Tegra 22.04.

First-boot credentials:
  user: ${DEFAULT_USER}
  pass: (the value of JETSON_FLASH_USER_PASS — rotate after first SSH)

Pre-baked: docker, netbird ${NETBIRD_VERSION}, rathole ${RATHOLE_VERSION},
htop, iotop, tmux, vim, jq, git, openssh-server, ufw, fail2ban,
nmap, iperf3, tcpdump, ethtool, mtr, dig, wireguard-tools, etc.

Next steps from the admin Mac (see docs/operations/pilot-bringup.md):
  1. Find the Orin's LAN IP (DHCP by default).
  2. ops/scripts/seed-ssh-key.sh "${DEFAULT_USER}@<orin-ip>" <PASS_ENV_VAR>
  3. ops/ansible/mgmt-host-baseline.yml
  4. ops/ansible/mgmt-host-luks.yml      # NVMe LUKS enrollment
  5. <site>-bootstrap.yml                # mgmt-plane stack

EOM
}

main "$@"
