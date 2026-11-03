#!/bin/sh
# Run this from the initramfs busybox shell.
# Usage: sh /mnt/boot/encrypt-rootfs.sh
#
# This script handles the entire encryption process:
#   1. Filesystem check
#   2. Shrink filesystem
#   3. Encrypt partition (LUKS2) — ~40 minutes
#   4. Open encrypted volume
#   5. Filesystem repair + expand
#   6. Sync and reboot

# Look for a USB-key keyfile (.lek). If none found, fall back to passphrase
# mode (used by the YubiKey path — 03-prepare-encryption.sh).
KEY_FILE=$(ls /tmp/*.lek 2>/dev/null | head -1)
if [ -z "$KEY_FILE" ]; then
    KEY_FILE=$(ls /mnt/boot/*.lek 2>/dev/null | head -1)
    if [ -z "$KEY_FILE" ]; then
        mkdir -p /mnt/boot
        # Detect boot partition (NVMe or SD) and try to mount it
        if [ -b /dev/nvme0n1p1 ]; then
            mount -t vfat /dev/nvme0n1p1 /mnt/boot 2>/dev/null || true
        else
            mount -t vfat /dev/mmcblk0p1 /mnt/boot 2>/dev/null || true
        fi
        KEY_FILE=$(ls /mnt/boot/*.lek 2>/dev/null | head -1)
    fi
fi

# If no keyfile, switch to passphrase mode (interactive prompt during reencrypt).
USE_PASSPHRASE=0
if [ -z "$KEY_FILE" ]; then
    USE_PASSPHRASE=1
    echo ""
    echo "================================================"
    echo "  No .lek keyfile found — using PASSPHRASE mode"
    echo "  (this is the YubiKey path — you'll enroll the"
    echo "   YubiKey after first boot via 04-enroll-yubikey.sh)"
    echo "================================================"
    echo ""
fi

# Detect root partition (NVMe or SD card)
if [ -b /dev/nvme0n1p2 ]; then
    ROOT_PART="/dev/nvme0n1p2"
    BOOT_PART="/dev/nvme0n1p1"
elif [ -b /dev/mmcblk0p2 ]; then
    ROOT_PART="/dev/mmcblk0p2"
    BOOT_PART="/dev/mmcblk0p1"
else
    echo "ERROR: No root partition found (checked nvme0n1p2 and mmcblk0p2)"
    exit 1
fi

echo ""
echo "================================================"
echo "  ODT | Root Filesystem Encryption"
echo "================================================"
echo ""
if [ "$USE_PASSPHRASE" = "1" ]; then
    echo "Mode:           PASSPHRASE (YubiKey path)"
    echo "Root partition: $ROOT_PART"
    echo ""
    echo "You will be prompted for a passphrase. Use a strong one — it"
    echo "guards your data until you enroll the YubiKey post-boot."
else
    echo "Mode:           USB KEYFILE"
    echo "Using key:      $KEY_FILE"
    echo "Root partition: $ROOT_PART"
fi
echo ""

# A device already carrying a LUKS header means a previous run started
# encrypting. Re-running from the top would point e2fsck/resize2fs at
# ciphertext — refuse, and name the resume command instead. (An ssh-driven
# run whose session drops mid-reencrypt lands exactly here.)
if cryptsetup isLuks "$ROOT_PART" 2>/dev/null; then
    echo ""
    echo "================================================"
    echo "  $ROOT_PART is already (partially) LUKS."
    echo "  DO NOT re-run this script. Resume instead:"
    echo "    cryptsetup reencrypt $ROOT_PART"
    echo "  then reboot when it completes."
    echo "================================================"
    exit 1
fi

echo "=== [1/6] Checking filesystem ==="
e2fsck -fy "$ROOT_PART"

echo ""
echo "=== [2/6] Shrinking filesystem ==="
resize2fs -M "$ROOT_PART"

echo ""
echo "=== [3/6] Encrypting root filesystem ==="
echo "Budget minutes per ~15 GB of PARTITION size (the whole device is"
echo "reencrypted, not just used blocks — a 2 TB NVMe is hours, not 40 min)."
echo "DO NOT power off. If you are on ssh and the session drops, reconnect"
echo "and resume with: cryptsetup reencrypt $ROOT_PART  (never re-run this script)."
echo ""
echo ">>> cryptsetup asks 'Are you sure?' — the answer is YES, ALL CAPITALS. <<<"
echo ">>> Lowercase 'yes' silently aborts. (This cost an afternoon once.)    <<<"
echo ""
# FIPS-mode LUKS2: AES-256-XTS (-s 512), SHA-256, PBKDF2 (argon2id is not FIPS-approved)
if [ "$USE_PASSPHRASE" = "1" ]; then
    cryptsetup reencrypt --new --reduce-device-size=16M \
      --type=luks2 -c aes-xts-plain64 -s 512 -h sha256 \
      --pbkdf pbkdf2 --pbkdf-force-iterations 600000 \
      --use-urandom \
      "$ROOT_PART"
else
    cryptsetup reencrypt --new --reduce-device-size=16M \
      --type=luks2 -c aes-xts-plain64 -s 512 -h sha256 \
      --pbkdf pbkdf2 --pbkdf-force-iterations 600000 \
      --use-urandom --key-file "$KEY_FILE" \
      "$ROOT_PART"
fi

# cryptsetup failing outright (a mistyped passphrase VERIFY is enough) must
# STOP the script here: a still-plain device sails past the resume loop below
# (no online-reencrypt flag to see), [4/6] and [5/6] fail, and [6/6] reboots —
# from outside that loop is indistinguishable from a power fault. Proven the
# hard way on delta-core-b, 2026-09-11.
if ! cryptsetup isLuks "$ROOT_PART"; then
    echo ""
    echo "================================================"
    echo "  ERROR: encryption did not start — $ROOT_PART is still plain."
    echo "  Read the cryptsetup message above (a passphrase verify"
    echo "  mismatch is the usual cause), then re-run this script."
    echo "  NOT rebooting — the shell is yours."
    echo "================================================"
    exit 1
fi

# Verify reencryption finished cleanly. If interrupted, the LUKS header keeps
# 'online-reencrypt-v2' in Requirements and a portion of the device stays in
# plaintext — but the mapper presents a consistent view, so this is silent.
# Auto-resume until the flag clears.
echo ""
echo "=== [3.5/6] Verifying reencryption completion ==="
RESUME_ATTEMPTS=0
while cryptsetup luksDump "$ROOT_PART" 2>/dev/null | grep -q 'online-reencrypt'; do
    RESUME_ATTEMPTS=$((RESUME_ATTEMPTS + 1))
    if [ "$RESUME_ATTEMPTS" -gt 5 ]; then
        echo "ERROR: reencryption flag still set after 5 resume attempts. Aborting."
        exit 1
    fi
    echo "Reencryption incomplete (attempt $RESUME_ATTEMPTS) — resuming..."
    if [ "$USE_PASSPHRASE" = "1" ]; then
        cryptsetup reencrypt --resume-only "$ROOT_PART"
    else
        cryptsetup reencrypt --resume-only --key-file "$KEY_FILE" "$ROOT_PART"
    fi
done
echo "Reencryption complete. No 'online-reencrypt' in Requirements."

echo ""
echo "=== [4/6] Opening encrypted volume ==="
# Three tries, then stop WITHOUT rebooting: at this point the data is fully
# encrypted and safe; a reboot here just costs the operator a reconnect.
OPEN_TRIES=0
while :; do
    if [ "$USE_PASSPHRASE" = "1" ]; then
        cryptsetup luksOpen "$ROOT_PART" rootfs && break
    else
        cryptsetup luksOpen --key-file "$KEY_FILE" "$ROOT_PART" rootfs && break
    fi
    OPEN_TRIES=$((OPEN_TRIES + 1))
    if [ "$OPEN_TRIES" -ge 3 ]; then
        echo "ERROR: could not open $ROOT_PART after 3 attempts. NOT rebooting."
        echo "Encryption itself is COMPLETE. Finish by hand:"
        echo "  cryptsetup luksOpen $ROOT_PART rootfs"
        echo "  e2fsck -fy /dev/mapper/rootfs && resize2fs /dev/mapper/rootfs"
        echo "  sync; reboot -f"
        exit 1
    fi
    echo "Unlock failed (attempt $OPEN_TRIES/3) — try again."
done

echo ""
echo "=== [5/6] Repairing and expanding filesystem ==="
e2fsck -fy /dev/mapper/rootfs
resize2fs /dev/mapper/rootfs

echo ""
echo "=== [6/6] Syncing and rebooting ==="
echo "Flushing all writes to disk..."
sync
sync
sleep 2

echo ""
echo "================================================"
echo "  Encryption complete!"
echo ""
echo "  After reboot, you will be at busybox again."
echo "  Run:"
echo "    sh /mnt/boot/encrypt-rootfs-unlock.sh"
echo ""
echo "  Rebooting in 5 seconds..."
echo "================================================"
sleep 5
reboot -f
