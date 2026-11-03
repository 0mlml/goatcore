#!/bin/sh
# Run this from initramfs busybox shell after the post-encryption reboot.
# Usage: sh /mnt/boot/encrypt-rootfs-unlock.sh

# Look for key in /tmp/ first (recommended: mount USB, copy key to /tmp/)
# Fall back to /mnt/boot/ for backward compatibility
KEY_FILE=$(ls /tmp/*.lek 2>/dev/null | head -1)
if [ -z "$KEY_FILE" ]; then
    KEY_FILE=$(ls /mnt/boot/*.lek 2>/dev/null | head -1)
    if [ -z "$KEY_FILE" ]; then
        mkdir -p /mnt/boot
        # Detect boot partition (NVMe or SD)
        if [ -b /dev/nvme0n1p1 ]; then
            mount -t vfat /dev/nvme0n1p1 /mnt/boot
        else
            mount -t vfat /dev/mmcblk0p1 /mnt/boot
        fi
        KEY_FILE=$(ls /mnt/boot/*.lek 2>/dev/null | head -1)
    fi
fi

if [ -z "$KEY_FILE" ]; then
    echo "ERROR: No .lek key file found"
    echo ""
    echo "Mount the USB key drive and copy the key to /tmp/:"
    echo "  mkdir -p /mnt/usbkey && mount /dev/sda1 /mnt/usbkey"
    echo "  cp /mnt/usbkey/*.lek /tmp/"
    echo "  sh /mnt/boot/encrypt-rootfs-unlock.sh"
    exit 1
fi

echo ""
echo "================================================"
echo "  ODT | Unlock Encrypted Rootfs"
echo "================================================"
echo ""
echo "Unlocking with: $KEY_FILE"

# Check if already unlocked
if [ -e /dev/mapper/rootfs ]; then
    echo "rootfs already unlocked."
else
    # Detect root partition (NVMe or SD)
    if [ -b /dev/nvme0n1p2 ]; then
        ROOT_PART="/dev/nvme0n1p2"
    else
        ROOT_PART="/dev/mmcblk0p2"
    fi
    cryptsetup luksOpen --key-file "$KEY_FILE" "$ROOT_PART" rootfs
fi

# Filesystem check and repair
echo "Running filesystem check..."
e2fsck -fy /dev/mapper/rootfs

echo ""
echo "================================================"
echo "  Unlocked. Booting into full OS..."
echo "================================================"
echo ""
exit
